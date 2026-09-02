package snapshot

import (
	"errors"
	"fmt"
	"testing"

	"Troot0Fobia/samar/cinema"
)

// TestClassifyConnError pins the connect/login-phase error taxonomy: which
// wire failures land in which bucket, and specifically that a mid-exchange
// drop (EOF / reset) is connection_error (not counted as "silent"), a bare
// HTTP auth rejection is authorization_error (not wrong_creds), and only
// true unreachability is network_error.
func TestClassifyConnError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"sentinel bad credentials", fmt.Errorf("login: %w", cinema.ErrBadCredentials), "wrong_creds"},
		{"sentinel account locked", fmt.Errorf("login: %w", cinema.ErrAccountLocked), "account_locked"},
		{"bare 401", errors.New("sessionLogin failed: status 401"), "authorization_error"},
		{"unauthorized text", errors.New("GET /ISAPI: Unauthorized"), "authorization_error"},
		{"403 forbidden", errors.New("play: 403 Forbidden"), "authorization_error"},
		{"connection refused", errors.New("dial tcp 1.2.3.4:80: connect: connection refused"), "network_error"},
		{"no route to host", errors.New("dial tcp: no route to host"), "network_error"},
		{"network unreachable", errors.New("dial tcp: network is unreachable"), "network_error"},
		{"reset by peer", errors.New("read tcp: connection reset by peer"), "connection_error"},
		{"broken pipe", errors.New("write tcp: broken pipe"), "connection_error"},
		{"bare EOF", errors.New("EOF"), "connection_error"},
		{"i/o timeout", errors.New("dial tcp 1.2.3.4:80: i/o timeout"), "timeout"},
		{"unknown", errors.New("something weird happened"), "camera_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyConnError(tc.err).ErrorType; got != tc.want {
				t.Fatalf("classifyConnError(%q).ErrorType = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

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
