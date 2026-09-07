package cinema

import (
	"bytes"
	"encoding/binary"
	"log"
	"testing"
)

func tag4(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func newTestHikStream() *HikStream {
	return &HikStream{logger: log.New(bytes.NewBuffer(nil), "", 0)}
}

// TestClassifyAsAudioLocksOnContinuity verifies the bootstrap/lock mechanism:
// a payload size only becomes "audio" once it's shown audioLockThreshold
// consecutive chunks whose tag advances by exactly the payload length,
// surviving video chunks of other (non-repeating) sizes interleaved in
// between — real traffic is interleaved this way (see the package comment on
// the 8-byte "stream tag").
func TestClassifyAsAudioLocksOnContinuity(t *testing.T) {
	s := newTestHikStream()
	audio := make([]byte, 320)
	video1 := make([]byte, 1200)
	video2 := make([]byte, 340) // deliberately close to the audio size

	var audioTag uint32 = 1000

	// First audioLockThreshold-1 continuous audio chunks (interleaved with
	// video of varying sizes) must NOT classify as audio yet.
	for i := range audioLockThreshold - 1 {
		if s.classifyAsAudio(tag4(audioTag), audio) {
			t.Fatalf("chunk %d: classified as audio before reaching audioLockThreshold", i)
		}
		audioTag += uint32(len(audio))
		if s.classifyAsAudio(tag4(0xAAAA0000+uint32(i)), video1) {
			t.Fatalf("video1 chunk %d misclassified as audio", i)
		}
	}
	if s.audioLockedSize != 0 {
		t.Fatalf("locked prematurely at size %d", s.audioLockedSize)
	}

	// The audioLockThreshold-th consecutive continuity-matching chunk locks.
	if !s.classifyAsAudio(tag4(audioTag), audio) {
		t.Fatal("expected lock on the threshold-th consecutive continuous chunk")
	}
	if s.audioLockedSize != len(audio) {
		t.Fatalf("audioLockedSize = %d, want %d", s.audioLockedSize, len(audio))
	}

	// A differently-sized chunk (340, not 320) is never audio once locked.
	if s.classifyAsAudio(tag4(0xBBBB0000), video2) {
		t.Fatal("differently-sized chunk classified as audio after lock")
	}

	// Further same-size chunks classify as audio regardless of tag
	// continuity — by design, classification is size-only once locked (see
	// classifyAsAudio's doc comment: re-checking continuity forever would let
	// one dropped chunk permanently break detection).
	if !s.classifyAsAudio(tag4(0xDEADBEEF), audio) {
		t.Fatal("same-size chunk with broken tag continuity should still classify as audio post-lock")
	}
}

// TestClassifyAsAudioNeverLocksOnPureVideo feeds chunks whose size sometimes
// repeats but whose tag never shows the continuity pattern (mimicking how
// video's per-frame-constant-then-jump tag looks to this function) and checks
// no lock ever happens — the classifier must stay inert for a stream with no
// real audio track, even though 800 repeats three times below.
func TestClassifyAsAudioNeverLocksOnPureVideo(t *testing.T) {
	s := newTestHikStream()
	sizes := []int{800, 800, 800, 1200, 340, 340, 900}
	tag := uint32(0x10000)
	for _, n := range sizes {
		payload := make([]byte, n)
		if s.classifyAsAudio(tag4(tag), payload) {
			t.Fatalf("size %d misclassified as audio", n)
		}
		tag += 999 // deliberately not equal to n, breaking continuity every time
	}
	if s.audioLockedSize != 0 {
		t.Fatalf("locked at size %d on a pure-video stream", s.audioLockedSize)
	}
}
