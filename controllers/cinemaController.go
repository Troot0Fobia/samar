package controllers

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"Troot0Fobia/samar/cinema"
	"Troot0Fobia/samar/helpers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/middleware"
	"Troot0Fobia/samar/models"

	"github.com/gin-gonic/gin"
)

// ─── SSE event types ──────────────────────────────────────────────────────────

type cinemaCamEvent struct {
	Type     string      `json:"type"`
	Index    uint        `json:"index"`
	Host     string      `json:"host"`
	Name     string      `json:"name,omitempty"`
	Status   string      `json:"status"`
	Model    string      `json:"model,omitempty"`
	Address  string      `json:"address,omitempty"`
	IP       string      `json:"ip,omitempty"`
	Port     string      `json:"port,omitempty"`
	Channels []cinemaCh  `json:"channels,omitempty"`
}

type cinemaCh struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type cinemaRTSPEvt struct {
	Type    string `json:"type"`
	Index   uint   `json:"index"`
	Name    string `json:"name,omitempty"`
	Status  string `json:"status"`
	Address string `json:"address,omitempty"`
	Link    string `json:"link,omitempty"`
	IP      string `json:"ip,omitempty"`
	Port    string `json:"port,omitempty"`
}

type cinemaRTSPChsEvt struct {
	Type     string         `json:"type"`
	Index    uint           `json:"index"`
	Channels []cinemaRTSPCh `json:"channels"`
}

type cinemaRTSPCh struct {
	Idx    int    `json:"idx"`
	Label  string `json:"label"`
	Codec  string `json:"codec"`
	URL    string `json:"url"`
	Status string `json:"status,omitempty"`
}

// ─── Page handler ─────────────────────────────────────────────────────────────

func GetCinemaPage(c *gin.Context) {
	_, role, _ := middleware.CheckAuth(c)
	c.HTML(http.StatusOK, "cinema.html", gin.H{
		"isModer": role == "moderator" || role == "admin",
		"isAdmin": role == "admin",
	})
}

// ─── SSE ─────────────────────────────────────────────────────────────────────

// maxCinemaIDs caps the number of cameras probed per SSE request.
// Each ID spawns a goroutine that opens a TCP connection, so a large value
// is a DoS amplifier.
const maxCinemaIDs = 50

// CinemaEventStream serves GET /api/cinema/events?ids=1,2,3
func CinemaEventStream(c *gin.Context) {
	ids := parseIDsParam(c.Query("ids"))
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no ids"})
		return
	}
	if len(ids) > maxCinemaIDs {
		ids = ids[:maxCinemaIDs]
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	var cams []models.Camera
	initializers.DB.Where("id IN ?", ids).Find(&cams)

	events := make(chan string, 64)
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	var wg sync.WaitGroup
	for _, cam := range cams {
		wg.Add(1)
		go func(cam models.Camera) {
			defer wg.Done()
			if cam.Link != "" {
				probeRTSPCinema(ctx, cam, events)
			} else {
				probeDahuaCinema(ctx, cam, events)
			}
		}(cam)
	}

	// After all probes finish, keep the SSE connection open with periodic
	// keepalives so the browser EventSource never auto-reconnects.
	go func() {
		wg.Wait()
		tick := time.NewTicker(25 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				close(events)
				return
			case <-tick.C:
				select {
				case events <- `{"type":"ping"}`:
				case <-ctx.Done():
					close(events)
					return
				}
			}
		}
	}()

	for {
		select {
		case msg, ok := <-events:
			if !ok {
				return
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", msg)
			c.Writer.Flush()
		case <-ctx.Done():
			return
		}
	}
}

// probeDahuaCinema connects to a Dahua camera via DVRIP, discovers channels,
// and sends SSE events. Cached channels from DB are sent first if available.
func probeDahuaCinema(ctx context.Context, cam models.Camera, events chan<- string) {
	host := net.JoinHostPort(cam.IP, cam.Port)
	if cam.Port == "" || cam.Port == "0" {
		host = net.JoinHostPort(cam.IP, "37777")
	}
	tag := fmt.Sprintf("cinema cam=%d (%s)", cam.ID, host)

	send := func(ev any) {
		if ctx.Err() != nil {
			return
		}
		data, _ := json.Marshal(ev)
		select {
		case events <- string(data):
		case <-ctx.Done():
		}
	}

	// Send cached channels immediately if available
	if cam.Channels != "" {
		var cached []cinemaCh
		if err := json.Unmarshal([]byte(cam.Channels), &cached); err == nil && len(cached) > 0 {
			send(cinemaCamEvent{
				Type:     "camera",
				Index:    cam.ID,
				Host:     host,
				Name:     cam.Name,
				Status:   "cached",
				Address:  cam.Address,
				IP:       cam.IP,
				Port:     cam.Port,
				Channels: cached,
			})
		} else {
			send(cinemaCamEvent{Type: "camera", Index: cam.ID, Host: host, Name: cam.Name, Status: "connecting", Address: cam.Address, IP: cam.IP, Port: cam.Port})
		}
	} else {
		send(cinemaCamEvent{Type: "camera", Index: cam.ID, Host: host, Name: cam.Name, Status: "connecting", Address: cam.Address, IP: cam.IP, Port: cam.Port})
	}

	if ctx.Err() != nil {
		return
	}

	client, err := cinema.NewClient(host, cam.Login, cam.Password, tag)
	if err != nil {
		helpers.LogError("cinema dahua connect", tag, err.Error())
		send(cinemaCamEvent{Type: "camera", Index: cam.ID, Host: host, Name: cam.Name, Status: "offline", Address: cam.Address, IP: cam.IP, Port: cam.Port})
		return
	}
	defer client.Close()

	send(cinemaCamEvent{Type: "camera", Index: cam.ID, Host: host, Name: cam.Name, Status: "authed", Address: cam.Address, IP: cam.IP, Port: cam.Port})

	if ctx.Err() != nil {
		return
	}

	model, _, _ := client.DeviceInfo()
	raw := client.ListChannels()

	var chs []cinemaCh
	for _, ch := range raw {
		if ch.SubType != 0 {
			continue
		}
		chs = append(chs, cinemaCh{Index: ch.Index, Name: ch.Name, State: ch.ConnectionState})
	}

	send(cinemaCamEvent{
		Type:     "camera",
		Index:    cam.ID,
		Host:     host,
		Name:     cam.Name,
		Status:   "online",
		Model:    model,
		Address:  cam.Address,
		IP:       cam.IP,
		Port:     cam.Port,
		Channels: chs,
	})

	// Cache channels to DB
	if chJSON, err := json.Marshal(chs); err == nil {
		initializers.DB.Model(&cam).Update("channels", string(chJSON))
	}
}

// probeRTSPCinema probes an RTSP camera, discovers channels, and sends SSE events.
func probeRTSPCinema(ctx context.Context, cam models.Camera, events chan<- string) {
	rawURL := buildRTSPURL(cam)
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	name := u.Host + u.EscapedPath()
	if u.RawQuery != "" {
		name += "?" + u.RawQuery
	}

	send := func(ev any) {
		if ctx.Err() != nil {
			return
		}
		data, _ := json.Marshal(ev)
		select {
		case events <- string(data):
		case <-ctx.Done():
		}
	}

	mode := cinema.DetectRTSPMode(rawURL)

	// rawURL already has credentials injected by buildRTSPURL; send it as Link
	// so the frontend can offer "copy full RTSP address" including credentials.
	// name (used for display) strips credentials via url.Parse.
	send(cinemaRTSPEvt{Type: "rtsp", Index: cam.ID, Name: name, Status: "checking", Address: cam.Address, Link: rawURL, IP: cam.IP, Port: cam.Port})

	if ctx.Err() != nil {
		return
	}

	switch mode {
	case cinema.RTSPModeTemplate:
		expanded, _ := cinema.ExpandTemplate(rawURL)
		if len(expanded) == 0 {
			send(cinemaRTSPEvt{Type: "rtsp", Index: cam.ID, Name: name, Status: "offline", Address: cam.Address})
			send(cinemaRTSPChsEvt{Type: "rtspchannels", Index: cam.ID, Channels: []cinemaRTSPCh{}})
			return
		}

		// Probe each channel status concurrently
		var mu sync.Mutex
		channels := make([]cinema.RTSPChannel, len(expanded))
		copy(channels, expanded)

		var chWg sync.WaitGroup
		sem := make(chan struct{}, 20)
		for j := range channels {
			chWg.Add(1)
			go func(j int) {
				defer chWg.Done()
				if ctx.Err() != nil {
					return
				}
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()
				_, status, err := cinema.RtspDescribe(channels[j].URL, 5*time.Second)
				mu.Lock()
				// Any server response except 404 means the channel endpoint exists.
				// 401/403 happen when DESCRIBE is auth-protected but the stream still works.
				if err == nil && status != 0 && status != 404 {
					channels[j].Status = "online"
				} else {
					channels[j].Status = "offline"
				}
				mu.Unlock()
			}(j)
		}
		chWg.Wait()

		online, offline := 0, 0
		chs := make([]cinemaRTSPCh, len(channels))
		for i, ch := range channels {
			chs[i] = cinemaRTSPCh{
				Idx:    i,
				Label:  ch.Label,
				Codec:  ch.Codec,
				URL:    cinema.StripRTSPCreds(ch.URL),
				Status: ch.Status,
			}
			if ch.Status == "online" {
				online++
			} else {
				offline++
			}
		}

		overall := "offline"
		if online > 0 && offline == 0 {
			overall = "online"
		} else if online > 0 {
			overall = "partial"
		}

		send(cinemaRTSPEvt{Type: "rtsp", Index: cam.ID, Name: name, Status: overall, Address: cam.Address})
		send(cinemaRTSPChsEvt{Type: "rtspchannels", Index: cam.ID, Channels: chs})

	case cinema.RTSPModeAuto:
		if !cinema.ProbeRTSP(rawURL) {
			send(cinemaRTSPEvt{Type: "rtsp", Index: cam.ID, Name: name, Status: "offline", Address: cam.Address})
			send(cinemaRTSPChsEvt{Type: "rtspchannels", Index: cam.ID, Channels: []cinemaRTSPCh{}})
			return
		}
		send(cinemaRTSPEvt{Type: "rtsp", Index: cam.ID, Name: name, Status: "online", Address: cam.Address})

		channels := cinema.EnumerateRTSPChannels(ctx, rawURL)
		chs := make([]cinemaRTSPCh, len(channels))
		for i, ch := range channels {
			chs[i] = cinemaRTSPCh{
				Idx:    i,
				Label:  ch.Label,
				Codec:  ch.Codec,
				URL:    cinema.StripRTSPCreds(ch.URL),
				Status: ch.Status,
			}
		}
		send(cinemaRTSPChsEvt{Type: "rtspchannels", Index: cam.ID, Channels: chs})

		// Cache discovered channels to DB
		if len(channels) > 0 {
			if chJSON, err := json.Marshal(channels); err == nil {
				initializers.DB.Model(&cam).Update("channels", string(chJSON))
			}
		}

	case cinema.RTSPModeSingle:
		st := "offline"
		if cinema.ProbeRTSP(rawURL) {
			st = "online"
		}
		send(cinemaRTSPEvt{Type: "rtsp", Index: cam.ID, Name: name, Status: st, Address: cam.Address})
		send(cinemaRTSPChsEvt{Type: "rtspchannels", Index: cam.ID, Channels: []cinemaRTSPCh{
			{Idx: 0, Label: cinema.ChannelLabel(u.Path), URL: cinema.StripRTSPCreds(rawURL), Status: st},
		}})
	}
}

// ─── WebSocket — Dahua ────────────────────────────────────────────────────────

// WsCinemaDahua serves WS /ws/cinema/dahua/:id/:ch
func WsCinemaDahua(c *gin.Context) {
	id, err1 := strconv.ParseUint(c.Param("id"), 10, 64)
	ch, err2 := strconv.Atoi(c.Param("ch"))
	// ch must be a non-negative channel index within a sane range (0-63).
	// Negative values would produce a negative slot index in openStreamBinary,
	// causing a runtime panic (index out of range) that kills the process.
	if err1 != nil || err2 != nil || ch < 0 || ch > 63 {
		c.String(http.StatusBadRequest, "bad params")
		return
	}

	var cam models.Camera
	if err := initializers.DB.First(&cam, id).Error; err != nil {
		c.String(http.StatusNotFound, "camera not found")
		return
	}

	host := net.JoinHostPort(cam.IP, cam.Port)
	if cam.Port == "" || cam.Port == "0" {
		host = net.JoinHostPort(cam.IP, "37777")
	}
	tag := fmt.Sprintf("cinema ws dahua=%d (%s)", cam.ID, host)

	conn, err := wsUpgradeCinema(c.Writer, c.Request)
	if err != nil {
		helpers.LogError("cinema dahua ws upgrade", tag, err.Error())
		return
	}
	defer conn.Close()
	helpers.LogSuccess(fmt.Sprintf("[%s] WS connected ch=%d", tag, ch), tag)

	key := fmt.Sprintf("dahua:%d:%d", cam.ID, ch)

	// Capture values for the goroutine closure.
	camIP, camPort, camLogin, camPassword := cam.IP, cam.Port, cam.Login, cam.Password

	ms := globalHub.join(key, func(ctx context.Context, broadcast func([]byte)) {
		hubHost := net.JoinHostPort(camIP, camPort)
		if camPort == "" || camPort == "0" {
			hubHost = net.JoinHostPort(camIP, "37777")
		}
		client, err := cinema.NewClient(hubHost, camLogin, camPassword, tag)
		if err != nil {
			helpers.LogError("cinema dahua connect", tag, err.Error())
			return
		}
		defer client.Close()

		stream, err := client.OpenStream(ch, 0)
		if err != nil {
			helpers.LogError("cinema dahua open stream", tag, err.Error())
			return
		}
		defer stream.Close()

		codec, err := stream.PeekFirstFrame()
		if err != nil {
			helpers.LogError("cinema dahua peek frame", tag, err.Error())
			return
		}

		runFFmpegBroadcast(ctx, stream, codec, tag, broadcast)
	})
	defer globalHub.leave(key, ms)

	subCh, initData := ms.subscribe()
	defer ms.unsubscribe(subCh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Watch for client disconnect in the background (proper WS frame reader).
	go wsReadLoop(conn, cancel)

	// Send buffered tail to late joiners so they receive an init segment.
	if len(initData) > 0 {
		conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) //nolint:errcheck
		wsSendBinaryFrame(conn, initData)                     //nolint:errcheck
		conn.SetWriteDeadline(time.Time{})                    //nolint:errcheck
	}

	pumpSubToWS(ctx, conn, subCh)
}

// ─── WebSocket — RTSP ─────────────────────────────────────────────────────────

// WsCinemaRTSP serves WS /ws/cinema/rtsp/:id and /ws/cinema/rtsp/:id/:chIdx
func WsCinemaRTSP(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.String(http.StatusBadRequest, "bad id")
		return
	}

	var cam models.Camera
	if err := initializers.DB.First(&cam, id).Error; err != nil {
		c.String(http.StatusNotFound, "camera not found")
		return
	}

	rawURL := buildRTSPURL(cam)
	if rawURL == "" {
		// Dahua cameras (cam.Link == "") must use the Dahua WS endpoint, not RTSP.
		c.String(http.StatusBadRequest, "camera has no RTSP link")
		return
	}
	tag := fmt.Sprintf("cinema ws rtsp=%d", cam.ID)

	chIdxParam := c.Param("chIdx")
	if chIdxParam != "" {
		chIdx, err := strconv.Atoi(chIdxParam)
		if err != nil || chIdx < 0 {
			c.String(http.StatusBadRequest, "bad chIdx")
			return
		}
		rawURL = resolveRTSPChannel(cam, rawURL, chIdx)
	}

	conn, err := wsUpgradeCinema(c.Writer, c.Request)
	if err != nil {
		helpers.LogError("cinema rtsp ws upgrade", tag, err.Error())
		return
	}
	defer conn.Close()

	key := fmt.Sprintf("rtsp:%d:%s", cam.ID, chIdxParam)

	ms := globalHub.join(key, func(ctx context.Context, broadcast func([]byte)) {
		ffmpegArgs := []string{
			"-loglevel", "warning",
			"-rtsp_transport", "tcp",
			"-i", rawURL,
			"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
			"-r", "25", "-g", "25", "-an",
			"-f", "mpegts", "pipe:1",
		}
		cmd := exec.CommandContext(ctx, "ffmpeg", ffmpegArgs...)

		ffmpegOut, err := cmd.StdoutPipe()
		if err != nil {
			return
		}
		ffmpegErr, err := cmd.StderrPipe()
		if err != nil {
			return
		}
		if err := cmd.Start(); err != nil {
			helpers.LogError("cinema rtsp ffmpeg start", tag, err.Error())
			return
		}
		helpers.LogSuccess(fmt.Sprintf("[%s] ffmpeg hub started (rtsp)", tag), tag)
		go func() {
			sc := bufio.NewScanner(ffmpegErr)
			for sc.Scan() {
				helpers.LogError("cinema rtsp ffmpeg", tag, sc.Text())
			}
		}()

		buf := make([]byte, 188*128)
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
		helpers.LogSuccess(fmt.Sprintf("[%s] ffmpeg hub stopped (rtsp)", tag), tag)
	})
	defer globalHub.leave(key, ms)

	subCh, initData := ms.subscribe()
	defer ms.unsubscribe(subCh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Disconnect detection — cancels ctx so pumpSubToWS exits.
	go wsReadLoop(conn, cancel)

	// Send buffered tail to late joiners so they receive an init segment.
	if len(initData) > 0 {
		conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) //nolint:errcheck
		wsSendBinaryFrame(conn, initData)                     //nolint:errcheck
		conn.SetWriteDeadline(time.Time{})                    //nolint:errcheck
	}

	pumpSubToWS(ctx, conn, subCh)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func parseIDsParam(s string) []uint {
	var ids []uint
	for p := range strings.SplitSeq(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err == nil && n > 0 {
			ids = append(ids, uint(n))
		}
	}
	return ids
}

func buildRTSPURL(cam models.Camera) string {
	if cam.Link == "" {
		return ""
	}
	u, err := url.Parse(cam.Link)
	if err != nil {
		return cam.Link
	}
	if u.User == nil && cam.Login != "" {
		u.User = url.UserPassword(cam.Login, cam.Password)
	}
	return u.String()
}

// resolveRTSPChannel returns the concrete RTSP URL for the given channel index.
func resolveRTSPChannel(cam models.Camera, rawURL string, chIdx int) string {
	mode := cinema.DetectRTSPMode(rawURL)

	switch mode {
	case cinema.RTSPModeTemplate:
		channels, ok := cinema.ExpandTemplate(rawURL)
		if ok && chIdx >= 0 && chIdx < len(channels) {
			return channels[chIdx].URL
		}
	case cinema.RTSPModeAuto:
		if cam.Channels != "" {
			var cached []cinema.RTSPChannel
			if err := json.Unmarshal([]byte(cam.Channels), &cached); err == nil {
				if chIdx >= 0 && chIdx < len(cached) {
					return cached[chIdx].URL
				}
			}
		}
	}
	return rawURL
}

// wsUpgradeCinema performs the WebSocket handshake.
// It validates the Upgrade header and Origin to prevent cross-site WebSocket
// hijacking (CSWSH): any browser-initiated WS from a foreign origin would
// carry the session cookie under SameSite=Lax, so an origin check is the
// server-side defence layer in addition to the browser policy.
func wsUpgradeCinema(w http.ResponseWriter, r *http.Request) (net.Conn, error) {
	// RFC 6455 §4.1: Upgrade must equal "websocket" (case-insensitive).
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "not a websocket upgrade", http.StatusBadRequest)
		return nil, fmt.Errorf("missing Upgrade: websocket")
	}

	// Origin validation — reject requests whose Origin doesn't match the
	// server's own host.  Non-browser clients (curl, ffplay) omit Origin
	// entirely, so we only reject a present but mismatched value.
	if origin := r.Header.Get("Origin"); origin != "" {
		ou, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(ou.Host, r.Host) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return nil, fmt.Errorf("origin mismatch: %q vs host %q", ou.Host, r.Host)
		}
	}

	key := r.Header.Get("Sec-Websocket-Key")
	if key == "" {
		http.Error(w, "not a websocket upgrade", http.StatusBadRequest)
		return nil, fmt.Errorf("missing Sec-Websocket-Key")
	}
	sum    := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return nil, fmt.Errorf("hijack unsupported")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(rw,
		"HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: %s\r\n\r\n",
		accept)
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// wsSendBinaryFrame sends a binary WebSocket frame.
func wsSendBinaryFrame(conn net.Conn, data []byte) error {
	n := len(data)
	var hdr []byte
	switch {
	case n < 126:
		hdr = []byte{0x82, byte(n)}
	case n < 65536:
		hdr = []byte{0x82, 126, byte(n >> 8), byte(n)}
	default:
		hdr = []byte{0x82, 127, 0, 0, 0, 0, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	}
	if _, err := conn.Write(hdr); err != nil {
		return err
	}
	_, err := conn.Write(data)
	return err
}

// wsReadLoop reads WebSocket frames from the client, properly parsing frame
// headers per RFC 6455.  It calls cancel when a CLOSE frame (opcode 0x8) is
// received or when any read error occurs (TCP disconnect, timeout, etc.).
//
// Browser→server frames are always masked; the masking key (4 bytes) is
// counted as part of the payload skip so we never need to demask.
func wsReadLoop(conn net.Conn, cancel context.CancelFunc) {
	hdr := make([]byte, 2)
	for {
		if _, err := io.ReadFull(conn, hdr); err != nil {
			cancel()
			return
		}
		opcode := hdr[0] & 0x0F
		masked := hdr[1]&0x80 != 0
		plen   := int64(hdr[1] & 0x7F)

		switch plen {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(conn, ext[:]); err != nil {
				cancel()
				return
			}
			plen = int64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(conn, ext[:]); err != nil {
				cancel()
				return
			}
			plen = int64(binary.BigEndian.Uint64(ext[:]))
		}
		if masked {
			plen += 4 // masking key is prepended to the payload bytes on the wire
		}
		if plen > 0 {
			if _, err := io.CopyN(io.Discard, conn, plen); err != nil {
				cancel()
				return
			}
		}
		if opcode == 0x8 { // CLOSE
			cancel()
			return
		}
	}
}

