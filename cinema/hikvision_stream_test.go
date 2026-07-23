package cinema

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"log"
	"strings"
	"testing"
)

// hikPlayBodyLeadingIDR is the real /SDK/play response body (IMKH preamble +
// RTP-chunk stream) captured from 37.57.77.80 channel 0
// (hikvision_particaly.pcapng). Its very first RTP payload is an H.264 IDR
// slice (NAL type 5, byte 0x65) with no preceding SPS in this session — the
// device apparently doesn't repeat SPS/PPS before every keyframe. This used to
// leave HikStream.Codec "" (codec sniffing only recognised SPS/VPS-shaped
// NALs), so PeekFirstFrame handed ffmpeg an empty -f value and it failed with
// "Unknown input format: ''" — exactly the failure seen in run #17 for this
// camera's channel 0.
const hikPlayBodyLeadingIDR = "" +
	"0000004000000348000000010000000000000028494d4b48010100000400000110710110401f" +
	"000000fa000000000000000000000000000000000000040000000300500180803cf9ca3e4ac0" +
	"5566778865656565666465656564656565656564656465656565656565656564656565656564" +
	"6565656565666565656564656565656564646565656564656565656465656564656465656565" +
	"6565656564656464646465656565656465646565656564656565656465656464656564656465" +
	"6464656465646465646464646564656565656565646464646564646465656464646564646464" +
	"6464646465656565656464656566656565656565646565656564656465656465656464646565" +
	"6564656565656565656565656565656465646565656565646565656565646565656565646565" +
	"6565656464646464646464646464656465656464656465656565656565656565656565666565" +
	"6565656465656564656565656665656565646465656565656565656565666565656566656565" +
	"65656565656565656565666564656565656666650300440090f0e48ae33dce8c556677880001" +
	"000c400e484b0100e212d70a600000ffffff450a1b910040b2a1ffffffff4112484b00010203" +
	"0405060708090a0b0c0d0e0f0300300090f0e48be33dce8c5566778800020007420e00006000" +
	"078004380200ff004651430a00a0ff007d0303e803ff03003800a060c604e33dce8c55667788" +
	"6764002aad84010c20086100430802184010c200842b703c0113f2c2000023280002bf210800" +
	"0003030014008060c605e33dce8c5566778868ee3cb0"

// TestPeekFirstFrameLeadingIDR replays the real capture through skipPreamble +
// PeekFirstFrame and requires a non-empty codec, guarding against the empty-
// codec regression described above.
func TestPeekFirstFrameLeadingIDR(t *testing.T) {
	raw, err := hex.DecodeString(hikPlayBodyLeadingIDR)
	if err != nil {
		t.Fatalf("bad hex fixture: %v", err)
	}
	conn := &mockConn{r: bytes.NewReader(raw)}
	s := &HikStream{
		conn:   conn,
		reader: bufio.NewReaderSize(conn, 64*1024),
		logger: log.New(bytes.NewBuffer(nil), "", 0),
	}
	if err := s.skipPreamble(); err != nil {
		t.Fatalf("skipPreamble: %v", err)
	}
	codec, err := s.PeekFirstFrame()
	if err != nil {
		t.Fatalf("PeekFirstFrame failed on a leading-IDR stream: %v", err)
	}
	if codec == "" {
		t.Fatal("PeekFirstFrame returned an empty codec — ffmpeg would fail with -f ''")
	}
	if codec != "h264" {
		t.Fatalf("expected h264, got %q", codec)
	}
}

// hikNoSignalReply is the real /SDK/play response body captured from
// 178.159.212.213 channels 4 and 5 (hikvision_particaly.pcapng, run #17
// "partial" category) — an NVR channel with no camera attached. Byte-identical
// across both channels and every retry: a 0x40 leading marker (also present in
// normal IMKH replies), a 4-byte code repeated twice, then all zeros. No IMKH
// magic anywhere.
const hikNoSignalReply = "00000040000000070000000700000000000000000000000000000000000000" +
	"0000000000000000000000000000000000000000000000000000000000000000"

// TestSkipPreambleNoSignal requires skipPreamble to name this condition
// specifically ("no video source") rather than reporting the generic
// "IMKH magic not found" parse-failure message, which reads like a bug in our
// code when the device is actually just reporting an empty channel.
func TestSkipPreambleNoSignal(t *testing.T) {
	raw, err := hex.DecodeString(hikNoSignalReply)
	if err != nil {
		t.Fatalf("bad hex fixture: %v", err)
	}
	conn := &mockConn{r: bytes.NewReader(raw)}
	s := &HikStream{
		conn:   conn,
		reader: bufio.NewReaderSize(conn, 64*1024),
		logger: log.New(bytes.NewBuffer(nil), "", 0),
	}
	err = s.skipPreamble()
	if err == nil {
		t.Fatal("expected an error for a no-signal reply, got nil")
	}
	if strings.Contains(err.Error(), "IMKH magic not found") {
		t.Fatalf("error still reads as a parse failure, not a device-side no-signal condition: %v", err)
	}
	if !strings.Contains(err.Error(), "no video source") {
		t.Fatalf("expected a clear no-video-source message, got: %v", err)
	}
}
