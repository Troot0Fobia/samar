package controllers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"

	"Troot0Fobia/samar/helpers"
)

const (
	hubTailBufSize = 512 * 1024 // 512 KB tail buffer for late joiners
	hubSubChanBuf  = 256        // per-subscriber channel capacity
)

// managedStream holds a single upstream connection (ffmpeg) shared by all
// viewers of the same camera channel.
type managedStream struct {
	cancel  context.CancelFunc
	mu      sync.Mutex
	subs    map[chan []byte]struct{}
	tailBuf []byte
	refs    int
	done    chan struct{} // closed by closeAll when the stream ends
}

func (ms *managedStream) subscribe() (chan []byte, []byte) {
	ch := make(chan []byte, hubSubChanBuf)
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// If the stream is already dead, return a pre-closed channel.
	select {
	case <-ms.done:
		close(ch)
		return ch, nil
	default:
	}

	snapshot := make([]byte, len(ms.tailBuf))
	copy(snapshot, ms.tailBuf)
	ms.subs[ch] = struct{}{}
	return ch, snapshot
}

func (ms *managedStream) unsubscribe(ch chan []byte) {
	ms.mu.Lock()
	delete(ms.subs, ch)
	ms.mu.Unlock()
}

func (ms *managedStream) broadcast(data []byte) {
	chunk := make([]byte, len(data))
	copy(chunk, data)

	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Append to tail buffer, trimming to hubTailBufSize at 188-byte (TS) boundary.
	ms.tailBuf = append(ms.tailBuf, chunk...)
	if len(ms.tailBuf) > hubTailBufSize {
		excess := len(ms.tailBuf) - hubTailBufSize
		excess = (excess / 188) * 188
		if excess > 0 {
			ms.tailBuf = ms.tailBuf[excess:]
		}
	}

	for ch := range ms.subs {
		select {
		case ch <- chunk:
		default:
			// slow consumer — drop frame rather than blocking others
		}
	}
}

func (ms *managedStream) closeAll() {
	ms.mu.Lock()
	for ch := range ms.subs {
		close(ch)
		delete(ms.subs, ch)
	}
	close(ms.done)
	ms.mu.Unlock()
}

// cinemaStreamHub manages a shared managed stream per unique stream key.
type cinemaStreamHub struct {
	mu      sync.Mutex
	streams map[string]*managedStream
}

var globalHub = &cinemaStreamHub{streams: make(map[string]*managedStream)}

// join returns the existing managed stream for key, or creates a new one and
// starts startFn in a goroutine. The caller must call leave when done.
func (h *cinemaStreamHub) join(key string, startFn func(ctx context.Context, broadcast func([]byte))) *managedStream {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ms, ok := h.streams[key]; ok {
		ms.refs++
		return ms
	}

	ctx, cancel := context.WithCancel(context.Background())
	ms := &managedStream{
		cancel: cancel,
		subs:   make(map[chan []byte]struct{}),
		refs:   1,
		done:   make(chan struct{}),
	}
	h.streams[key] = ms

	go func() {
		startFn(ctx, ms.broadcast)
		// Clean up: remove from hub so the next caller creates a fresh stream.
		h.mu.Lock()
		if h.streams[key] == ms {
			delete(h.streams, key)
		}
		h.mu.Unlock()
		ms.closeAll()
	}()

	return ms
}

// leave decrements the viewer count for ms. When it reaches zero the upstream
// connection (ffmpeg) is cancelled.
func (h *cinemaStreamHub) leave(key string, ms *managedStream) {
	ms.mu.Lock()
	ms.refs--
	shouldCancel := ms.refs <= 0
	ms.mu.Unlock()

	if shouldCancel {
		// Remove from the hub map so a new viewer gets a fresh stream.
		h.mu.Lock()
		if h.streams[key] == ms {
			delete(h.streams, key)
		}
		h.mu.Unlock()
		ms.cancel()
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// runFFmpegBroadcast is the hub-aware analogue of runFFmpegWS: it runs ffmpeg,
// reads its MPEG-TS output, and fans data out via broadcast instead of writing
// directly to a single WebSocket connection.
func runFFmpegBroadcast(ctx context.Context, stream io.Reader, codec, tag string, broadcast func([]byte)) {
	var ffmpegArgs []string
	switch codec {
	case "mjpeg":
		ffmpegArgs = []string{
			"-loglevel", "warning",
			"-f", "mjpeg", "-i", "pipe:0",
			"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
			"-r", "15", "-g", "15", "-an",
			"-f", "mpegts", "pipe:1",
		}
	case "hevc":
		ffmpegArgs = []string{
			"-loglevel", "warning",
			"-f", "hevc", "-i", "pipe:0",
			"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
			"-r", "25", "-g", "25", "-an",
			"-f", "mpegts", "pipe:1",
		}
	default:
		ffmpegArgs = []string{
			"-loglevel", "warning",
			"-f", "h264", "-i", "pipe:0",
			"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
			"-r", "25", "-g", "25", "-an",
			"-f", "mpegts", "pipe:1",
		}
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", ffmpegArgs...)
	cmd.Stdin = stream

	ffmpegOut, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	ffmpegErr, err := cmd.StderrPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		helpers.LogError("cinema ffmpeg broadcast start", tag, err.Error())
		return
	}
	helpers.LogSuccess(fmt.Sprintf("[%s] ffmpeg hub started (codec=%s)", tag, codec), tag)
	go func() {
		sc := bufio.NewScanner(ffmpegErr)
		for sc.Scan() {
			helpers.LogError("cinema ffmpeg broadcast", tag, sc.Text())
		}
	}()

	buf := make([]byte, 188*128) // 188-byte aligned read buffer
	for {
		n, err := ffmpegOut.Read(buf)
		if n > 0 {
			broadcast(buf[:n])
		}
		if err != nil {
			break
		}
		if ctx.Err() != nil {
			break
		}
	}

	if cmd.Process != nil {
		cmd.Process.Kill()
	}
	cmd.Wait()
	helpers.LogSuccess(fmt.Sprintf("[%s] ffmpeg hub stopped", tag), tag)
}

// pumpSubToWS reads chunks from a subscriber channel and writes them as
// WebSocket binary frames. Returns when the ctx is cancelled, the channel is
// closed (stream ended), or a write error occurs.
func pumpSubToWS(ctx context.Context, conn net.Conn, subCh chan []byte) {
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-subCh:
			if !ok {
				return // managed stream ended
			}
			if err := wsSendBinaryFrame(conn, data); err != nil {
				return
			}
		}
	}
}
