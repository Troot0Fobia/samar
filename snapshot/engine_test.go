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
