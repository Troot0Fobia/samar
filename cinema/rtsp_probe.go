package cinema

import (
	"bufio"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type SDPInfo struct {
	SessionID  string
	Codec      string
	FmtpHash   string
	ControlURL string
	Valid       bool
}

type RTSPChannel struct {
	URL    string `json:"url"`
	Label  string `json:"label"`
	Codec  string `json:"codec"`
	Hash   string `json:"hash"`
	Status string `json:"status,omitempty"`
}

type RTSPMode uint8

const (
	RTSPModeAuto     RTSPMode = iota
	RTSPModeSingle
	RTSPModeTemplate
)

var (
	reTemplate       = regexp.MustCompile(`\{([^}]*)\}`)
	reHikChannels    = regexp.MustCompile(`(?i)/streaming/channels?/\d+`)
	reHikUnicast     = regexp.MustCompile(`(?i)/streaming/unicast/channels?/\d+`)
	reDahuaH264      = regexp.MustCompile(`(?i)/h264/ch\d+/(main|sub)`)
	reChNN           = regexp.MustCompile(`(?i)/ch0*\d+/\d`)
	reChNNHasZero    = regexp.MustCompile(`(?i)/ch0\d`)
	reChannelInPath  = regexp.MustCompile(`(?i)(channel=)\d+`)
	reChannelIDQuery = regexp.MustCompile(`(?i)(channelid=)\d+`)
	reLabelHik       = regexp.MustCompile(`(?i)/streaming/(unicast/)?channels?/(\d+)`)
	reLabelH264      = regexp.MustCompile(`(?i)/h264/ch(\d+)/(main|sub)`)
	reLabelChNN      = regexp.MustCompile(`(?i)/ch0*(\d+)/(\d)`)
	reLabelStdCh     = regexp.MustCompile(`(?i)/stdch(\d+)$`)
	reLabelNumeric   = regexp.MustCompile(`^/(\d+)$`)
	reDigestParam    = regexp.MustCompile(`(\w+)="([^"]*)"`)
)

// RtspDescribe sends RTSP DESCRIBE with Basic auth, retries with Digest on 401.
func RtspDescribe(rawURL string, timeout time.Duration) (SDPInfo, int, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return SDPInfo{}, 0, err
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "554"
	}

	user, pass := "", ""
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}

	cleanURL := &url.URL{Scheme: "rtsp", Host: u.Host, Path: u.Path, RawQuery: u.RawQuery}
	reqURI := cleanURL.String()
	addr := net.JoinHostPort(host, port)

	var authHdr string
	if user != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		authHdr = "Authorization: Basic " + cred + "\r\n"
	}

	status, wwwAuth, body, err := describeOnce(addr, reqURI, authHdr, 1, timeout)
	if err != nil {
		return SDPInfo{}, 0, err
	}

	if status == 401 && wwwAuth != "" && user != "" {
		authHdr = buildDigestAuth(wwwAuth, user, pass, reqURI, "DESCRIBE")
		status, _, body, err = describeOnce(addr, reqURI, authHdr, 2, timeout)
		if err != nil {
			return SDPInfo{}, status, err
		}
	}

	if status != 200 {
		return SDPInfo{Valid: false}, status, nil
	}
	return parseSDP(body), status, nil
}

func describeOnce(addr, reqURI, authHdr string, cseq int, timeout time.Duration) (status int, wwwAuth, body string, err error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return 0, "", "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout)) //nolint: errcheck

	req := fmt.Sprintf(
		"DESCRIBE %s RTSP/1.0\r\nCSeq: %d\r\nAccept: application/sdp\r\nUser-Agent: probe/1.0\r\n%s\r\n",
		reqURI, cseq, authHdr,
	)
	if _, err = fmt.Fprint(conn, req); err != nil {
		return 0, "", "", err
	}

	br := bufio.NewReader(conn)

	line, err := br.ReadString('\n')
	if err != nil {
		return 0, "", "", err
	}
	parts := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(parts) < 2 {
		return 0, "", "", fmt.Errorf("bad status line: %q", line)
	}
	status, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, "", "", fmt.Errorf("parse status %q: %v", parts[1], err)
	}

	contentLen := 0
	for {
		line, err = br.ReadString('\n')
		if err != nil {
			return status, wwwAuth, "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "www-authenticate:"):
			wwwAuth = strings.TrimSpace(line[len("www-authenticate:"):])
		case strings.HasPrefix(lower, "content-length:"):
			contentLen, _ = strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
		}
	}

	if contentLen > 0 {
		buf := make([]byte, contentLen)
		if _, err = io.ReadFull(br, buf); err != nil {
			return status, wwwAuth, "", err
		}
		body = string(buf)
	}
	return status, wwwAuth, body, nil
}

func buildDigestAuth(wwwAuth, user, pass, uri, method string) string {
	if !strings.Contains(strings.ToLower(wwwAuth), "digest") {
		cred := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		return "Authorization: Basic " + cred + "\r\n"
	}

	params := map[string]string{}
	for _, m := range reDigestParam.FindAllStringSubmatch(wwwAuth, -1) {
		params[m[1]] = m[2]
	}

	realm  := params["realm"]
	nonce  := params["nonce"]
	opaque := params["opaque"]
	algo   := params["algorithm"]
	qop    := params["qop"]

	if strings.Contains(qop, "auth") {
		qop = "auth"
	} else {
		qop = ""
	}

	b := make([]byte, 8)
	rand.Read(b) //nolint: errcheck
	cnonce := hex.EncodeToString(b)
	ncStr  := "00000001"

	md5hex := func(s string) string {
		h := md5.Sum([]byte(s))
		return hex.EncodeToString(h[:])
	}

	A1 := md5hex(user + ":" + realm + ":" + pass)
	if strings.EqualFold(algo, "MD5-sess") {
		A1 = md5hex(A1 + ":" + nonce + ":" + cnonce)
	}
	A2 := md5hex(method + ":" + uri)

	var respInput string
	if qop != "" {
		respInput = A1 + ":" + nonce + ":" + ncStr + ":" + cnonce + ":" + qop + ":" + A2
	} else {
		respInput = A1 + ":" + nonce + ":" + A2
	}
	response := md5hex(respInput)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Authorization: Digest username=\"%s\", realm=\"%s\", nonce=\"%s\", uri=\"%s\", response=\"%s\"",
		user, realm, nonce, uri, response)
	if algo != "" {
		fmt.Fprintf(&sb, ", algorithm=\"%s\"", algo)
	}
	if opaque != "" {
		fmt.Fprintf(&sb, ", opaque=\"%s\"", opaque)
	}
	if qop != "" {
		fmt.Fprintf(&sb, ", qop=\"%s\", cnonce=\"%s\", nc=%s", qop, cnonce, ncStr)
	}
	sb.WriteString("\r\n")
	return sb.String()
}

func parseSDP(body string) SDPInfo {
	var info SDPInfo
	var inVideo bool
	var videoAttrs strings.Builder

	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "o="):
			fields := strings.Fields(line[2:])
			if len(fields) >= 2 {
				info.SessionID = fields[1]
			}

		case !inVideo && strings.HasPrefix(line, "a=control:"):
			ctl := strings.TrimPrefix(line, "a=control:")
			if ctl != "*" && strings.HasPrefix(ctl, "rtsp://") {
				info.ControlURL = ctl
			}

		case strings.HasPrefix(line, "m="):
			inVideo = strings.HasPrefix(line, "m=video")
			if inVideo {
				videoAttrs.WriteString(line + "\n")
			}

		case inVideo && strings.HasPrefix(line, "a=rtpmap:") && info.Codec == "":
			rest := strings.TrimPrefix(line, "a=rtpmap:")
			parts := strings.Fields(rest)
			if len(parts) >= 2 {
				codec := strings.ToUpper(strings.SplitN(parts[1], "/", 2)[0])
				if codec == "HEVC" {
					codec = "H265"
				}
				info.Codec = codec
			}
			videoAttrs.WriteString(line + "\n")

		case inVideo && strings.HasPrefix(line, "a=fmtp:") && info.FmtpHash == "":
			videoAttrs.WriteString(line + "\n")
			if idx := strings.Index(line, "sprop-parameter-sets="); idx >= 0 {
				rest := line[idx+len("sprop-parameter-sets="):]
				sps  := strings.SplitN(rest, ",", 2)[0]
				sps   = strings.TrimRight(sps, "; \t\r")
				h    := sha1.Sum([]byte(sps))
				info.FmtpHash = hex.EncodeToString(h[:8])
			}

		case inVideo && strings.HasPrefix(line, "a="):
			videoAttrs.WriteString(line + "\n")
		}
	}

	info.Valid = inVideo || info.Codec != ""
	if info.FmtpHash == "" {
		if va := videoAttrs.String(); va != "" {
			h := sha1.Sum([]byte(va))
			info.FmtpHash = hex.EncodeToString(h[:8])
		} else if info.SessionID != "" {
			h := sha1.Sum([]byte(info.SessionID))
			info.FmtpHash = hex.EncodeToString(h[:8])
		}
	}
	return info
}

func channelLabel(path string) string {
	if m := reLabelHik.FindStringSubmatch(path); m != nil {
		n, _ := strconv.Atoi(m[2])
		if n < 100 {
			return fmt.Sprintf("CH%d Main", n)
		}
		ch, sub := n/100, n%100
		if sub <= 1 {
			return fmt.Sprintf("CH%d Main", ch)
		}
		return fmt.Sprintf("CH%d Sub", ch)
	}
	if m := reLabelH264.FindStringSubmatch(path); m != nil {
		ch, _ := strconv.Atoi(m[1])
		t := strings.ToUpper(m[2][:1]) + strings.ToLower(m[2][1:])
		return fmt.Sprintf("CH%d %s", ch, t)
	}
	if m := reLabelChNN.FindStringSubmatch(path); m != nil {
		ch, _ := strconv.Atoi(m[1])
		sub, _ := strconv.Atoi(m[2])
		if sub == 0 {
			return fmt.Sprintf("CH%d Main", ch)
		}
		return fmt.Sprintf("CH%d Sub", ch)
	}
	if m := reLabelStdCh.FindStringSubmatch(path); m != nil {
		ch, _ := strconv.Atoi(m[1])
		return fmt.Sprintf("CH%d", ch)
	}
	if m := reLabelNumeric.FindStringSubmatch(path); m != nil {
		return "CH" + m[1]
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		return parts[len(parts)-1]
	}
	return "Stream"
}

// ExpandTemplate parses a {…} placeholder in the URL path and returns one RTSPChannel
// per expanded value. Returns nil, false if no placeholder found.
func ExpandTemplate(rawURL string) ([]RTSPChannel, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, false
	}
	m := reTemplate.FindStringSubmatchIndex(u.Path)
	if m == nil {
		return nil, false
	}
	content := u.Path[m[2]:m[3]]

	if content == "" {
		u2 := *u
		u2.Path = u.Path[:m[0]] + u.Path[m[1]:]
		return []RTSPChannel{{URL: u2.String(), Label: "Stream"}}, true
	}

	width := 0
	if idx := strings.IndexAny(content, "0123456789"); idx >= 0 {
		j := idx
		for j < len(content) && content[j] >= '0' && content[j] <= '9' {
			j++
		}
		first := content[idx:j]
		if len(first) > 1 && first[0] == '0' {
			width = len(first)
		}
	}

	format := func(n int) string {
		if width > 0 {
			return fmt.Sprintf("%0*d", width, n)
		}
		return strconv.Itoa(n)
	}

	var numbers []int
	for _, seg := range strings.Split(content, ",") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if dash := strings.Index(seg, "-"); dash >= 0 {
			lo, errL := strconv.Atoi(seg[:dash])
			hi, errH := strconv.Atoi(seg[dash+1:])
			if errL != nil || errH != nil || lo > hi {
				continue
			}
			for i := lo; i <= hi; i++ {
				numbers = append(numbers, i)
			}
		} else {
			n, err := strconv.Atoi(seg)
			if err == nil {
				numbers = append(numbers, n)
			}
		}
	}

	prefix := u.Path[:m[0]]
	suffix := u.Path[m[1]:]
	var result []RTSPChannel
	for _, n := range numbers {
		u2 := *u
		u2.Path = prefix + format(n) + suffix
		result = append(result, RTSPChannel{
			URL:   u2.String(),
			Label: "CH" + strconv.Itoa(n),
		})
	}
	return result, len(result) > 0
}

// DetectRTSPMode returns the mode for a given RTSP URL.
func DetectRTSPMode(rawURL string) RTSPMode {
	u, err := url.Parse(rawURL)
	if err != nil {
		return RTSPModeSingle
	}
	if reTemplate.MatchString(u.Path) {
		return RTSPModeTemplate
	}
	if u.Path == "" || u.Path == "/" {
		return RTSPModeAuto
	}
	return RTSPModeSingle
}

type candidateFamily struct {
	name string
	urls []string
}

const (
	maxChannels   = 32
	maxConsecHits = 16
	maxConsecDups = 3
	maxConsec404  = 6
	globalMaxCh   = 32
)

func channelCandidates(rawURL string) []candidateFamily {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	path := u.Path
	if path == "" {
		path = "/"
	}

	mk := func(p string) string {
		c := *u
		c.Path = p
		c.RawQuery = ""
		c.Fragment = ""
		return c.String()
	}

	hikvisionURLs := func() []string {
		s := make([]string, maxChannels)
		for i := range s {
			s[i] = mk(fmt.Sprintf("/Streaming/Channels/%d01", i+1))
		}
		return s
	}
	hikvisionUnicastURLs := func() []string {
		s := make([]string, maxChannels)
		for i := range s {
			s[i] = mk(fmt.Sprintf("/Streaming/Unicast/channels/%d01", i+1))
		}
		return s
	}
	dahuaH264URLs := func() []string {
		s := make([]string, maxChannels)
		for i := range s {
			s[i] = mk(fmt.Sprintf("/h264/ch%d/main/av_stream", i+1))
		}
		return s
	}
	chNNURLs := func() []string {
		s := make([]string, maxChannels)
		for i := range s {
			s[i] = mk(fmt.Sprintf("/ch%02d/0", i+1))
		}
		return s
	}
	chNNBareURLs := func() []string {
		s := make([]string, maxChannels)
		for i := range s {
			s[i] = mk(fmt.Sprintf("/ch%d/0", i+1))
		}
		return s
	}
	channelParamURLs := func() []string {
		s := make([]string, maxChannels)
		for i := range s {
			c := *u
			c.RawQuery = reChannelInPath.ReplaceAllString(u.RawQuery, fmt.Sprintf("${1}%d", i+1))
			s[i] = c.String()
		}
		return s
	}
	channelInPathURLs := func() []string {
		s := make([]string, maxChannels)
		for i := range s {
			c := *u
			c.Path = reChannelInPath.ReplaceAllString(u.Path, fmt.Sprintf("${1}%d", i+1))
			s[i] = c.String()
		}
		return s
	}
	channelIDParamURLs := func() []string {
		s := make([]string, maxChannels)
		for i := range s {
			c := *u
			c.RawQuery = reChannelIDQuery.ReplaceAllString(u.RawQuery, fmt.Sprintf("${1}%d", i+1))
			s[i] = c.String()
		}
		return s
	}
	stdChURLs := func() []string {
		s := make([]string, maxChannels)
		for i := range s {
			s[i] = mk(fmt.Sprintf("/StdCh%d", i+1))
		}
		return s
	}
	numericURLs := func() []string {
		s := make([]string, maxChannels)
		for i := range s {
			s[i] = mk(fmt.Sprintf("/%d", i+1))
		}
		return s
	}

	switch {
	case reHikUnicast.MatchString(path):
		return []candidateFamily{{name: "Hikvision-Unicast", urls: hikvisionUnicastURLs()}}
	case reHikChannels.MatchString(path):
		return []candidateFamily{{name: "Hikvision", urls: hikvisionURLs()}}
	case reDahuaH264.MatchString(path):
		return []candidateFamily{{name: "Dahua-h264", urls: dahuaH264URLs()}}
	case reChNN.MatchString(path):
		if reChNNHasZero.MatchString(path) {
			return []candidateFamily{{name: "chNN-padded", urls: chNNURLs()}}
		}
		return []candidateFamily{{name: "chNN-bare", urls: chNNBareURLs()}}
	}

	lq := strings.ToLower(u.RawQuery)
	if strings.Contains(lq, "channel=") {
		return []candidateFamily{{name: "channelParam", urls: channelParamURLs()}}
	}
	if strings.Contains(lq, "channelid=") {
		return []candidateFamily{{name: "channelIDParam", urls: channelIDParamURLs()}}
	}

	if reChannelInPath.MatchString(path) {
		return []candidateFamily{{name: "channelInPath", urls: channelInPathURLs()}}
	}

	if path != "/" && path != "" {
		return []candidateFamily{{name: "original", urls: []string{rawURL}}}
	}

	return []candidateFamily{
		{name: "Hikvision",         urls: hikvisionURLs()},
		{name: "Hikvision-Unicast", urls: hikvisionUnicastURLs()},
		{name: "Dahua-h264",        urls: dahuaH264URLs()},
		{name: "chNN-padded",       urls: chNNURLs()},
		{name: "chNN-bare",         urls: chNNBareURLs()},
		{name: "StdCh",             urls: stdChURLs()},
		{name: "numeric",           urls: numericURLs()},
	}
}

const scanCandidateTimeout = 5 * time.Second

// EnumerateRTSPChannels probes candidate families for unique streams.
func EnumerateRTSPChannels(rawURL string) []RTSPChannel {
	families := channelCandidates(rawURL)
	seen     := map[string]bool{}
	var result []RTSPChannel

	addIfUnique := func(candidateURL, path string, sdp SDPInfo) {
		if sdp.FmtpHash != "" {
			key := sdp.FmtpHash
			if sdp.ControlURL != "" {
				key = sdp.ControlURL + "\x00" + sdp.FmtpHash
			}
			if seen[key] {
				return
			}
			seen[key] = true
		}
		result = append(result, RTSPChannel{
			URL:   candidateURL,
			Label: channelLabel(path),
			Codec: sdp.Codec,
			Hash:  sdp.FmtpHash,
		})
	}

	for _, fam := range families {
		if len(result) >= globalMaxCh {
			break
		}
		consecHits := 0
		consecDups := 0
		consec404  := 0
		for _, u := range fam.urls {
			if len(result) >= globalMaxCh {
				break
			}
			sdp, status, err := RtspDescribe(u, scanCandidateTimeout)
			if err != nil {
				consecHits = 0
				consecDups = 0
				consec404  = 0
				continue
			}
			if status == 404 {
				consec404++
				if consec404 >= maxConsec404 {
					break
				}
				continue
			}
			consec404 = 0
			if status == 200 && sdp.Valid {
				prevLen := len(result)
				pu, _ := url.Parse(u)
				addIfUnique(u, pu.Path, sdp)
				if len(result) == prevLen {
					consecDups++
					consecHits = 0
					if consecDups >= maxConsecDups {
						break
					}
				} else {
					consecDups = 0
					consecHits++
					if consecHits >= maxConsecHits {
						break
					}
				}
			} else {
				consecHits = 0
				consecDups = 0
			}
		}
	}

	return result
}

// ProbeRTSP dials host:port and checks for a valid RTSP server response.
func ProbeRTSP(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "554"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 30*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second)) //nolint: errcheck

	clean := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawQuery: u.RawQuery}
	req := "OPTIONS " + clean.String() + " RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: probe/1.0\r\n\r\n"
	if _, err := fmt.Fprint(conn, req); err != nil {
		return false
	}

	buf := make([]byte, 32)
	n, err := conn.Read(buf)
	return err == nil && n >= 5 && string(buf[:5]) == "RTSP/"
}

// StripRTSPCreds removes credentials from an RTSP URL.
func StripRTSPCreds(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	return u.String()
}
