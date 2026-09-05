package controllers

import (
	"context"
	"testing"
	"time"
)

func newTestManagedStream() *managedStream {
	return &managedStream{
		subs:       make(map[chan []byte]struct{}),
		done:       make(chan struct{}),
		audioSubs:  make(map[chan []byte]struct{}),
		audioReady: make(chan struct{}),
	}
}

func TestBroadcastAudioFanout(t *testing.T) {
	ms := newTestManagedStream()
	a := ms.subscribeAudio()
	b := ms.subscribeAudio()

	ms.broadcastAudio([]byte{1, 2, 3})

	for i, ch := range []chan []byte{a, b} {
		select {
		case got := <-ch:
			if string(got) != "\x01\x02\x03" {
				t.Fatalf("sub %d got % x", i, got)
			}
		default:
			t.Fatalf("sub %d received nothing", i)
		}
	}

	// A full subscriber must be skipped, not block the broadcast.
	full := ms.subscribeAudio()
	for range hubSubChanBuf {
		full <- nil
	}
	done := make(chan struct{})
	go func() { ms.broadcastAudio([]byte{9}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broadcastAudio blocked on a full subscriber")
	}
}

func TestCloseAllClosesAudioSubs(t *testing.T) {
	ms := newTestManagedStream()
	ch := ms.subscribeAudio()
	ms.closeAll()

	if _, ok := <-ch; ok {
		t.Fatal("audio sub channel not closed by closeAll")
	}
	// A subscription after closeAll returns a pre-closed channel.
	late := ms.subscribeAudio()
	if _, ok := <-late; ok {
		t.Fatal("subscribeAudio after close should return a closed channel")
	}
}

func TestAudioMetaOnceAndWait(t *testing.T) {
	ms := newTestManagedStream()

	if ms.waitAudioMeta(context.Background(), 20*time.Millisecond) != nil {
		t.Fatal("waitAudioMeta should time out to nil when never set")
	}

	ms.setAudioMeta("pcm16", 8000, 1)
	ms.setAudioMeta("aac", 16000, 1) // ignored — once only

	m := ms.waitAudioMeta(context.Background(), time.Second)
	if m == nil || m.Codec != "pcm16" || m.Rate != 8000 {
		t.Fatalf("meta = %+v, want pcm16/8000", m)
	}

	ms2 := newTestManagedStream()
	close(ms2.done)
	if ms2.waitAudioMeta(context.Background(), time.Second) != nil {
		t.Fatal("waitAudioMeta should return nil once the stream is done")
	}
}

func TestTranscodeCapEnv(t *testing.T) {
	t.Setenv("CINEMA_MAX_TRANSCODES", "3")
	if got := transcodeCap(); got != 3 {
		t.Fatalf("transcodeCap() = %d, want 3", got)
	}
	t.Setenv("CINEMA_MAX_TRANSCODES", "garbage")
	if got := transcodeCap(); got < 4 || got > 12 {
		t.Fatalf("transcodeCap() fallback = %d, want 4..12", got)
	}
}
