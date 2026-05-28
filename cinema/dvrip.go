package cinema

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	magicPoll     = [4]byte{0xa0, 0x05, 0x00, 0x60}
	magicAuthResp = [4]byte{0xb0, 0x01, 0x00, 0x68}
	magicInfoReq  = [4]byte{0xa4, 0x00, 0x00, 0x00}
	magicInfoResp = [4]byte{0xb4, 0x00, 0x00, 0x68}
	magicRPCReq   = [4]byte{0xf6, 0x00, 0x00, 0x00}
	magicRPCResp  = [4]byte{0xf6, 0x00, 0x00, 0x68}
	magicF4Req    = [4]byte{0xf4, 0x00, 0x00, 0x00}
	magicF4Resp   = [4]byte{0xf4, 0x00, 0x00, 0x68}
	magicKeepaliv = [4]byte{0xa1, 0x00, 0x00, 0x00}
	magicBCVideo  = [4]byte{0xbc, 0x00, 0x00, 0x00}
)

type Client struct {
	conn        net.Conn
	addr        string
	user        string
	pass        string
	serverToken uint32
	clientSID   uint32
	callSeq     atomic.Uint32
	mu          sync.Mutex
	openMu      sync.Mutex // serialises concurrent OpenStream calls
	logger      *log.Logger
	closeOnce   sync.Once
	done        chan struct{} // closed by Close(); signals keepaliveLoop to exit

	slotMu      sync.Mutex
	activeSlots [32]bool
}

type ChannelInfo struct {
	Index           int
	Name            string
	SubType         int
	ConnectionState string
}

func NewClient(addr, user, pass, tag string) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}
	c := &Client{
		conn:      conn,
		addr:      addr,
		user:      user,
		pass:      pass,
		clientSID: 155692144,
		logger:    log.New(log.Writer(), "["+tag+"] ", log.LstdFlags),
		done:      make(chan struct{}),
	}
	if err := c.login(); err != nil {
		conn.Close()
		return nil, err
	}
	go c.keepaliveLoop()
	return c, nil
}

func (c *Client) Close() {
	c.closeOnce.Do(func() { close(c.done) })
	c.conn.Close()
}

func md5upper(s string) string {
	h := md5.Sum([]byte(s))
	return strings.ToUpper(fmt.Sprintf("%x", h))
}

func collapseHash(b []byte) string {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	out := make([]byte, 8)
	for i := range 8 {
		out[i] = charset[(int(b[i*2])+int(b[i*2+1]))%62]
	}
	return string(out)
}

func dvripCredentials(user, realm, random, pass string) []byte {
	hashA := md5upper(fmt.Sprintf("%s:%s:%s", user, realm, pass))
	hashA = md5upper(fmt.Sprintf("%s:%s:%s", user, random, hashA))

	rawMD5 := md5.Sum([]byte(pass))
	sofia := collapseHash(rawMD5[:])
	hashB := md5upper(fmt.Sprintf("%s:%s:%s", user, random, sofia))

	return []byte(user + "&&" + hashA + hashB)
}

func (c *Client) login() error {
	c.conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer c.conn.SetDeadline(time.Time{})

	poll := make([]byte, 32)
	copy(poll[0:4], magicPoll[:])
	poll[24], poll[25], poll[26], poll[27] = 0x05, 0x02, 0x00, 0x01
	poll[28], poll[29], poll[30], poll[31] = 0x00, 0x00, 0xa1, 0xaa
	if _, err := c.conn.Write(poll); err != nil {
		return fmt.Errorf("poll: %w", err)
	}

	hdr, payload, err := c.readFrame()
	if err != nil {
		return fmt.Errorf("challenge: %w", err)
	}
	if hdr[0] != 0xb0 {
		return fmt.Errorf("challenge: unexpected magic %x", hdr[0:4])
	}
	realm, random := parseChallenge(string(payload))
	if realm == "" || random == "" {
		return fmt.Errorf("challenge parse failed: %q", payload)
	}

	creds := dvripCredentials(c.user, realm, random, c.pass)
	credsFrame := make([]byte, 32+len(creds))
	copy(credsFrame[0:4], magicPoll[:])
	binary.LittleEndian.PutUint32(credsFrame[4:8], uint32(len(creds)))
	credsFrame[24], credsFrame[25], credsFrame[26], credsFrame[27] = 0x05, 0x02, 0x00, 0x08
	credsFrame[28], credsFrame[29], credsFrame[30], credsFrame[31] = 0x00, 0x00, 0xa1, 0xaa
	copy(credsFrame[32:], creds)
	if _, err := c.conn.Write(credsFrame); err != nil {
		return fmt.Errorf("creds: %w", err)
	}

	var loginPayload []byte
	hdr, loginPayload, err = c.readFrame()
	if err != nil {
		return fmt.Errorf("login result: %w", err)
	}
	if hdr[0] != 0xb0 {
		return fmt.Errorf("login result: unexpected magic %x", hdr[0:4])
	}
	c.logger.Printf("[login] result hdr: %02x", hdr)
	plen := binary.LittleEndian.Uint32(hdr[4:8])
	if plen != 0 && !strings.Contains(string(loginPayload), "Function:") {
		return fmt.Errorf("login failed (plen=%d; likely bad credentials)", plen)
	}
	c.serverToken = binary.LittleEndian.Uint32(hdr[16:20])
	c.logger.Printf("[login] serverToken=%d (0x%08x)", c.serverToken, c.serverToken)
	return nil
}

func parseChallenge(text string) (realm, random string) {
	for line := range strings.SplitSeq(text, "\r\n") {
		switch {
		case strings.HasPrefix(line, "Realm:"):
			realm = strings.TrimPrefix(line, "Realm:")
		case strings.HasPrefix(line, "Random:"):
			random = strings.TrimPrefix(line, "Random:")
		}
	}
	return
}

func (c *Client) keepaliveLoop() {
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-tick.C:
			frame := make([]byte, 32)
			copy(frame[0:4], magicKeepaliv[:])
			c.mu.Lock()
			_, err := c.conn.Write(frame)
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (c *Client) readFrame() ([]byte, []byte, error) {
	hdr := make([]byte, 32)
	if _, err := io.ReadFull(c.conn, hdr); err != nil {
		return nil, nil, err
	}
	plen := binary.LittleEndian.Uint32(hdr[4:8])
	if plen > 4*1024*1024 {
		return nil, nil, fmt.Errorf("payload too large: %d", plen)
	}
	if plen == 0 {
		return hdr, nil, nil
	}
	payload := make([]byte, plen)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return nil, nil, err
	}
	return hdr, payload, nil
}

func readFrameFrom(conn net.Conn) ([]byte, []byte, error) {
	hdr := make([]byte, 32)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return nil, nil, err
	}
	plen := binary.LittleEndian.Uint32(hdr[4:8])
	if plen > 4*1024*1024 {
		return nil, nil, fmt.Errorf("payload too large: %d", plen)
	}
	if plen == 0 {
		return hdr, nil, nil
	}
	payload := make([]byte, plen)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, nil, err
	}
	return hdr, payload, nil
}

func writeF4(conn net.Conn, payload []byte) error {
	hdr := make([]byte, 32)
	copy(hdr[0:4], magicF4Req[:])
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(payload)))
	if _, err := conn.Write(hdr); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

func (c *Client) queryInfo(cmdID uint32) ([]byte, error) {
	c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	defer c.conn.SetDeadline(time.Time{})

	req := make([]byte, 32)
	copy(req[0:4], magicInfoReq[:])
	binary.LittleEndian.PutUint32(req[8:12], cmdID)

	c.mu.Lock()
	_, err := c.conn.Write(req)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}

	for {
		hdr, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		if hdr[0] != 0xb4 {
			continue
		}
		if binary.LittleEndian.Uint32(hdr[8:12]) != cmdID {
			continue
		}
		return payload, nil
	}
}

func (c *Client) queryInfoStr(cmdID uint32) string {
	b, err := c.queryInfo(cmdID)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\x00")
}

func (c *Client) queryInfoU32(cmdID uint32) uint32 {
	b, err := c.queryInfo(cmdID)
	if err != nil || len(b) < 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(b[0:4])
}

func (c *Client) ListChannels() []ChannelInfo {
	result, _, err := c.rpcCall("LogicDeviceManager.factory.instance", nil, nil)
	if err != nil {
		return c.listChannelsFallback()
	}
	var handle int
	if err := json.Unmarshal(result, &handle); err != nil {
		return c.listChannelsFallback()
	}

	_, rpcParams, err := c.rpcCall("LogicDeviceManager.getVideoInputChannels", map[string]any{}, nil)
	if err != nil {
		return c.listChannelsFallback()
	}
	var countResp struct {
		ChannelCountInfo struct {
			MaxLocal  int `json:"MaxLocal"`
			MaxRemote int `json:"MaxRemote"`
		} `json:"channelCountInfo"`
	}
	if err := json.Unmarshal(rpcParams, &countResp); err != nil {
		return c.listChannelsFallback()
	}
	total := countResp.ChannelCountInfo.MaxLocal + countResp.ChannelCountInfo.MaxRemote
	if total == 0 {
		return c.listChannelsFallback()
	}

	allIdxs := make([]int, total)
	for i := range allIdxs {
		allIdxs[i] = i
	}
	_, rpcParams, err = c.rpcCall("LogicDeviceManager.getCameraState",
		map[string]any{"uniqueChannels": allIdxs}, &handle)
	if err != nil {
		return c.listChannelsFallback()
	}
	var statesResp struct {
		States []struct {
			Channel         int    `json:"channel"`
			CameraName      string `json:"cameraName"`
			ConnectionState string `json:"connectionState"`
		} `json:"states"`
	}
	if err := json.Unmarshal(rpcParams, &statesResp); err != nil {
		return c.listChannelsFallback()
	}
	states := statesResp.States

	type meta struct {
		name  string
		state string
	}
	byIdx := map[int]meta{}
	for _, s := range states {
		byIdx[s.Channel] = meta{name: s.CameraName, state: s.ConnectionState}
	}

	var channels []ChannelInfo
	for idx := range total {
		m := byIdx[idx]
		name := m.name
		if name == "" {
			name = fmt.Sprintf("Channel %d", idx)
		}
		for sub := 0; sub <= 1; sub++ {
			label := "Main"
			if sub == 1 {
				label = "Sub"
			}
			channels = append(channels, ChannelInfo{
				Index:           idx,
				Name:            fmt.Sprintf("%s (%s)", name, label),
				SubType:         sub,
				ConnectionState: m.state,
			})
		}
	}
	return channels
}

func (c *Client) listChannelsFallback() []ChannelInfo {
	localCh := uint32(0)
	if b, err := c.queryInfo(0x01); err == nil && len(b) >= 3 {
		localCh = uint32(b[2])
	}
	if localCh == 0 {
		localCh = 1
	}
	var channels []ChannelInfo
	for ch := 0; ch < int(localCh); ch++ {
		for sub := 0; sub <= 1; sub++ {
			label := "Main"
			if sub == 1 {
				label = "Sub"
			}
			channels = append(channels, ChannelInfo{
				Index:   ch,
				Name:    fmt.Sprintf("Channel %d (%s)", ch, label),
				SubType: sub,
			})
		}
	}
	return channels
}

func (c *Client) DeviceInfo() (model, serial, firmware string) {
	model = c.queryInfoStr(0x0B)
	serial = c.queryInfoStr(0x07)
	firmware = c.queryInfoStr(0x08)
	return
}

type rpcReq struct {
	ID      uint32 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
	Object  *int   `json:"object,omitempty"`
	Session uint32 `json:"session"`
}

type rpcResp struct {
	ID     uint32          `json:"id"`
	Result json.RawMessage `json:"result"`
	Params json.RawMessage `json:"params"`
}

func (c *Client) rpcCall(method string, params any, objectHandle *int) (json.RawMessage, json.RawMessage, error) {
	c.conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer c.conn.SetDeadline(time.Time{})

	id := c.callSeq.Add(1)
	body, err := json.Marshal(rpcReq{
		ID:      id,
		Method:  method,
		Params:  params,
		Object:  objectHandle,
		Session: c.serverToken,
	})
	if err != nil {
		return nil, nil, err
	}
	body = append(body, '\n', 0)

	hdr := make([]byte, 32)
	copy(hdr[0:4], magicRPCReq[:])
	plen := uint32(len(body))
	binary.LittleEndian.PutUint32(hdr[4:8], plen)
	binary.LittleEndian.PutUint32(hdr[8:12], id)
	binary.LittleEndian.PutUint32(hdr[16:20], plen)
	binary.LittleEndian.PutUint32(hdr[24:28], c.serverToken)

	c.mu.Lock()
	_, err = c.conn.Write(hdr)
	if err == nil {
		_, err = c.conn.Write(body)
	}
	c.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}

	for {
		fhdr, fpayload, err := c.readFrame()
		if err != nil {
			return nil, nil, err
		}
		c.logger.Printf("[rpc] rx magic=%02x plen=%d", fhdr[0:4], binary.LittleEndian.Uint32(fhdr[4:8]))
		if fhdr[0] != 0xf6 {
			continue
		}
		fpayload = []byte(strings.TrimRight(string(fpayload), "\n\x00"))
		c.logger.Printf("[rpc] payload: %s", fpayload)
		var resp rpcResp
		if err := json.Unmarshal(fpayload, &resp); err != nil {
			continue
		}
		if resp.ID != id {
			continue
		}
		return resp.Result, resp.Params, nil
	}
}

var errNoConnectionID = errors.New("server rejected AddObject (no ConnectionID)")

type Stream struct {
	videoConn net.Conn
	buf       []byte
	eof       bool
	gotIFrame bool
	Codec     string
	pendingBC []byte
}

func (c *Client) OpenStream(channel, subType int) (*Stream, error) {
	// Serialise concurrent opens so readAddObjectResponse / waitF4OKControl
	// don't race each other on c.conn when the client is shared across channels.
	c.openMu.Lock()
	defer c.openMu.Unlock()

	txID := 1940

	videoConn, err := net.DialTimeout("tcp", c.addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("pre-dial video conn: %w", err)
	}

	addObjBody := fmt.Sprintf(
		"TransactionID:%d\r\nMethod:AddObject\r\nParameterName:Dahua.Device.Network.ControlConnection.Passive\r\nConnectProtocol:0\r\n\r\n",
		txID)
	c.mu.Lock()
	err = writeF4(c.conn, []byte(addObjBody))
	c.mu.Unlock()
	if err != nil {
		videoConn.Close()
		return nil, fmt.Errorf("AddObject: %w", err)
	}

	connID, err := c.readAddObjectResponse(txID)
	if err != nil {
		videoConn.Close()
		if errors.Is(err, errNoConnectionID) {
			c.logger.Printf("AddObject rejected by server, falling back to binary f1 stream")
			return c.openStreamBinary(channel, subType)
		}
		c.logger.Printf("AddObject response error (%v), falling back to binary f1 stream", err)
		return c.openStreamBinary(channel, subType)
	}
	txID++
	c.logger.Printf("AddObject → ConnectionID=%d", connID)

	ackBody := fmt.Sprintf(
		"TransactionID:0\r\nMethod:GetParameterNames\r\nParameterName:Dahua.Device.Network.ControlConnection.AckSubChannel\r\nSessionID:%d\r\nConnectionID:%d\r\n\r\n",
		c.serverToken, connID)
	c.logger.Printf("AckSubChannel → %s", strings.ReplaceAll(ackBody, "\r\n", " "))
	if err := writeF4(videoConn, []byte(ackBody)); err != nil {
		videoConn.Close()
		return nil, fmt.Errorf("AckSubChannel: %w", err)
	}

	if err := waitF4OK(videoConn, 5*time.Second, c.logger); err != nil {
		videoConn.Close()
		c.logger.Printf("AckSubChannel failed (%v), falling back to binary f1 stream", err)
		return c.openStreamBinary(channel, subType)
	}

	monBody := fmt.Sprintf(
		"TransactionID:%d\r\nMethod:GetParameterNames\r\nParameterName:Dahua.Device.Network.Monitor.General\r\nchannel:%d\r\nstate:1\r\nConnectionID:%d\r\nstream:%d\r\n\r\n",
		txID, channel, connID, subType)
	c.mu.Lock()
	err = writeF4(c.conn, []byte(monBody))
	c.mu.Unlock()
	if err != nil {
		videoConn.Close()
		return nil, fmt.Errorf("Monitor.General: %w", err)
	}

	if err := c.waitF4OKControl(txID); err != nil {
		c.logger.Printf("Monitor.General response: %v — proceeding anyway", err)
	}

	return &Stream{videoConn: videoConn}, nil
}

func (c *Client) openStreamBinary(channel, subType int) (*Stream, error) {
	videoConn, err := net.DialTimeout("tcp", c.addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("binary: dial video conn: %w", err)
	}

	init := make([]byte, 32)
	init[0] = 0xf1
	binary.LittleEndian.PutUint32(init[8:12], c.serverToken)
	init[12] = byte(channel + 1)
	init[13] = byte(subType + 1)

	videoConn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := videoConn.Write(init); err != nil {
		videoConn.Close()
		return nil, fmt.Errorf("binary: f1 init write: %w", err)
	}

	ack := make([]byte, 32)
	if _, err := io.ReadFull(videoConn, ack); err != nil {
		videoConn.Close()
		return nil, fmt.Errorf("binary: f1 ack: %w", err)
	}
	if ack[0] != 0xf1 {
		videoConn.Close()
		return nil, fmt.Errorf("binary: f1 ack unexpected magic %02x", ack[0])
	}
	videoConn.SetDeadline(time.Time{})
	c.logger.Printf("binary f1 ack ok (ch=%d sub=%d token=0x%08x)", channel, subType, c.serverToken)

	slot := channel*2 + subType
	c.slotMu.Lock()
	if slot >= 0 && slot < len(c.activeSlots) {
		c.activeSlots[slot] = true
	}
	frame := c.buildBinaryMonitor()
	c.slotMu.Unlock()

	c.mu.Lock()
	_, err = c.conn.Write(frame)
	c.mu.Unlock()
	if err != nil {
		videoConn.Close()
		return nil, fmt.Errorf("binary: Monitor.General write: %w", err)
	}

	return &Stream{videoConn: videoConn}, nil
}

func (c *Client) buildBinaryMonitor() []byte {
	frame := make([]byte, 48)
	frame[0] = 0x11
	frame[3] = 0x01
	binary.LittleEndian.PutUint32(frame[4:8], 16)
	for i, active := range c.activeSlots {
		if active {
			frame[8+i] = 0x01
		}
	}
	return frame
}

func (c *Client) readAddObjectResponse(_ int) (uint32, error) {
	c.conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer c.conn.SetDeadline(time.Time{})

	for {
		hdr, payload, err := c.readFrame()
		if err != nil {
			return 0, err
		}
		if hdr[0] != 0xf4 {
			continue
		}
		text := string(payload)
		if !strings.Contains(text, "AddObjectResponse") {
			continue
		}
		for line := range strings.SplitSeq(text, "\r\n") {
			if strings.HasPrefix(line, "ConnectionID:") {
				var id uint32
				fmt.Sscanf(strings.TrimPrefix(line, "ConnectionID:"), "%d", &id)
				return id, nil
			}
		}
		return 0, fmt.Errorf("%w: %s", errNoConnectionID, text)
	}
}

func waitF4OK(conn net.Conn, timeout time.Duration, logger *log.Logger) error {
	conn.SetDeadline(time.Now().Add(timeout))
	defer conn.SetDeadline(time.Time{})

	for {
		hdr := make([]byte, 32)
		n, err := io.ReadFull(conn, hdr)
		if err != nil {
			if n > 0 {
				logger.Printf("[waitF4OK] partial header (%d/32 bytes): %02x", n, hdr[:n])
			} else {
				logger.Printf("[waitF4OK] EOF with 0 bytes — server closed connection immediately")
			}
			return fmt.Errorf("header: %w", err)
		}
		magic := [4]byte(hdr[0:4])
		plen := binary.LittleEndian.Uint32(hdr[4:8])
		logger.Printf("[waitF4OK] rx magic=%02x plen=%d", magic, plen)

		if plen > 4*1024*1024 {
			return fmt.Errorf("payload too large: %d", plen)
		}
		var payload []byte
		if plen > 0 {
			payload = make([]byte, plen)
			if _, err := io.ReadFull(conn, payload); err != nil {
				return fmt.Errorf("payload: %w", err)
			}
			logger.Printf("[waitF4OK] payload: %q", payload)
		}

		if magic[0] == 0xf4 && strings.Contains(string(payload), "FaultCode:OK") {
			return nil
		}
		logger.Printf("[waitF4OK] skipping frame (not f4OK): magic=%02x payload=%q", magic, payload)
	}
}

func (c *Client) waitF4OKControl(_ int) error {
	c.conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer c.conn.SetDeadline(time.Time{})

	for {
		hdr, payload, err := c.readFrame()
		if err != nil {
			return err
		}
		magic := [4]byte(hdr[0:4])
		c.logger.Printf("[waitF4OKCtrl] rx magic=%02x plen=%d", magic, binary.LittleEndian.Uint32(hdr[4:8]))
		if magic[0] != 0xf4 {
			continue
		}
		text := string(payload)
		c.logger.Printf("[waitF4OKCtrl] f4 payload: %q", text)
		if strings.Contains(text, "GetParameterNamesResponse") && strings.Contains(text, "FaultCode:OK") {
			return nil
		}
	}
}

func (s *Stream) readDHAVFrame() (byte, []byte, error) {
	for {
		s.videoConn.SetDeadline(time.Now().Add(30 * time.Second))

		var bchdr, rest []byte

		if len(s.pendingBC) >= 64 {
			bchdr = s.pendingBC[:32]
			rest = s.pendingBC[32:]
			s.pendingBC = nil
		} else {
			bchdr = make([]byte, 32)
			if _, err := io.ReadFull(s.videoConn, bchdr); err != nil {
				return 0, nil, err
			}
			if bchdr[0] != 0xbc || bchdr[1] != 0 || bchdr[2] != 0 {
				continue
			}
			payloadLen := binary.LittleEndian.Uint32(bchdr[4:8])
			if payloadLen < 32 {
				io.CopyN(io.Discard, s.videoConn, int64(payloadLen)) //nolint:errcheck
				continue
			}
			if payloadLen > 4*1024*1024 {
				return 0, nil, fmt.Errorf("DHAV BC payload too large: %d", payloadLen)
			}
			rest = make([]byte, payloadLen)
			if _, err := io.ReadFull(s.videoConn, rest); err != nil {
				return 0, nil, err
			}
		}

		if rest[0] != 0x44 || rest[1] != 0x48 || rest[2] != 0x41 || rest[3] != 0x56 {
			continue
		}

		frameType := rest[4]
		if frameType == 0xfe {
			return 0, nil, io.EOF
		}
		if frameType == 0xfa {
			continue
		}

		frameTotalSize := binary.LittleEndian.Uint32(rest[12:16])
		payloadLen := uint32(len(rest))
		var dhavPayload []byte
		if frameTotalSize <= payloadLen {
			dhavPayload = rest[32:]
		} else {
			dhavPayload = make([]byte, 0, int(frameTotalSize))
			dhavPayload = append(dhavPayload, rest[32:]...)
			for {
				contHdr := make([]byte, 32)
				if _, err := io.ReadFull(s.videoConn, contHdr); err != nil {
					return 0, nil, err
				}
				if contHdr[0] != 0xbc {
					return 0, nil, fmt.Errorf("expected bc continuation, got magic %02x", contHdr[0])
				}
				contLen32 := binary.LittleEndian.Uint32(contHdr[4:8])
				if contLen32 > 4*1024*1024 {
					return 0, nil, fmt.Errorf("DHAV continuation payload too large: %d", contLen32)
				}
				contLen := int(contLen32)
				contData := make([]byte, contLen)
				if _, err := io.ReadFull(s.videoConn, contData); err != nil {
					return 0, nil, err
				}
				if len(contData) >= 4 && contData[0] == 'D' && contData[1] == 'H' &&
					contData[2] == 'A' && contData[3] == 'V' {
					s.pendingBC = append(contHdr, contData...)
					break
				}
				dhavPayload = append(dhavPayload, contData...)
			}
		}
		return frameType, dhavPayload, nil
	}
}

func (s *Stream) PeekFirstFrame() (string, error) {
	s.videoConn.SetDeadline(time.Now().Add(15 * time.Second))
	defer s.videoConn.SetDeadline(time.Time{})

	for {
		frameType, payload, err := s.readDHAVFrame()
		if err != nil {
			return "", err
		}
		switch frameType {
		case 0xfb:
			s.Codec = "mjpeg"
			s.gotIFrame = true
			if off := jpegStart(payload); off >= 0 {
				s.buf = append(s.buf, payload[off:]...)
			}
			return "mjpeg", nil
		case 0xfd:
			nalByte := firstNALByte(payload)
			codec := "h264"
			if nalByte >= 0x40 && nalByte <= 0x4F {
				codec = "hevc"
			}
			s.Codec = codec
			s.gotIFrame = true
			if off := annexBStart(payload); off >= 0 {
				s.buf = append(s.buf, payload[off:]...)
			}
			return codec, nil
		}
	}
}

func (s *Stream) Read(p []byte) (int, error) {
	if s.eof {
		return 0, io.EOF
	}
	if len(s.buf) > 0 {
		n := copy(p, s.buf)
		s.buf = s.buf[n:]
		return n, nil
	}

	for {
		frameType, payload, err := s.readDHAVFrame()
		if err != nil {
			if err == io.EOF {
				s.eof = true
			}
			return 0, err
		}

		switch frameType {
		case 0xfb:
			if s.Codec == "" {
				s.Codec = "mjpeg"
			}
			s.gotIFrame = true
			off := jpegStart(payload)
			if off < 0 {
				continue
			}
			data := payload[off:]
			n := copy(p, data)
			if n < len(data) {
				s.buf = make([]byte, len(data)-n)
				copy(s.buf, data[n:])
			}
			return n, nil

		case 0xfd, 0xfc:
			if s.Codec == "" {
				s.Codec = "h264"
			}
			if !s.gotIFrame {
				if frameType != 0xfd {
					continue
				}
				s.gotIFrame = true
			}
			off := annexBStart(payload)
			if off < 0 {
				continue
			}
			h264 := payload[off:]
			if len(h264) == 0 {
				continue
			}
			n := copy(p, h264)
			if n < len(h264) {
				s.buf = make([]byte, len(h264)-n)
				copy(s.buf, h264[n:])
			}
			return n, nil

		default:
			// Unknown or audio frame type — skip silently to avoid spinning
			// on a stream that never sends video (e.g., audio-only channels).
			// Returning io.EOF lets the caller treat this as a graceful end.
			return 0, io.EOF
		}
	}
}

func (s *Stream) Close() error {
	return s.videoConn.Close()
}

func annexBStart(b []byte) int {
	for i := 0; i+2 < len(b); i++ {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 {
			if i > 0 && b[i-1] == 0 {
				return i - 1
			}
			return i
		}
	}
	return -1
}

func firstNALByte(b []byte) byte {
	for i := 0; i+4 <= len(b); i++ {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 0 && b[i+3] == 1 && i+4 < len(b) {
			return b[i+4]
		}
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 && i+3 < len(b) {
			return b[i+3]
		}
	}
	return 0
}

func jpegStart(b []byte) int {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == 0xFF && b[i+1] == 0xD8 {
			return i
		}
	}
	return -1
}
