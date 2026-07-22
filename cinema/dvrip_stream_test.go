package cinema

import (
	"bytes"
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
	s := &Stream{videoConn: &mockConn{r: bytes.NewReader(data)}}

	codec, err := s.PeekFirstFrame()
	if err != nil {
		t.Fatalf("PeekFirstFrame failed on a valid I-frame stream: %v", err)
	}
	if codec != "h264" {
		t.Fatalf("expected h264, got %q", codec)
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
