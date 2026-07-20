package cinema

import (
	"bufio"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ─── Hikvision native protocol ────────────────────────────────────────────────
//
// Reverse-engineered from live Burp Suite + Wireshark captures of the stock
// Hikvision web UI (firmware V4.0.1build180929) talking to itself — not from
// any SDK documentation. Two independent pieces:
//
//  1. ISAPI sessionLogin (HTTP, port 80/443) — auth only. Verified byte-for-byte
//     against a captured real login: our computed hash matched the exact value
//     the browser sent, for a known username/password pulled from the camera DB.
//
//  2. GET /SDK/play (HTTP, same port) — the actual video transport. This is the
//     legacy "NetSDK-over-HTTP" mechanism the ActiveX/NPAPI plugin era client
//     uses instead of RTSP: a GET request carrying a 44-byte binary command
//     body, authenticated via `Cookie: WebSession=<id>` (the sessionID returned
//     by sessionLogin's POST response body — NOT a Set-Cookie header). The
//     response is an indefinite `Content-Type: Opaque/data` stream framed in
//     small chunks that repackage H.264 NAL units using the standard RTP H.264
//     payload format (RFC 6184: Single NAL Unit / FU-A), just carried over TCP
//     instead of actual RTP/UDP. Verified by reconstructing Annex-B H.264 from
//     a full captured session and decoding real, uncorrupted video frames with
//     ffmpeg (visible on-screen timestamp + "Camera 03" OSD confirmed it was
//     real footage, not noise).
//
// ─── Chunk framing observed on the wire ──────────────────────────────────────
//
//	offset 0:  0x03 0x00            — chunk marker (constant)
//	offset 2:  uint16 little-endian — TOTAL chunk length, measured from offset 0
//	offset 4:  4 bytes              — per-chunk sequence number (increments by 1)
//	offset 8:  8 bytes              — constant per-connection stream tag
//	offset 16: (length-16) bytes    — RTP-style H.264 payload (see below)
//
// The declared length does not always land exactly on the next chunk marker
// (observed drift up to ~60 bytes on some chunks, cause unconfirmed — possibly
// a variable trailing metadata field). readChunk() treats the declared length
// as a hint and resyncs defensively: if the next two bytes aren't 0x03 0x00,
// it scans forward a bounded distance for a position whose own declared length
// ALSO lands on a further 0x03 0x00 (two-deep validation, to avoid locking onto
// an incidental 0x03 0x00 byte pair inside compressed video data), and folds
// the skipped bytes into whatever NAL is currently being reassembled.
//
// RTP H.264 payload (RFC 6184), only the two modes actually observed:
//   - Single NAL Unit: payload[0]&0x1F is 1-23 → the whole payload is one
//     complete NAL, used as-is (SPS/PPS chunks in the capture were unfragmented).
//   - FU-A (type 28): payload[0] is the FU indicator, payload[1] is the FU
//     header (S|E|R|type bits). Reassemble by concatenating fragments across
//     chunks; reconstructed NAL header = (FU_indicator & 0xE0) | (FU header & 0x1F).
//
// HEVC (H.265) has also been observed through this pathway on some
// devices/channels — RFC 7798 framing (2-byte NAL header, VPS/SPS/PPS/SEI/FU
// types 32/33/34/39/49), distinct from RFC 6184's H.264 framing seen on the
// device this was originally captured from. The codec isn't known ahead of
// time, so appendRTPPayload sniffs which framing applies per-stream before
// committing (see its doc comment).

// ─── sessionLogin (ISAPI) ─────────────────────────────────────────────────────

type hikSessionLoginCap struct {
	XMLName        xml.Name `xml:"SessionLoginCap"`
	SessionID      string   `xml:"sessionID"`
	Challenge      string   `xml:"challenge"`
	Iterations     int      `xml:"iterations"`
	IsIrreversible string   `xml:"isIrreversible"`
	Salt           string   `xml:"salt"`
}

type hikSessionUserCheck struct {
	XMLName      xml.Name `xml:"SessionUserCheck"`
	StatusValue  int      `xml:"statusValue"`
	StatusString string   `xml:"statusString"`
	SessionID    string   `xml:"sessionID"`
	LockStatus   string   `xml:"lockStatus"`
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// hikSessionLoginHash implements the device's own encodePwd() (javascripts/utils.js),
// verified byte-for-byte against a real captured login (see package comment above).
func hikSessionLoginHash(user, pass, challenge, salt string, iterations int, irreversible bool) string {
	if irreversible {
		a := sha256Hex(user + salt + pass)
		a = sha256Hex(a + challenge)
		for i := 2; i < iterations; i++ {
			a = sha256Hex(a)
		}
		return a
	}
	a := sha256Hex(pass) + challenge
	for i := 1; i < iterations; i++ {
		a = sha256Hex(a)
	}
	return a
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s)) //nolint:errcheck
	return b.String()
}

// ─── HikClient ─────────────────────────────────────────────────────────────────

type HikClient struct {
	addr       string // "ip:port"
	user, pass string
	httpClient *http.Client
	logger     *log.Logger

	mu         sync.Mutex
	webSession string

	// Digest fallback: some older firmware (verified live: DS-2CD1331-I,
	// DS-2CD2T42WD-I8) requires HTTP Digest on every ISAPI request, including
	// sessionLogin/capabilities itself — there is no WebSession for these at
	// all. digestMode is set once by login() after a 401+WWW-Authenticate,
	// and every subsequent request (ISAPI GETs and /SDK/play) computes a
	// fresh Authorization header instead of using the Cookie.
	digestMode   bool
	digestRealm  string
	digestNonce  string
	digestQop    string
	digestOpaque string
	digestNC     uint32

	// reloginMu serialises relogin() so a burst of concurrent 401s (multiple
	// viewers hitting an expired WebSession/stale nonce at once) triggers one
	// re-authentication instead of a stampede.
	reloginMu   sync.Mutex
	lastRelogin time.Time

	done      chan struct{}
	closeOnce sync.Once
}

// reloginCooldown bounds how often relogin() actually re-runs the login
// handshake; callers that lose the reloginMu race within this window just
// reuse whatever another goroutine already refreshed.
const reloginCooldown = 2 * time.Second

// relogin re-runs the login handshake to recover from an expired WebSession
// (cookie mode) or a stale Digest nonce (digest mode) — login() itself
// figures out which applies. Safe to call concurrently from multiple
// goroutines (doGet, dialPlay, heartbeatLoop) sharing this client.
func (c *HikClient) relogin() error {
	c.reloginMu.Lock()
	defer c.reloginMu.Unlock()
	if time.Since(c.lastRelogin) < reloginCooldown {
		return nil // another goroutine just refreshed the session
	}
	c.logger.Printf("[relogin] session expired, re-authenticating")
	if err := c.login(); err != nil {
		return err
	}
	c.lastRelogin = time.Now()
	return nil
}

func NewHikClient(addr, user, pass, tag string) (*HikClient, error) {
	c := &HikClient{
		addr: addr, user: user, pass: pass,
		// Cameras are addressed by raw IP, often behind self-signed or
		// unknown-CA certs (and some firmware redirects a plain http://
		// request to https:// on its own). Verifying that cert would just
		// break otherwise-working cameras for no security benefit — the
		// device's identity here is already pinned by IP/port + credentials,
		// not by CA trust.
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		},
		logger: log.New(cameraLogWriter(), "["+tag+"] ", log.LstdFlags),
		done:   make(chan struct{}),
	}
	if err := c.login(); err != nil {
		return nil, err
	}
	go c.heartbeatLoop()
	return c, nil
}

// Close stops the session-heartbeat goroutine. Safe to call multiple times.
func (c *HikClient) Close() {
	c.closeOnce.Do(func() { close(c.done) })
}

// heartbeatLoop periodically pings /ISAPI/Security/sessionHeartbeat, matching
// what the real web UI does (captured live: PUT with the WebSession cookie,
// empty body). Without it a long-idle WebSession may expire ISAPI-side and
// the device can drop the /SDK/play connection out from under an active
// viewer — this keeps the underlying auth session alive for as long as the
// client is in use, independent of how long any one video connection lasts.
func (c *HikClient) heartbeatLoop() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-tick.C:
			if c.isDigestMode() {
				continue // no WebSession to keep alive; every digest request re-authenticates itself
			}
			req, err := http.NewRequest(http.MethodPut,
				fmt.Sprintf("http://%s/ISAPI/Security/sessionHeartbeat", c.addr), nil)
			if err != nil {
				continue
			}
			req.Header.Set("Cookie", "WebSession="+c.session())
			resp, err := c.httpClient.Do(req)
			if err != nil {
				c.logger.Printf("[heartbeat] failed: %v", err)
				continue
			}
			c.logger.Printf("[heartbeat] HTTP %d", resp.StatusCode)
			resp.Body.Close()
			// Proactively refresh an expired WebSession here so the next
			// viewer request or reconnect doesn't have to hit a 401 first.
			if resp.StatusCode == http.StatusUnauthorized {
				if rerr := c.relogin(); rerr != nil {
					c.logger.Printf("[heartbeat] relogin after 401 failed: %v", rerr)
				}
			}
		}
	}
}

func (c *HikClient) login() error {
	capURL := fmt.Sprintf("http://%s/ISAPI/Security/sessionLogin/capabilities?username=%s",
		c.addr, url.QueryEscape(c.user))
	c.logger.Printf("[login] GET %s", capURL)
	resp, err := c.httpClient.Get(capURL)
	if err != nil {
		c.logger.Printf("[login] capabilities request failed: %v", err)
		return fmt.Errorf("capabilities: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		c.logger.Printf("[login] capabilities read failed (HTTP %d): %v", resp.StatusCode, err)
		return fmt.Errorf("capabilities read: %w", err)
	}
	c.logger.Printf("[login] capabilities response: HTTP %d, %d bytes", resp.StatusCode, len(body))

	if resp.StatusCode == http.StatusUnauthorized {
		return c.loginDigest(resp.Header.Values("WWW-Authenticate"))
	}

	var cap hikSessionLoginCap
	if err := xml.Unmarshal(body, &cap); err != nil {
		c.logger.Printf("[login] capabilities parse failed: %v, body=%q", err, previewBytes(body, 300))
		return fmt.Errorf("capabilities parse: %w", err)
	}
	if cap.SessionID == "" || cap.Challenge == "" {
		c.logger.Printf("[login] capabilities missing sessionID/challenge, body=%q", previewBytes(body, 300))
		return fmt.Errorf("capabilities: no sessionID/challenge (not a Hikvision ISAPI device?)")
	}

	iterations := cap.Iterations
	if iterations == 0 {
		iterations = 100
	}
	// Older firmware omits <isIrreversible> and <salt> entirely (bare
	// sessionLogin) and only understands the non-salted chained-SHA256
	// scheme — verified live: defaulting to "irreversible" with an empty
	// salt produced statusValue=401 for such a device, while the reversible
	// (no-salt) formula gave statusValue=200 with the same credentials.
	irreversible := cap.IsIrreversible == "true"
	c.logger.Printf("[login] capabilities parsed: iterations=%d irreversible=%v saltLen=%d challengeLen=%d",
		iterations, irreversible, len(cap.Salt), len(cap.Challenge))

	hash := hikSessionLoginHash(c.user, c.pass, cap.Challenge, cap.Salt, iterations, irreversible)

	loginXML := fmt.Sprintf(
		"<?xml version='1.0' encoding='UTF-8'?>"+
			"<SessionLogin><userName>%s</userName><password>%s</password>"+
			"<sessionID>%s</sessionID><isSessionIDValidLongTerm>false</isSessionIDValidLongTerm></SessionLogin>",
		xmlEscape(c.user), hash, cap.SessionID)

	loginURL := fmt.Sprintf("http://%s/ISAPI/Security/sessionLogin", c.addr)
	req, err := http.NewRequest(http.MethodPost, loginURL, strings.NewReader(loginXML))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")

	c.logger.Printf("[login] POST %s", loginURL)
	resp2, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Printf("[login] sessionLogin request failed: %v", err)
		return fmt.Errorf("sessionLogin: %w", err)
	}
	body2, err := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if err != nil {
		c.logger.Printf("[login] sessionLogin read failed (HTTP %d): %v", resp2.StatusCode, err)
		return fmt.Errorf("sessionLogin read: %w", err)
	}
	c.logger.Printf("[login] sessionLogin response: HTTP %d, %d bytes", resp2.StatusCode, len(body2))

	var check hikSessionUserCheck
	if err := xml.Unmarshal(body2, &check); err != nil {
		c.logger.Printf("[login] sessionLogin parse failed: %v, body=%q", err, previewBytes(body2, 300))
		return fmt.Errorf("sessionLogin parse: %w", err)
	}
	c.logger.Printf("[login] sessionLogin parsed: statusValue=%d statusString=%q lockStatus=%q sessionID=%q",
		check.StatusValue, check.StatusString, check.LockStatus, check.SessionID)
	if check.StatusValue != 200 {
		if strings.Contains(strings.ToLower(check.LockStatus), "lock") {
			return fmt.Errorf("sessionLogin: account locked")
		}
		return fmt.Errorf("sessionLogin failed: statusValue=%d %s", check.StatusValue, check.StatusString)
	}
	if check.SessionID == "" {
		return fmt.Errorf("sessionLogin: success but no WebSession id in response")
	}

	c.mu.Lock()
	c.webSession = check.SessionID
	c.mu.Unlock()
	c.logger.Printf("[login] ok, WebSession=%s", check.SessionID)
	return nil
}

// previewBytes returns a short, printable prefix of b for log lines — full
// device error pages can be large HTML documents that would drown the log.
func previewBytes(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}

func (c *HikClient) session() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.webSession
}

func (c *HikClient) isDigestMode() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.digestMode
}

// ─── Digest fallback (older firmware, no sessionLogin/WebSession at all) ──────

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func extractQuoted(s, key string) string {
	needle := key + `="`
	i := strings.Index(s, needle)
	if i < 0 {
		return ""
	}
	i += len(needle)
	j := strings.Index(s[i:], `"`)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}

// loginDigest handles the 401 fallback: parse the WWW-Authenticate challenge,
// switch the client into digest mode, then verify the credentials actually
// work by hitting a known ISAPI endpoint with a computed Authorization header.
func (c *HikClient) loginDigest(wwwAuth []string) error {
	var challenge string
	for _, h := range wwwAuth {
		if strings.HasPrefix(h, "Digest") {
			challenge = h
			break
		}
	}
	if challenge == "" {
		c.logger.Printf("[login] 401 without a Digest challenge, headers=%v", wwwAuth)
		return fmt.Errorf("sessionLogin unavailable and no Digest challenge offered")
	}

	realm := extractQuoted(challenge, "realm")
	nonce := extractQuoted(challenge, "nonce")
	if realm == "" || nonce == "" {
		return fmt.Errorf("digest challenge missing realm/nonce: %q", challenge)
	}

	c.mu.Lock()
	c.digestMode = true
	c.digestRealm = realm
	c.digestNonce = nonce
	c.digestQop = extractQuoted(challenge, "qop")
	c.digestOpaque = extractQuoted(challenge, "opaque")
	c.digestNC = 0
	c.mu.Unlock()
	c.logger.Printf("[login] digest challenge: realm=%q qop=%q", realm, c.digestQop)

	// Verify the credentials actually work — userCheck is the standard
	// unauthenticated-until-Digest ISAPI probe endpoint for this.
	body, status, err := c.digestRequest(http.MethodGet, "/ISAPI/Security/userCheck", nil)
	if err != nil {
		return fmt.Errorf("digest verify request failed: %w", err)
	}
	c.logger.Printf("[login] digest verify: HTTP %d, %d bytes", status, len(body))
	if status == http.StatusUnauthorized {
		return fmt.Errorf("digest verify failed: invalid credentials (HTTP 401)")
	}
	if status >= 400 {
		return fmt.Errorf("digest verify failed: HTTP %d, body=%q", status, previewBytes(body, 300))
	}
	c.logger.Printf("[login] ok (digest, realm=%q)", realm)
	return nil
}

// digestAuthHeader computes the Authorization header value for one request.
// nc must increment on every request reusing this client's cached challenge
// (RFC 2617 §3.2.2) — callers must not reuse a returned header for a second
// request.
func (c *HikClient) digestAuthHeader(method, uri string) string {
	c.mu.Lock()
	c.digestNC++
	nc := c.digestNC
	realm, nonce, qop, opaque := c.digestRealm, c.digestNonce, c.digestQop, c.digestOpaque
	c.mu.Unlock()

	ncStr := fmt.Sprintf("%08x", nc)
	cnonceBuf := make([]byte, 8)
	_, _ = rand.Read(cnonceBuf)
	cnonce := hex.EncodeToString(cnonceBuf)

	ha1 := md5Hex(c.user + ":" + realm + ":" + c.pass)
	ha2 := md5Hex(method + ":" + uri)

	var response string
	if qop != "" {
		response = md5Hex(ha1 + ":" + nonce + ":" + ncStr + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = md5Hex(ha1 + ":" + nonce + ":" + ha2)
	}

	hdr := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
		c.user, realm, nonce, uri, response)
	if qop != "" {
		hdr += fmt.Sprintf(`, qop=%s, nc=%s, cnonce="%s"`, qop, ncStr, cnonce)
	}
	if opaque != "" {
		hdr += fmt.Sprintf(`, opaque="%s"`, opaque)
	}
	return hdr
}

// digestRequest performs one Digest-authenticated HTTP request against path.
func (c *HikClient) digestRequest(method, path string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequest(method, fmt.Sprintf("http://%s%s", c.addr, path), body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", c.digestAuthHeader(method, path))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, err
}

// doGet performs one ISAPI GET, transparently relogging in and retrying once
// if the session/nonce turns out to be expired (HTTP 401) — a long-idle
// WebSession or a stale Digest nonce would otherwise fail every call from
// here on, since neither cookie nor digest mode re-authenticate on their own.
func (c *HikClient) doGet(path string) ([]byte, error) {
	body, status, err := c.doGetOnce(path)
	if err == nil && status == http.StatusUnauthorized {
		if rerr := c.relogin(); rerr != nil {
			return body, fmt.Errorf("session expired, relogin failed: %w", rerr)
		}
		body, status, err = c.doGetOnce(path)
	}
	if err == nil && status >= 400 {
		return body, fmt.Errorf("GET %s: HTTP %d", path, status)
	}
	return body, err
}

func (c *HikClient) doGetOnce(path string) ([]byte, int, error) {
	if c.isDigestMode() {
		body, status, err := c.digestRequest(http.MethodGet, path, nil)
		c.logger.Printf("[GET %s] (digest) HTTP %d, %d bytes, err=%v", path, status, len(body), err)
		return body, status, err
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s%s", c.addr, path), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Cookie", "WebSession="+c.session())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	c.logger.Printf("[GET %s] HTTP %d, %d bytes, err=%v", path, resp.StatusCode, len(body), err)
	return body, resp.StatusCode, err
}

// ─── Device identity (ISAPI) ────────────────────────────────────────────────

type hikDeviceInfo struct {
	XMLName         xml.Name `xml:"DeviceInfo"`
	Model           string   `xml:"model"`
	SerialNumber    string   `xml:"serialNumber"`
	MacAddress      string   `xml:"macAddress"`
	FirmwareVersion string   `xml:"firmwareVersion"`
}

// DeviceInfo fetches serial/MAC/model/firmware over the already-open ISAPI
// session — no additional auth beyond what login() already established.
func (c *HikClient) DeviceInfo() (model, serial, mac, firmware string, err error) {
	body, err := c.doGet("/ISAPI/System/deviceInfo")
	if err != nil {
		c.logger.Printf("[identity] deviceInfo request failed: %v", err)
		return "", "", "", "", err
	}
	var info hikDeviceInfo
	if err := xml.Unmarshal(body, &info); err != nil {
		c.logger.Printf("[identity] deviceInfo parse failed: %v, body=%q", err, previewBytes(body, 300))
		return "", "", "", "", err
	}
	c.logger.Printf("[identity] deviceInfo: model=%q serial=%q mac=%q firmware=%q",
		info.Model, info.SerialNumber, info.MacAddress, info.FirmwareVersion)
	return info.Model, info.SerialNumber, info.MacAddress, info.FirmwareVersion, nil
}

// ─── Channel discovery (ISAPI) ─────────────────────────────────────────────────

type hikStreamingChannelList struct {
	XMLName  xml.Name `xml:"StreamingChannelList"`
	Channels []struct {
		ID    int    `xml:"id"`
		Name  string `xml:"channelName"`
		Video struct {
			Enabled   bool   `xml:"enabled"`
			CodecType string `xml:"videoCodecType"`
		} `xml:"Video"`
	} `xml:"StreamingChannel"`
}

// ListChannels mirrors the Dahua Client's method of the same name: it returns
// one ChannelInfo per (physical channel, main/sub) pair, 0-indexed to match
// OpenStream's channel/subType parameters.
func (c *HikClient) ListChannels() []ChannelInfo {
	body, err := c.doGet("/ISAPI/Streaming/channels")
	if err != nil {
		c.logger.Printf("[channels] request failed: %v", err)
		return nil
	}
	var list hikStreamingChannelList
	if err := xml.Unmarshal(body, &list); err != nil {
		c.logger.Printf("[channels] parse failed: %v, body=%q", err, previewBytes(body, 300))
		return nil
	}
	c.logger.Printf("[channels] found %d StreamingChannel entries", len(list.Channels))

	var out []ChannelInfo
	for _, ch := range list.Channels {
		if !ch.Video.Enabled || ch.ID <= 0 {
			continue
		}
		var phys, sub int
		if ch.ID >= 100 {
			// ISAPI streaming channel id convention: 101 = ch1 main, 102 = ch1 sub, etc.
			phys = ch.ID / 100
			sub = ch.ID%100 - 1
		} else {
			// Some devices report bare channel numbers (1, 2, ...) with no
			// main/sub encoding baked into the id; treat each as its own
			// main stream rather than silently dropping it below.
			phys = ch.ID
			sub = 0
		}
		if phys <= 0 || sub < 0 {
			continue
		}
		name := ch.Name
		if name == "" {
			name = fmt.Sprintf("Channel %d", phys)
		}
		label := "Main"
		switch {
		case sub == 1:
			label = "Sub"
		case sub > 1:
			label = fmt.Sprintf("Sub%d", sub)
		}
		out = append(out, ChannelInfo{
			Index:   phys - 1,
			Name:    fmt.Sprintf("%s (%s)", name, label),
			SubType: sub,
		})
	}
	return out
}

// ─── Video stream (/SDK/play) ─────────────────────────────────────────────────

type HikStream struct {
	client           *HikClient
	channel, subType int

	conn   net.Conn
	reader *bufio.Reader
	logger *log.Logger

	buf       []byte // ready-to-emit Annex-B bytes, drained by Read()
	nalBuf    []byte // in-progress FU-A reassembly
	eof       bool
	gotIFrame bool
	Codec     string
}

// OpenStream starts a native /SDK/play video connection. channel is 0-indexed
// (matches Dahua's convention); subType 0 = main stream, 1 = sub stream.
func (c *HikClient) OpenStream(channel, subType int) (*HikStream, error) {
	s := &HikStream{client: c, channel: channel, subType: subType, logger: c.logger}
	conn, br, err := c.dialPlay(channel, subType)
	if err != nil {
		return nil, err
	}
	s.conn, s.reader = conn, br
	if err := s.skipPreamble(); err != nil {
		conn.Close()
		c.logger.Printf("[play] preamble failed: %v", err)
		return nil, fmt.Errorf("preamble: %w", err)
	}
	c.logger.Printf("[play] preamble ok, streaming")
	return s, nil
}

// dialPlay opens one /SDK/play TCP connection and validates the HTTP
// upgrade response. Split out of OpenStream so Read() can transparently
// redial when the device closes the connection out from under an active
// viewer — observed live: this class of device (old NetSDK-over-HTTP path)
// hard-closes /SDK/play connections after roughly 30s regardless of the
// sessionHeartbeat keepalive, and the original captured client reconnected
// the same way rather than treating it as a fatal error.
func (c *HikClient) dialPlay(channel, subType int) (net.Conn, *bufio.Reader, error) {
	return c.dialPlayAttempt(channel, subType, true)
}

// dialPlayAttempt is dialPlay's implementation. allowRelogin gates a single
// relogin-and-retry on a 401 response, so a WebSession/nonce that expired
// between login() and this call (or between two /SDK/play connections) gets
// one automatic recovery attempt instead of failing outright.
func (c *HikClient) dialPlayAttempt(channel, subType int, allowRelogin bool) (net.Conn, *bufio.Reader, error) {
	c.logger.Printf("[play] dialing %s for channel=%d subType=%d", c.addr, channel, subType)
	conn, err := net.DialTimeout("tcp", c.addr, 10*time.Second)
	if err != nil {
		c.logger.Printf("[play] dial failed: %v", err)
		return nil, nil, fmt.Errorf("connect: %w", err)
	}

	body := make([]byte, 44)
	binary.BigEndian.PutUint32(body[0:4], 44)
	binary.BigEndian.PutUint32(body[12:16], 0x00030000) // play command
	binary.BigEndian.PutUint32(body[32:36], uint32(channel+1))
	binary.BigEndian.PutUint32(body[36:40], uint32(subType))
	binary.BigEndian.PutUint32(body[40:44], 0x400)

	host, _, err := net.SplitHostPort(c.addr)
	if err != nil {
		host = c.addr
	}

	// Digest-only firmware has no WebSession at all: /SDK/play needs its own
	// Authorization header, same as every other ISAPI request on those devices.
	var authLine string
	if c.isDigestMode() {
		authLine = "Authorization: " + c.digestAuthHeader(http.MethodGet, "/SDK/play") + "\r\n"
	} else {
		authLine = "Cookie: WebSession=" + c.session() + "\r\n"
	}

	req := fmt.Sprintf(
		"GET /SDK/play HTTP/1.1\r\n"+
			"HOST: %s\r\n"+
			"User-Agent: NS-HTTP/1.0\r\n"+
			"Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*.*;q=0.8\r\n"+
			"%s"+
			"Connection: keep-alive\r\n"+
			"Content-Type: application/octet-stream\r\n"+
			"Content-Length: %d\r\n\r\n",
		host, authLine, len(body))

	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		c.logger.Printf("[play] request write failed: %v", err)
		return nil, nil, fmt.Errorf("play request: %w", err)
	}
	if _, err := conn.Write(body); err != nil {
		conn.Close()
		c.logger.Printf("[play] body write failed: %v", err)
		return nil, nil, fmt.Errorf("play body: %w", err)
	}

	br := bufio.NewReaderSize(conn, 64*1024)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		c.logger.Printf("[play] response read failed: %v", err)
		return nil, nil, fmt.Errorf("play response: %w", err)
	}
	c.logger.Printf("[play] response: HTTP %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		conn.Close()
		c.logger.Printf("[play] non-200 body=%q", previewBytes(errBody, 500))
		if allowRelogin && resp.StatusCode == http.StatusUnauthorized {
			if rerr := c.relogin(); rerr == nil {
				return c.dialPlayAttempt(channel, subType, false)
			} else {
				c.logger.Printf("[play] relogin after 401 failed: %v", rerr)
			}
		}
		return nil, nil, fmt.Errorf("play failed: HTTP %d", resp.StatusCode)
	}
	conn.SetDeadline(time.Time{})
	return conn, br, nil
}

// reconnect redials /SDK/play on the same channel/subType and resets
// reassembly state for a fresh GOP (the new connection starts again at
// SPS/PPS/IDR). Used by Read() to ride out the device's periodic disconnect
// without surfacing an error to the ffmpeg pipe / WS viewer.
func (s *HikStream) reconnect() error {
	if s.conn != nil {
		s.conn.Close()
	}
	conn, br, err := s.client.dialPlay(s.channel, s.subType)
	if err != nil {
		return err
	}
	s.conn, s.reader = conn, br
	s.nalBuf = nil
	s.gotIFrame = false
	if err := s.skipPreamble(); err != nil {
		conn.Close()
		return fmt.Errorf("preamble: %w", err)
	}
	return nil
}

// skipPreamble consumes the header block that precedes the first chunk. Its
// exact leading structure varies a little between sessions (a couple of the
// fixed-looking fields aren't actually constant), so rather than assume fixed
// offsets this locates the "IMKH" magic directly: the 4 bytes immediately
// before it are the tagged block's own length (magic + trailing fields), and
// everything from there to the end of that block is preamble to discard.
func (s *HikStream) skipPreamble() error {
	const window = 256
	peek, err := s.reader.Peek(window)
	if err != nil && len(peek) == 0 {
		return err
	}
	idx := bytesIndex(peek, []byte("IMKH"))
	if idx < 4 {
		s.logger.Printf("[preamble] IMKH not found, first %d bytes=%x", len(peek), peek)
		return fmt.Errorf("IMKH magic not found in first %d bytes of preamble", window)
	}
	blockLen := binary.BigEndian.Uint32(peek[idx-4 : idx])
	if blockLen < 4 || blockLen > 4096 {
		return fmt.Errorf("implausible IMKH block length %d", blockLen)
	}
	total := idx + int(blockLen)
	if _, err := io.CopyN(io.Discard, s.reader, int64(total)); err != nil {
		return err
	}
	return nil
}

func bytesIndex(hay, needle []byte) int {
	return strings.Index(string(hay), string(needle))
}

const (
	hikChunkMinLen  = 16
	hikChunkMaxLen  = 1 << 16
	hikResyncBudget = 512 // max stray bytes tolerated between chunks
)

// readChunk reads one "03 00 <len16le> <seq4> <tag8> <payload>" record and
// returns its RTP-style H.264 payload. See package comment for the resync
// rationale.
func (s *HikStream) readChunk() ([]byte, []byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(s.reader, hdr); err != nil {
		s.logger.Printf("[chunk] header read failed: %v", err)
		return nil, nil, err
	}

	var stray []byte
	for hdr[0] != 0x03 || hdr[1] != 0x00 || !s.chainValid(hdr) {
		b, err := s.reader.ReadByte()
		if err != nil {
			s.logger.Printf("[chunk] resync read failed after %d stray bytes: %v", len(stray), err)
			return nil, nil, err
		}
		stray = append(stray, hdr[0])
		hdr[0], hdr[1], hdr[2], hdr[3] = hdr[1], hdr[2], hdr[3], b
		if len(stray) > hikResyncBudget {
			s.logger.Printf("[chunk] lost sync, budget=%d exhausted, last bytes=%x", hikResyncBudget, stray[max(0, len(stray)-32):])
			return nil, nil, fmt.Errorf("lost sync: no valid chunk marker within %d bytes", hikResyncBudget)
		}
	}

	length := int(hdr[2]) | int(hdr[3])<<8
	rest := make([]byte, length-4)
	if _, err := io.ReadFull(s.reader, rest); err != nil {
		return nil, nil, err
	}
	if len(rest) < 12 {
		return stray, nil, nil
	}
	return stray, rest[12:], nil
}

// chainValid peeks ahead (without consuming) to check that the chunk starting
// at hdr's position, if real, has a declared length that lands on ANOTHER
// plausible "03 00" marker. This two-deep check avoids locking onto an
// incidental 0x03 0x00 byte pair inside compressed video data during resync.
func (s *HikStream) chainValid(hdr []byte) bool {
	if hdr[0] != 0x03 || hdr[1] != 0x00 {
		return false
	}
	length := int(hdr[2]) | int(hdr[3])<<8
	if length < hikChunkMinLen || length > hikChunkMaxLen {
		return false
	}
	peek, err := s.reader.Peek(length - 4 + 2)
	if err != nil {
		// Not enough buffered data to verify — accept optimistically rather
		// than block; readChunk will fail cleanly downstream if this is wrong.
		return true
	}
	next := peek[length-4:]
	return next[0] == 0x03 && next[1] == 0x00
}

// appendRTPPayload feeds one chunk's RTP-style payload into the reassembler,
// flushing s.buf with Annex-B-framed NALs as they complete.
//
// The codec isn't known in advance — the same /SDK/play mechanism carries
// H.264 (RFC 6184 framing, observed in the original capture) on some
// channels/devices and H.265 (RFC 7798 framing) on others (observed live:
// VPS/SPS/PPS/SEI/FU NAL types 32/33/34/39/49 under the 2-byte HEVC NAL
// header). Until the codec is pinned down, every payload is offered to both
// interpretations' "is this an unfragmented parameter set?" check; whichever
// matches first wins for the rest of the stream.
func (s *HikStream) appendRTPPayload(payload []byte) {
	if len(payload) == 0 {
		return
	}
	if s.Codec == "" {
		if len(payload) >= 2 && (payload[0]>>1)&0x3F == 33 { // HEVC SPS
			s.Codec = "hevc"
		} else if payload[0]&0x1F == 7 { // H.264 SPS
			s.Codec = "h264"
		}
	}
	if s.Codec == "hevc" {
		s.appendHEVCPayload(payload)
	} else {
		s.appendH264Payload(payload)
	}
}

// appendH264Payload implements RFC 6184 (Single NAL Unit / FU-A).
func (s *HikStream) appendH264Payload(payload []byte) {
	nalType := payload[0] & 0x1F
	switch {
	case nalType == 28: // FU-A
		if len(payload) < 2 {
			return
		}
		fuIndicator := payload[0]
		fuHeader := payload[1]
		start := fuHeader&0x80 != 0
		end := fuHeader&0x40 != 0
		origType := fuHeader & 0x1F
		frag := payload[2:]

		if start {
			s.flushNAL()
			nalHeader := (fuIndicator & 0xE0) | origType
			s.nalBuf = append([]byte{nalHeader}, frag...)
		} else {
			s.nalBuf = append(s.nalBuf, frag...)
		}
		if end {
			s.flushNAL()
		}

	case nalType >= 1 && nalType <= 23: // single NAL unit packet
		s.flushNAL()
		s.emitNAL(payload)

	default:
		// STAP-A/B, MTAP, or unrecognised aggregation type — not observed in
		// captures; skip rather than risk corrupting the bitstream.
	}
}

// appendHEVCPayload implements RFC 7798 (Single NAL Unit / FU), 2-byte NAL
// header: forbidden(1) type(6) layerIdHigh(1) | layerIdLow(5) temporalId(3).
func (s *HikStream) appendHEVCPayload(payload []byte) {
	if len(payload) < 2 {
		return
	}
	nalType := (payload[0] >> 1) & 0x3F
	switch {
	case nalType == 49: // FU
		if len(payload) < 3 {
			return
		}
		payloadHdr0, payloadHdr1 := payload[0], payload[1]
		fuHeader := payload[2]
		start := fuHeader&0x80 != 0
		end := fuHeader&0x40 != 0
		fuType := fuHeader & 0x3F
		frag := payload[3:]

		if start {
			s.flushNAL()
			nalHdr0 := (payloadHdr0 & 0x81) | (fuType << 1)
			s.nalBuf = append([]byte{nalHdr0, payloadHdr1}, frag...)
		} else {
			s.nalBuf = append(s.nalBuf, frag...)
		}
		if end {
			s.flushNAL()
		}

	case nalType <= 40: // single NAL unit (VCL 0-31, non-VCL 32-40)
		s.flushNAL()
		s.emitNAL(payload)

	default:
		// Aggregation packet (48) or unrecognised — not observed; skip.
	}
}

func (s *HikStream) flushNAL() {
	if len(s.nalBuf) == 0 {
		return
	}
	s.emitNAL(s.nalBuf)
	s.nalBuf = nil
}

// emitNAL appends nal (Annex-B framed) to s.buf. Parameter sets (SPS/PPS, or
// VPS/SPS/PPS for HEVC) always pass through — the raw h264/hevc demuxer needs
// them present in the bitstream to configure the decoder. Ordinary slice NALs
// are dropped until the first IDR, so playback always starts on a keyframe.
func (s *HikStream) emitNAL(nal []byte) {
	var isParamSet, isKeyframe bool
	if s.Codec == "hevc" {
		t := (nal[0] >> 1) & 0x3F
		isParamSet = t == 32 || t == 33 || t == 34 // VPS/SPS/PPS
		// Any IRAP picture (BLA/IDR/CRA, types 16-21) can open decoding, not
		// just IDR — open-GOP encoders start streams on a CRA, and gating on
		// IDR alone would leave s.gotIFrame false forever for those.
		isKeyframe = t >= 16 && t <= 21
	} else {
		t := nal[0] & 0x1F
		isParamSet = t == 7 || t == 8 // SPS/PPS
		isKeyframe = t == 5
	}
	if isKeyframe {
		s.gotIFrame = true
	}
	if !isParamSet && !s.gotIFrame {
		return
	}
	s.buf = append(s.buf, 0x00, 0x00, 0x00, 0x01)
	s.buf = append(s.buf, nal...)
}

// PeekFirstFrame blocks until the first decodable NAL (SPS or IDR slice) has
// been reassembled and returns the sniffed codec, mirroring the Dahua Stream's
// method of the same name.
func (s *HikStream) PeekFirstFrame() (string, error) {
	s.conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer s.conn.SetDeadline(time.Time{})

	for len(s.buf) == 0 {
		stray, payload, err := s.readChunk()
		if err != nil {
			return "", err
		}
		if len(stray) > 0 && len(s.nalBuf) > 0 {
			s.nalBuf = append(s.nalBuf, stray...)
		}
		s.appendRTPPayload(payload)
	}
	return s.Codec, nil
}

const hikMaxReconnectAttempts = 5

func (s *HikStream) Read(p []byte) (int, error) {
	if s.eof {
		return 0, io.EOF
	}
	for len(s.buf) == 0 {
		s.conn.SetDeadline(time.Now().Add(30 * time.Second))
		stray, payload, err := s.readChunk()
		if err != nil {
			if rerr := s.tryReconnect(err); rerr != nil {
				s.eof = true
				return 0, rerr
			}
			continue
		}
		if len(stray) > 0 && len(s.nalBuf) > 0 {
			s.nalBuf = append(s.nalBuf, stray...)
		}
		s.appendRTPPayload(payload)
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

// tryReconnect retries dialPlay a few times with a short backoff after the
// device drops the connection (see reconnect's doc comment). Returns nil once
// reconnected, or the last error once the attempt budget is exhausted.
func (s *HikStream) tryReconnect(cause error) error {
	s.logger.Printf("[play] connection lost (%v), reconnecting", cause)
	var lastErr error
	for attempt := 1; attempt <= hikMaxReconnectAttempts; attempt++ {
		if err := s.reconnect(); err != nil {
			lastErr = err
			s.logger.Printf("[play] reconnect attempt %d/%d failed: %v", attempt, hikMaxReconnectAttempts, err)
			time.Sleep(time.Second)
			continue
		}
		s.logger.Printf("[play] reconnected on attempt %d/%d", attempt, hikMaxReconnectAttempts)
		return nil
	}
	return fmt.Errorf("gave up reconnecting after %d attempts: %w", hikMaxReconnectAttempts, lastErr)
}

func (s *HikStream) Close() error {
	return s.conn.Close()
}
