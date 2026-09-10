// Package probe holds the sanitized NPLN TLS termination research probe.
package probe

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log"
	"math/big"
	"net"
	"time"
)

// TenantHostname is Stardew's confirmed NPLN tenant endpoint. The locally
// generated certificate covers it so the guest sees a plausible identity;
// certificate-chain validation itself is bypassed by the external,
// build-scoped compatibility patch under research.
const TenantHostname = "t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net"

// TenantWildcard mirrors the identity shape of the production replacement
// certificate (public metadata: a broad wildcard over *.lp1.t.npln.srv.nintendo.net),
// so ANY NPLN tenant — including comparison titles like Splatoon 3 — passes
// post-handshake tenant-identity checks against the probe.
const TenantWildcard = "*.lp1.t.npln.srv.nintendo.net"

// GenerateCert creates a self-signed RSA-2048 certificate covering the tenant
// hostname and loopback. The key and certificate exist only in memory and are
// regenerated on every process start; they are never serialized to disk.
func GenerateCert() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: TenantWildcard},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{TenantWildcard, TenantHostname},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}

// Server wraps the TLS listener and per-connection handling.
type Server struct {
	TLSConfig *tls.Config
	Logger    *log.Logger
}

// BuildTLSConfig returns the probe's TLS configuration: the in-memory
// certificate, h2 ALPN, TLS 1.2+, and ClientHello SNI logging so every
// connection can be attributed to its title/tenant without any emulator-side
// log. Hostnames are public, non-sensitive metadata.
func BuildTLSConfig(cert tls.Certificate, logger *log.Logger) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
		GetConfigForClient: func(hi *tls.ClientHelloInfo) (*tls.Config, error) {
			logger.Printf("clienthello sni=%q alpn_offered=%v", hi.ServerName, hi.SupportedProtos)
			return nil, nil
		},
	}
}

// Serve accepts connections until the listener closes.
func (s *Server) Serve(ln net.Listener) error {
	var nextID int
	for {
		raw, err := ln.Accept()
		if err != nil {
			return err
		}
		nextID++
		id := nextID
		s.Logger.Printf("conn=%d accepted", id)
		go func() {
			defer raw.Close()
			tlsConn := tls.Server(raw, s.TLSConfig)
			HandleConn(id, raw, tlsConn, s.Logger)
		}()
	}
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS1.0"
	case tls.VersionTLS11:
		return "TLS1.1"
	case tls.VersionTLS12:
		return "TLS1.2"
	case tls.VersionTLS13:
		return "TLS1.3"
	default:
		return "unknown"
	}
}

func tlsCipherName(c uint16) string {
	if n := tls.CipherSuiteName(c); n != "" {
		return n
	}
	return "unknown"
}
