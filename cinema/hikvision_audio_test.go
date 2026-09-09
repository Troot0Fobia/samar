package cinema

import (
	"bytes"
	"log"
	"testing"
)

func newTestHikStream() *HikStream {
	return &HikStream{logger: log.New(bytes.NewBuffer(nil), "", 0), audioFmt: "alaw", audioRate: 8000}
}

// plausibleAlaw320 is a 320-byte payload that decodes to non-degenerate PCM
// (cycles a few A-law codes — not constant, not a ramp, not full-scale).
func plausibleAlaw320() []byte {
	codes := []byte{0x55, 0xD5, 0x2A, 0xAA, 0x35, 0xB5, 0x15, 0x95}
	b := make([]byte, 320)
	for i := range b {
		b[i] = codes[i%len(codes)]
	}
	return b
}

// TestHikAudioCodecSupported covers the shared support-check used both by
// audioCodecFor (actual stream decode) and ListChannels (the sidebar's
// "audio available" indicator) — they must never disagree about which
// codecs are playable.
func TestHikAudioCodecSupported(t *testing.T) {
	cases := []struct {
		codec string
		want  bool
	}{
		{"G.711ulaw", true},
		{"G.711alaw", true},
		{"MP2L2", false},
		{"UNKOWN", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := hikAudioCodecSupported(tc.codec); got != tc.want {
			t.Errorf("hikAudioCodecSupported(%q) = %v, want %v", tc.codec, got, tc.want)
		}
	}
}

// TestClassifyAsAudioMarker verifies the chunk-header discriminator: audio has
// byte4 == 0x80 and byte5 & 0x40 == 0; anything with the video type bit set,
// the wrong flag byte, or an out-of-band payload size is video.
func TestClassifyAsAudioMarker(t *testing.T) {
	aud := plausibleAlaw320()
	vid := make([]byte, 1200)

	cases := []struct {
		name      string
		b4, b5    byte
		payload   []byte
		wantAudio bool
	}{
		{"audio b5=0x88", 0x80, 0x88, aud, true},
		{"audio b5=0x80", 0x80, 0x80, aud, true},
		{"video P-slice b5=0x60", 0x80, 0x60, vid, false},
		{"video I-slice b5=0xe0", 0xa0, 0xe0, vid, false},
		{"video params b5=0xf0", 0x90, 0xf0, vid, false},
		{"audio marker, wrong flag byte", 0x90, 0x80, aud, false},
		{"audio marker, payload too large", 0x80, 0x80, make([]byte, 4096), false},
		{"audio marker, payload too small", 0x80, 0x80, make([]byte, 40), false},
	}
	for _, tc := range cases {
		s := newTestHikStream()
		if got := s.classifyAsAudio(tc.b4, tc.b5, tc.payload); got != tc.wantAudio {
			t.Errorf("%s: classifyAsAudio(0x%02x,0x%02x,len=%d) = %v, want %v",
				tc.name, tc.b4, tc.b5, len(tc.payload), got, tc.wantAudio)
		}
	}
}

// TestClassifyAsAudioSilencePasses checks that digital silence (a constant
// payload) is accepted — the one-shot G.711 sanity gate must not reject it.
func TestClassifyAsAudioSilencePasses(t *testing.T) {
	s := newTestHikStream()
	silence := bytes.Repeat([]byte{0xD5}, 320) // A-law silence
	for i := range 5 {
		if !s.classifyAsAudio(0x80, 0x88, silence) {
			t.Fatalf("chunk %d: silence rejected as non-audio", i)
		}
	}
	if s.audioDisabled {
		t.Fatal("audio latched off on a silent stream")
	}
}

// TestClassifyAsAudioSanityDisable feeds marker-matching chunks whose payload
// decodes to structurally non-audio PCM (a full-scale square wave). After
// hikAudioSanityRun of them the stream latches native audio off and stays off.
func TestClassifyAsAudioSanityDisable(t *testing.T) {
	s := newTestHikStream()
	square := make([]byte, 320)
	for i := range square {
		if i%2 == 0 {
			square[i] = 0xAA // ~ +full scale (A-law)
		} else {
			square[i] = 0x2A // ~ -full scale
		}
	}
	for i := range hikAudioSanityRun {
		if s.classifyAsAudio(0x80, 0x88, square) {
			t.Fatalf("chunk %d: non-audio payload accepted before the sanity run elapsed", i)
		}
	}
	if !s.audioDisabled {
		t.Fatalf("audio not latched off after %d failing chunks", hikAudioSanityRun)
	}
	// Real audio now is ignored — the stream is video-only.
	if s.classifyAsAudio(0x80, 0x88, plausibleAlaw320()) {
		t.Fatal("classifyAsAudio still accepts audio after being disabled")
	}
}
