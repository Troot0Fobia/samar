package initializers

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnsureDevTLSCert returns paths to a self-signed TLS cert/key for local
// development, generating and caching them under dir on first use.
//
// Browsers only ever negotiate HTTP/2 via TLS (ALPN) — there is no way to
// get it over plain HTTP/1.1 for ordinary browser navigation. Plain
// HTTP/1.1, which is what the dev server ran before this, caps every
// browser at 6 concurrent connections per origin; Cinema holds one
// persistent WebSocket per open camera for the life of the view, so that
// cap was reachable with a perfectly ordinary number of cameras open —
// confirmed live (browser DevTools showed new requests sitting "stalled",
// never even sent, past 6 simultaneous cameras, immediately resolved by
// closing one). Production already serves over HTTPS (see main.go's
// autocert branch) and gets HTTP/2 for free from Go's net/http; this gives
// local http://localhost dev testing the same HTTP/2 behaviour without
// needing a real domain or CA-issued cert — the browser will flag the
// self-signed cert once per machine, and remember the exception after that.
func EnsureDevTLSCert(dir string) (certFile, keyFile string, err error) {
	certFile = filepath.Join(dir, "dev-cert.pem")
	keyFile = filepath.Join(dir, "dev-key.pem")

	if _, statErr := os.Stat(certFile); statErr == nil {
		if _, statErr := os.Stat(keyFile); statErr == nil {
			return certFile, keyFile, nil
		}
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create %s: %w", dir, err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("generate serial: %w", err)
	}

	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "samar dev (localhost)"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", "", fmt.Errorf("create certificate: %w", err)
	}

	certOut, err := os.OpenFile(certFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return "", "", fmt.Errorf("open %s: %w", certFile, err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return "", "", fmt.Errorf("write cert: %w", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", fmt.Errorf("marshal key: %w", err)
	}
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("open %s: %w", keyFile, err)
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return "", "", fmt.Errorf("write key: %w", err)
	}

	return certFile, keyFile, nil
}
