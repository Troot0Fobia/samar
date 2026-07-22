package cinema

import (
	"encoding/hex"
	"errors"
	"testing"
)

// Real Dahua login-result frame headers captured from problem cameras
// (invalid_cams_part_{1,2}.pcapng). byte[8] is the auth verdict, byte[9] the
// sub-reason. These lock in the decode so the old plen heuristic can't creep
// back.
func TestDecodeLoginVerdict(t *testing.T) {
	cases := []struct {
		name    string
		hdr     string // hex of the 32-byte login-result header
		wantErr error  // nil, ErrBadCredentials, or ErrAccountLocked
	}{
		{"91.213.33.46 valid (session continues)", "b000005800000000000804084100000054020000010000000600f90000006402", nil},
		{"217.77.219.121 wrong password", "b001006800000000010010083d00000054ae4f37000000000600f90000066402", ErrBadCredentials},
		{"185.4.42.90 wrong password", "b001007800000000010018000000000000000000010000000600f90000040002", ErrBadCredentials},
		{"91.201.235.95 unknown user (uzer)", "b00000580000000001010109330000005ff6dd68018813000600f90000016402", ErrBadCredentials},
		{"194.44.89.124 wrong password at capture", "b001005800000000010008084100000086000000000000000600f90000036402", ErrBadCredentials},
		{"195.214.222.82 user locked", "b001005800000000010408084100000010010000000000000600f90000006402", ErrAccountLocked},
		{"194.28.144.22 blocklisted", "b001006800000000010508000000000000000000010000000600f90000050002", ErrAccountLocked},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hdr, err := hex.DecodeString(tc.hdr)
			if err != nil {
				t.Fatalf("bad hex: %v", err)
			}
			got := decodeLoginVerdict(hdr)
			switch {
			case tc.wantErr == nil && got != nil:
				t.Fatalf("expected success, got error: %v", got)
			case tc.wantErr != nil && !errors.Is(got, tc.wantErr):
				t.Fatalf("expected %v, got %v", tc.wantErr, got)
			}
		})
	}
}

// TestParseDeviceClass covers the login-result payloads captured from a VTO, an
// access controller (BSC — no video), and an IPC (no DeviceClass line).
func TestParseDeviceClass(t *testing.T) {
	cases := []struct {
		payload  string
		want     string
		hasVideo bool
	}{
		{"DeviceClass:VTO\r\nDeviceType:VTO2000A\r\n\r\n\x00", "VTO", true},
		{"DeviceClass:BSC\r\nDeviceType:ASI1212D\r\n\r\n\x00", "BSC", false},
		{"MediaEncrypt:2\r\n\x00UTCCaps:0x00000000\r\n", "", true}, // IPC — no class, has video
		{"", "", true}, // plain camera
	}
	for _, tc := range cases {
		got := parseDeviceClass([]byte(tc.payload))
		if got != tc.want {
			t.Fatalf("parseDeviceClass(%q) = %q, want %q", tc.payload, got, tc.want)
		}
		c := &Client{deviceClass: got}
		if c.HasVideo() != tc.hasVideo {
			t.Fatalf("HasVideo for class %q = %v, want %v", got, c.HasVideo(), tc.hasVideo)
		}
	}
}
