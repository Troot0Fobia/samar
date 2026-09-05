package cinema

import (
	"bytes"
	"testing"
)

// Anchor values cross-checked against ffmpeg's -f alaw / -f mulaw decoders over
// the full 0x00..0xFF range.
func TestG711DecodeTables(t *testing.T) {
	alaw := map[byte]int16{
		0x00: -5504, 0x55: -8, 0xd5: 8, 0x2a: -32256, 0xaa: 32256, 0x80: 5504, 0xff: 848,
	}
	for code, want := range alaw {
		if got := alawDecodeTable[code]; got != want {
			t.Errorf("alaw[%#02x] = %d, want %d", code, got, want)
		}
	}
	mulaw := map[byte]int16{
		0x00: -32124, 0x7f: 0, 0xff: 0, 0x80: 32124, 0x2a: -5372, 0xaa: 5372,
	}
	for code, want := range mulaw {
		if got := mulawDecodeTable[code]; got != want {
			t.Errorf("mulaw[%#02x] = %d, want %d", code, got, want)
		}
	}
}

func TestDecodeG711(t *testing.T) {
	in := []byte{0xd5, 0x55, 0x2a, 0xaa} // A-law: 8, -8, -32256, 32256

	out := DecodeG711("alaw", in)
	if len(out) != 2*len(in) {
		t.Fatalf("len = %d, want %d", len(out), 2*len(in))
	}
	want := []byte{0x08, 0x00, 0xf8, 0xff, 0x00, 0x82, 0x00, 0x7e}
	if !bytes.Equal(out, want) {
		t.Fatalf("alaw PCM = % x, want % x", out, want)
	}

	if got := DecodeG711("mulaw", []byte{0xff, 0x7f}); !bytes.Equal(got, []byte{0, 0, 0, 0}) {
		t.Fatalf("mulaw silence = % x, want zeros", got)
	}

	if DecodeG711("aac", in) != nil || DecodeG711("", in) != nil || DecodeG711("s16le", in) != nil {
		t.Fatal("non-G.711 formats must return nil")
	}
}
