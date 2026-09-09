package controllers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"Troot0Fobia/samar/helpers"
)

const (
	hubTailBufSize = 512 * 1024 // 512 KB tail buffer for late joiners
	hubSubChanBuf  = 256        // per-subscriber channel capacity
)

// audioMeta describes a managed stream's audio track for the audio WebSocket
// and the /audio_info availability probe. Codec: "pcm16" (little-endian s16
// mono, from G.711 decode or native s16le), "aac" (ADTS frames, browser
// decodes), or "none" (no track / unrecognised codec).
type audioMeta struct {
	Codec    string `json:"codec"`
	Rate     int    `json:"sampleRate"`
	Channels int    `json:"channels"`
}

// managedStream holds a single upstream connection (ffmpeg) shared by all
// viewers of the same camera channel.
type managedStream struct {
	cancel  context.CancelFunc
	mu      sync.Mutex
	subs    map[chan []byte]struct{}
	tailBuf []byte
	refs    atomic.Int32  // accessed via Add/Load; no external lock needed
	done    chan struct{} // closed by closeAll when the stream ends

	// Audio fan-out — no tailBuf (PCM/ADTS need no init segment). Populated
	// only by the Dahua start fn; Hikvision/RTSP leave it empty.
	audioMu    sync.Mutex
	audioSubs  map[chan []byte]struct{}
	audioMeta  atomic.Pointer[audioMeta]
	audioReady chan struct{} // closed once audioMeta is first published
	audioOnce  sync.Once
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
	// Round the cut UP to the nearest TS packet so the buffer never exceeds hubTailBufSize.
	ms.tailBuf = append(ms.tailBuf, chunk...)
	if len(ms.tailBuf) > hubTailBufSize {
		cut := ((len(ms.tailBuf) - hubTailBufSize + 187) / 188) * 188
		if cut >= len(ms.tailBuf) {
			ms.tailBuf = ms.tailBuf[:0]
		} else {
			ms.tailBuf = ms.tailBuf[cut:]
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

// ─── audio fan-out ───────────────────────────────────────────────────────────

func (ms *managedStream) subscribeAudio() chan []byte {
	ch := make(chan []byte, hubSubChanBuf)
	ms.audioMu.Lock()
	defer ms.audioMu.Unlock()
	select {
	case <-ms.done:
		close(ch)
		return ch
	default:
	}
	ms.audioSubs[ch] = struct{}{}
	return ch
}

func (ms *managedStream) unsubscribeAudio(ch chan []byte) {
	ms.audioMu.Lock()
	delete(ms.audioSubs, ch)
	ms.audioMu.Unlock()
}

func (ms *managedStream) broadcastAudio(data []byte) {
	ms.audioMu.Lock()
	defer ms.audioMu.Unlock()
	// The audio pump runs for the whole stream lifetime even with no
	// listeners (so /audio_info can answer) — skip the copy when nobody is
	// subscribed. Unlike broadcast() there's no tailBuf to keep filled.
	if len(ms.audioSubs) == 0 {
		return
	}
	chunk := make([]byte, len(data))
	copy(chunk, data)
	for ch := range ms.audioSubs {
		select {
		case ch <- chunk:
		default:
			// slow consumer — drop the frame
		}
	}
}

// setAudioMeta publishes the audio track description exactly once.
func (ms *managedStream) setAudioMeta(codec string, rate, channels int) {
	ms.audioOnce.Do(func() {
		ms.audioMeta.Store(&audioMeta{Codec: codec, Rate: rate, Channels: channels})
		close(ms.audioReady)
	})
}

// waitAudioMeta blocks until the audio track description is known, ctx is
// cancelled, the stream ends, or timeout elapses (returns nil in the latter
// three cases).
func (ms *managedStream) waitAudioMeta(ctx context.Context, timeout time.Duration) *audioMeta {
	select {
	case <-ms.audioReady:
		return ms.audioMeta.Load()
	case <-ctx.Done():
		return nil
	case <-ms.done:
		return nil
	case <-time.After(timeout):
		return nil
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

	ms.audioMu.Lock()
	for ch := range ms.audioSubs {
		close(ch)
		delete(ms.audioSubs, ch)
	}
	ms.audioMu.Unlock()
}

// cinemaStreamHub manages a shared managed stream per unique stream key.
type cinemaStreamHub struct {
	mu      sync.Mutex
	streams map[string]*managedStream
}

var globalHub = &cinemaStreamHub{streams: make(map[string]*managedStream)}

// join returns the existing managed stream for key, or creates a new one and
// starts startFn in a goroutine. The caller must call leave when done.
func (h *cinemaStreamHub) join(key string, startFn func(ctx context.Context, ms *managedStream)) *managedStream {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ms, ok := h.streams[key]; ok {
		ms.refs.Add(1)
		return ms
	}

	ctx, cancel := context.WithCancel(context.Background())
	ms := &managedStream{
		cancel:     cancel,
		subs:       make(map[chan []byte]struct{}),
		done:       make(chan struct{}),
		audioSubs:  make(map[chan []byte]struct{}),
		audioReady: make(chan struct{}),
	}
	ms.refs.Store(1)
	h.streams[key] = ms

	go func() {
		startFn(ctx, ms)
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
	if ms.refs.Add(-1) > 0 {
		return
	}
	// refs hit zero: remove from hub so the next viewer gets a fresh stream.
	h.mu.Lock()
	if h.streams[key] == ms {
		delete(h.streams, key)
	}
	h.mu.Unlock()
	ms.cancel()
}

// audioMetaFor returns the audio track description for a live stream, or nil if
// no stream is running under key or its audio format is not yet known. It does
// not touch the ref count — used by the /audio_info availability probe.
func (h *cinemaStreamHub) audioMetaFor(key string) *audioMeta {
	h.mu.Lock()
	ms, ok := h.streams[key]
	h.mu.Unlock()
	if !ok {
		return nil
	}
	return ms.audioMeta.Load()
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// transcodeSem caps concurrent ffmpeg jobs that run a libx264 encode loop
// (HEVC / MJPEG sources, and RTSP HEVC). H.264 sources use -c:v copy and cost
// almost nothing, so they never take a slot. Without this an operator opening
// a dozen HEVC channels spawns a dozen encoders and starves the whole process.
//
// Each transcode is itself bounded (see transcodeThreads/transcodeScaleFilter
// below) so a single job can no longer claim every core or encode at full
// camera resolution — a Cinema tile is small on screen, so encoding at source
// resolution (often 2K/4K on Dahua mains streams) was pure waste. With a
// bounded per-job footprint the process can safely admit more concurrent
// slots for the same core budget than before.
var transcodeSem = make(chan struct{}, transcodeCap())

// transcodeThreads caps libx264's own thread count per ffmpeg process.
// Left unset, ffmpeg defaults to one thread per core, so N concurrent
// transcodes oversubscribe the machine N-fold and thrash instead of
// completing — this is what actually starved the process at high channel
// counts, not merely the number of concurrent jobs.
const transcodeThreads = "2"

// transcodeScaleFilter caps encode resolution at 1280px wide (height keeps
// aspect ratio, forced even via -2) without upscaling smaller sources.
// libx264 cost scales with pixel count, so this is the single biggest lever
// on per-job CPU cost — a Cinema grid tile never needs source resolution.
// The comma inside min(...) is escaped because ffmpeg's filtergraph syntax
// otherwise reads it as a filter separator.
const transcodeScaleFilter = "scale=min(1280\\,iw):-2"

func transcodeCap() int {
	if v := os.Getenv("CINEMA_MAX_TRANSCODES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return min(max(runtime.GOMAXPROCS(0), 4), 12)
}

// acquireTranscodeSlot blocks until a transcode slot is free or ctx is done.
// Returns a release func (nil if ctx was cancelled — caller should abort).
func acquireTranscodeSlot(ctx context.Context, tag string) (release func(), ok bool) {
	select {
	case transcodeSem <- struct{}{}:
		return func() { <-transcodeSem }, true
	default:
	}
	helpers.LogError("cinema ffmpeg transcode", tag, "all transcode slots busy — waiting")
	select {
	case transcodeSem <- struct{}{}:
		return func() { <-transcodeSem }, true
	case <-ctx.Done():
		return nil, false
	}
}

// runFFmpegBroadcast runs ffmpeg to remux/transcode the camera video into
// MPEG-TS and fans the output to hub subscribers. Video only — Dahua audio
// travels its own PCM WebSocket, RTSP audio is muxed by WsCinemaRTSP's own
// ffmpeg, Hikvision native has none.
func runFFmpegBroadcast(ctx context.Context, stream io.Reader, codec, tag string, broadcast func([]byte)) {
	var args []string
	switch codec {
	case "mjpeg":
		// browsers can't play MJPEG in MSE — transcode to H.264.
		args = []string{
			"-loglevel", "warning",
			"-f", "mjpeg", "-i", "pipe:0",
			"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
			"-threads", transcodeThreads, "-vf", transcodeScaleFilter, "-r", "15", "-g", "15",
			"-an", "-f", "mpegts", "pipe:1",
		}
	case "hevc":
		// HEVC must be transcoded to H.264: browsers do not support HEVC in MSE.
		args = []string{
			"-loglevel", "warning",
			"-f", "hevc", "-i", "pipe:0",
			"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
			"-threads", transcodeThreads, "-vf", transcodeScaleFilter, "-r", "25", "-g", "25",
			"-an", "-f", "mpegts", "pipe:1",
		}
	default:
		// H.264: stream copy — no decode/encode loop, trivial CPU cost.
		// -r 25 before -i tells the raw demuxer to synthesise timestamps at
		// 25 fps (Dahua cameras send no timing info in the bitstream itself).
		args = []string{
			"-loglevel", "warning",
			"-r", "25", "-f", "h264", "-i", "pipe:0",
			"-c:v", "copy",
			"-an", "-f", "mpegts", "pipe:1",
		}
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdin = stream

	ffmpegOut, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	ffmpegErr, err := cmd.StderrPipe()
	if err != nil {
		return
	}

	if codec == "hevc" || codec == "mjpeg" {
		release, ok := acquireTranscodeSlot(ctx, tag)
		if !ok {
			return
		}
		defer release()
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

// wsWriteTimeout is the per-frame deadline for writing to a WebSocket client.
// If the OS TCP send buffer stays full for longer than this (e.g. the client
// has stalled), the write returns an error and the goroutine exits cleanly
// rather than blocking indefinitely. ctx.Done() alone cannot unblock a stuck
// net.Conn.Write; only setting a deadline does.
// 30 s gives background browser tabs enough time to drain their receive buffers
// when they are throttled by the browser's Page Visibility policy.
const wsWriteTimeout = 30 * time.Second

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
			conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) //nolint:errcheck
			if err := wsSendBinaryFrame(conn, data); err != nil {
				return
			}
		}
	}
}
