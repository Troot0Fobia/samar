package snapshot

import "testing"

func TestPrefixWithMethod(t *testing.T) {
	cases := []struct {
		name, method, msg, want string
	}{
		{"tags a bare message", "dahua", "dial tcp 1.2.3.4:37777: i/o timeout", "dahua: dial tcp 1.2.3.4:37777: i/o timeout"},
		{"empty message stays empty", "dahua", "", ""},
		{"empty method leaves message untouched", "", "EOF", "EOF"},
		{"already tagged is idempotent", "hikvision", "hikvision: sessionLogin: account locked", "hikvision: sessionLogin: account locked"},
		{"a different method's prefix is not mistaken for a tag", "rtsp", "dahua: something", "rtsp: dahua: something"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := prefixWithMethod(tc.method, tc.msg); got != tc.want {
				t.Fatalf("prefixWithMethod(%q, %q) = %q, want %q", tc.method, tc.msg, got, tc.want)
			}
		})
	}
}

// TestInjectCredsDashSentinel covers "-" as an explicitly-absent login or
// password (matches BuildGeneratedRTSP's convention) — neither should ever
// end up literally in the RTSP URL's userinfo.
func TestInjectCredsDashSentinel(t *testing.T) {
	cases := []struct {
		name, url, login, pass, want string
	}{
		{"both dash", "rtsp://1.2.3.4:554/x", "-", "-", "rtsp://1.2.3.4:554/x"},
		{"dash login only", "rtsp://1.2.3.4:554/x", "-", "somepass", "rtsp://1.2.3.4:554/x"},
		{"dash password only", "rtsp://1.2.3.4:554/x", "admin", "-", "rtsp://1.2.3.4:554/x"},
		{"empty login", "rtsp://1.2.3.4:554/x", "", "irrelevant", "rtsp://1.2.3.4:554/x"},
		{"real creds get injected", "rtsp://1.2.3.4:554/x", "admin", "admin123", "rtsp://admin:admin123@1.2.3.4:554/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := injectCreds(tc.url, tc.login, tc.pass); got != tc.want {
				t.Fatalf("injectCreds(%q, %q, %q) = %q, want %q", tc.url, tc.login, tc.pass, got, tc.want)
			}
		})
	}
}
