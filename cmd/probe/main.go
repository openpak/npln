// cmd/probe: a controlled, sanitized TLS termination point for Stardew's NPLN
// tenant traffic (see handoff.md, Experiment 2026-08-31-7).
//
// The probe terminates TLS locally with an in-memory self-signed certificate,
// answers only the minimum HTTP/2 framing required to keep the peer talking,
// and logs sanitized metadata: TLS version/cipher/ALPN, HTTP/2 frame types and
// flags, the gRPC pseudo-headers, other header NAMES, and byte counts. It
// never logs payload bodies, non-pseudo header values, keys, addresses, or
// certificates, and it never answers an application request.
//
// Configuration (environment):
//
//	STARDEW_PROBE_ADDR   listen address, default 127.0.0.1:18500 (loopback only)
package main

import (
	"log"
	"net"
	"os"

	"github.com/NextendoNetwork/stardew-nextendo/internal/probe"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	addr := os.Getenv("STARDEW_PROBE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:18500"
	}
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "127.0.0.1" && host != "localhost" {
		log.Fatalf("refusing to listen on non-loopback address %q", addr)
	}

	cert, err := probe.GenerateCert()
	if err != nil {
		log.Fatalf("generate in-memory certificate: %v", err)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	log.Printf("NPLN probe listening on %s (SAN: %s + %s); metadata-only logging, no application responses", addr, probe.TenantWildcard, probe.TenantHostname)

	srv := &probe.Server{TLSConfig: probe.BuildTLSConfig(cert, log.Default()), Logger: log.Default()}
	if err := srv.Serve(ln); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
