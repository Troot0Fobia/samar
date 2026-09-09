package cinema

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// mockConn adapts a byte slice to net.Conn so the DHAV parser can be exercised
// against real captured video without a live camera.
type mockConn struct{ r *bytes.Reader }

func (m *mockConn) Read(p []byte) (int, error)         { return m.r.Read(p) }
func (m *mockConn) Write(p []byte) (int, error)        { return len(p), nil }
func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// TestPeekFirstFrameRealCapture feeds the real DHAV stream captured from
// 178.165.116.41 (works in SmartPSS, timed out in our snapshot) through the
// parser. The capture is a clean H.264 I-frame (SPS/PPS/IDR), so PeekFirstFrame
// must return "h264" quickly rather than spinning until the deadline.
func TestPeekFirstFrameRealCapture(t *testing.T) {
	// Fixture is a real captured DHAV stream, kept out of the repo. When it's
	// absent, skip rather than fail — the parser logic is still covered by the
	// other tests.
	data, err := os.ReadFile("testdata/dhav_178_165_116_41.bin")
	if err != nil {
		t.Skipf("capture fixture not present: %v", err)
	}
	s := newStream(&mockConn{r: bytes.NewReader(data)})

	codec, err := s.PeekFirstFrame()
	if err != nil {
		t.Fatalf("PeekFirstFrame failed on a valid I-frame stream: %v", err)
	}
	if codec != "h264" {
		t.Fatalf("expected h264, got %q", codec)
	}
}

// buildDHAVAudioFrame assembles a DHAV 0xf0 frame the way a Dahua device frames
// one on the wire: 24-byte base header, a 4-byte 0x83 audio-info block, an
// 8-byte 0x88 block, an optional 4-byte 0x96 block, the media payload, then the
// 8-byte "dhav" trailer.
func buildDHAVAudioFrame(infoBlock [4]byte, block96 bool, payload []byte) []byte {
	return buildDHAVAudioFrameLayout(infoBlock, true, block96, payload)
}

// buildDHAVAudioFrameLayout is buildDHAVAudioFrame with control over whether
// the 8-byte 0x88 block is present — some models (confirmed against packet
// captures) omit it entirely and go straight from the 0x83 block into a 0x9x
// block.
func buildDHAVAudioFrameLayout(infoBlock [4]byte, block88, block96 bool, payload []byte) []byte {
	var frame []byte
	base := make([]byte, 24)
	copy(base, "DHAV")
	base[4] = 0xf0
	n := 24 + 4 + len(payload) + 8
	if block88 {
		n += 8
	}
	if block96 {
		n += 4
	}
	base[12] = byte(n)
	base[13] = byte(n >> 8)
	frame = append(frame, base...)
	frame = append(frame, infoBlock[:]...)
	if block88 {
		frame = append(frame, 0x88, 1, 2, 3, 4, 0, 0, 0)
	}
	if block96 {
		frame = append(frame, 0x96, 0x01, 0x00, 0x00)
	}
	frame = append(frame, payload...)
	frame = append(frame, "dhav"...)
	frame = append(frame, 0, 0, 0, 0)
	return frame
}

// buildDHAVAudioFrame9xBefore88 builds a frame with the 0x9x block preceding
// the 0x88 block — a third real-world layout confirmed against packet
// captures (176.97.56.19): 0x83 block, then a 0x9x block, then the 0x88
// block, then the payload.
func buildDHAVAudioFrame9xBefore88(infoBlock [4]byte, payload []byte) []byte {
	var frame []byte
	base := make([]byte, 24)
	copy(base, "DHAV")
	base[4] = 0xf0
	n := 24 + 4 + 4 + 8 + len(payload) + 8
	base[12] = byte(n)
	base[13] = byte(n >> 8)
	frame = append(frame, base...)
	frame = append(frame, infoBlock[:]...)
	frame = append(frame, 0x96, 0x01, 0x00, 0x00)
	frame = append(frame, 0x88, 1, 2, 3, 4, 0, 0, 0)
	frame = append(frame, payload...)
	frame = append(frame, "dhav"...)
	frame = append(frame, 0, 0, 0, 0)
	return frame
}

// TestDHAVAudioPayload covers the 0xf0 audio-frame decoder: the codec id in the
// 0x83 block's 3rd byte (verified against packet captures), the optional 0x9x
// block that shifts the payload, the real-world header layouts (0x88 present
// or absent, and a 0x9x block before or after it — confirmed against packet
// captures), and an unfamiliar codec left as video-only.
func TestDHAVAudioPayload(t *testing.T) {
	// Starts with an ADTS sync word (0xFF 0xF1) so the AAC cases pass
	// dhavAudioPayload's sync-word check; 0xFF/0xF1 are also valid G.711 and
	// s16le bytes, so the other codecs are unaffected.
	payload := append([]byte{0xFF, 0xF1}, bytes.Repeat([]byte{0xd5, 0x55}, 159)...)

	cases := []struct {
		name     string
		info     [4]byte
		block88  bool
		block96  bool
		wantFmt  string
		wantRate int
	}{
		{"s16le 16k", [4]byte{0x83, 0x01, 0x10, 0x04}, true, false, "s16le", 16000},
		{"s16le 16k with 0x9x block", [4]byte{0x83, 0x01, 0x10, 0x04}, true, true, "s16le", 16000},
		{"g711 a-law", [4]byte{0x83, 0x01, 0x0e, 0x02}, true, true, "alaw", 8000},
		{"g711 mu-law", [4]byte{0x83, 0x01, 0x0a, 0x02}, true, false, "mulaw", 8000},
		{"aac", [4]byte{0x83, 0x01, 0x1a, 0x04}, true, true, "aac", 16000},
		{"g711 a-law no 0x88 block", [4]byte{0x83, 0x01, 0x0e, 0x02}, false, true, "alaw", 8000},
		{"g711 mu-law no 0x88 block", [4]byte{0x83, 0x01, 0x0a, 0x02}, false, true, "mulaw", 8000},
		{"aac no 0x88 block", [4]byte{0x83, 0x01, 0x1a, 0x04}, false, true, "aac", 16000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStream(nil)
			got := s.dhavAudioPayload(buildDHAVAudioFrameLayout(tc.info, tc.block88, tc.block96, payload))
			if !bytes.Equal(got, payload) {
				t.Fatalf("payload = %d bytes %x, want %d (0x9x/0x88 block not skipped correctly?)", len(got), got[:min(6, len(got))], len(payload))
			}
			if r, f := s.AudioFormat(); r != tc.wantRate || f != tc.wantFmt {
				t.Fatalf("AudioFormat = (%d, %q), want (%d, %q)", r, f, tc.wantRate, tc.wantFmt)
			}
		})
	}

	t.Run("g711 a-law 0x9x before 0x88", func(t *testing.T) {
		s := newStream(nil)
		got := s.dhavAudioPayload(buildDHAVAudioFrame9xBefore88([4]byte{0x83, 0x01, 0x0e, 0x02}, payload))
		if !bytes.Equal(got, payload) {
			t.Fatalf("payload = %d bytes %x, want %d (0x9x-before-0x88 order not handled?)", len(got), got[:min(6, len(got))], len(payload))
		}
		if r, f := s.AudioFormat(); r != 8000 || f != "alaw" {
			t.Fatalf("AudioFormat = (%d, %q), want (8000, \"alaw\")", r, f)
		}
	})

	t.Run("unknown codec", func(t *testing.T) {
		s := newStream(nil)
		if got := s.dhavAudioPayload(buildDHAVAudioFrame([4]byte{0x83, 0x01, 0x05, 0x05}, false, payload)); got != nil {
			t.Fatalf("unrecognised codec must yield nil, got %d bytes", len(got))
		}
		if r, f := s.AudioFormat(); r != 0 || f != "" {
			t.Fatalf("AudioFormat = (%d, %q), want (0, \"\")", r, f)
		}
		if seen, form := s.AudioProbe(); !seen || form != "83010505" {
			t.Fatalf("AudioProbe = (%v, %q), want (true, \"83010505\")", seen, form)
		}
	})

	t.Run("malformed frame", func(t *testing.T) {
		s := newStream(nil)
		if got := s.dhavAudioPayload([]byte("DHAV\xf0 short")); got != nil {
			t.Fatalf("short frame must yield nil, got %d bytes", len(got))
		}
	})

	t.Run("trailing bytes past frameTotalSize do not leak into the payload", func(t *testing.T) {
		s := newStream(nil)
		frame := buildDHAVAudioFrame([4]byte{0x83, 0x01, 0x0e, 0x02}, false, payload)
		frame = append(frame, bytes.Repeat([]byte{0xAB}, 200)...) // start of the next frame
		got := s.dhavAudioPayload(frame)
		if !bytes.Equal(got, payload) {
			t.Fatalf("payload = %d bytes, want %d — frameTotalSize/trailer not honoured", len(got), len(payload))
		}
	})

	t.Run("missing dhav trailer yields nil", func(t *testing.T) {
		s := newStream(nil)
		frame := buildDHAVAudioFrame([4]byte{0x83, 0x01, 0x0e, 0x02}, false, payload)
		copy(frame[len(frame)-8:], "XXXX") // corrupt the trailer magic
		if got := s.dhavAudioPayload(frame); got != nil {
			t.Fatalf("frame with a broken trailer must yield nil, got %d bytes", len(got))
		}
	})
}

// TestDHAVAudioExtractionRealCapture drains a real interleaved video+audio DVRIP
// capture: video through Stream.Read (as ffmpeg would) and audio through
// NextAudioFrame, checking the audio track comes out as ~40 ms 16 kHz frames.
// The fixture is kept out of the repo; skip when it's absent.
func TestDHAVAudioExtractionRealCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/dhav_av_dahua.bin")
	if err != nil {
		t.Skipf("capture fixture not present: %v", err)
	}
	s := newStream(&mockConn{r: bytes.NewReader(data)})

	type res struct{ bytes, frames int }
	audio := make(chan res, 1)
	go func() {
		var r res
		for {
			payload, _, _, err := s.NextAudioFrame(context.Background())
			if err != nil {
				break
			}
			r.bytes += len(payload)
			r.frames++
		}
		audio <- r
	}()
	io.Copy(io.Discard, s) //nolint:errcheck — drives Stream.Read to EOF, routing audio
	s.Close()

	got := <-audio
	if s.AudioRate() != 16000 {
		t.Fatalf("AudioRate = %d, want 16000", s.AudioRate())
	}
	// Every 0xf0 payload must be exactly 1280 bytes (640 s16le samples = 40 ms
	// @ 16 kHz) — a non-multiple means header/trailer bytes leaked in.
	if got.bytes == 0 || got.bytes%1280 != 0 {
		t.Fatalf("collected %d PCM bytes, want a non-zero multiple of 1280", got.bytes)
	}
	if got.frames < 100 {
		t.Fatalf("collected only %d audio frames, fixture should yield >100", got.frames)
	}
}

// TestNextAudioFrameDrainThenEOF checks queued audio survives Close() and EOF
// is returned only once the queue is empty.
func TestNextAudioFrameDrainThenEOF(t *testing.T) {
	s := newStream(nil)
	s.audioFmt, s.audioRate = "alaw", 8000
	for i := range 3 {
		s.pushAudio([]byte{byte(i), byte(i)})
	}
	s.Close()

	for i := range 3 {
		p, format, rate, err := s.NextAudioFrame(context.Background())
		if err != nil {
			t.Fatalf("frame %d: unexpected err %v", i, err)
		}
		if len(p) != 2 || format != "alaw" || rate != 8000 {
			t.Fatalf("frame %d: got (%v, %q, %d)", i, p, format, rate)
		}
	}
	if _, _, _, err := s.NextAudioFrame(context.Background()); err != io.EOF {
		t.Fatalf("after drain: err = %v, want io.EOF", err)
	}
}

// TestNextAudioFrameCtxCancel: a cancelled ctx returns promptly.
func TestNextAudioFrameCtxCancel(t *testing.T) {
	s := newStream(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, _, _, err := s.NextAudioFrame(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("NextAudioFrame did not return on ctx cancel")
	}
}

// TestParseClaimReturn covers the real bc claim payloads captured from
// 178.165.116.41 — slot 0 (main) is dead (return=2), the substreams stream
// (return=0). Reading this lets the client fail over immediately.
func TestParseClaimReturn(t *testing.T) {
	const payload = "channel=0&return=2,channel=1&return=0,channel=2&return=0,channel=3&return=2,"
	cases := []struct {
		slot      int
		wantCode  string
		wantFound bool
	}{
		{0, "2", true},
		{1, "0", true},
		{2, "0", true},
		{3, "2", true},
		{4, "", false},
	}
	for _, tc := range cases {
		code, found := parseClaimReturn(payload, tc.slot)
		if found != tc.wantFound || code != tc.wantCode {
			t.Fatalf("slot %d: got (%q,%v), want (%q,%v)", tc.slot, code, found, tc.wantCode, tc.wantFound)
		}
	}
}

// TestLooksLikeClaimVerdict covers awaitStreamClaim's early-return check: a
// `bc` frame naming some other slot's verdict (combined or single-entry) is
// still recognised as a verdict round, so we stop waiting instead of burning
// the deadline; unrelated `bc` traffic is not mistaken for one.
func TestLooksLikeClaimVerdict(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    bool
	}{
		{"real combined capture", "channel=0&return=2,channel=1&return=0,channel=2&return=0,channel=3&return=2,", true},
		{"single entry, other slot", "channel=1&return=0,", true},
		{"no trailing comma", "channel=0&return=0", true},
		{"unrelated bc payload", "some other status text", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		if got := looksLikeClaimVerdict([]byte(tc.payload)); got != tc.want {
			t.Fatalf("looksLikeClaimVerdict(%q) = %v, want %v", tc.payload, got, tc.want)
		}
	}
}
