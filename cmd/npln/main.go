// npln serves Stardew Valley's NPLN control plane (nn.npln.*) over gRPC/TLS.
//
//	NPLN_LISTEN            listen address (default :18501)
//	CERT_FILE / KEY_FILE   TLS cert covering the tenant host t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net
//	NEXTENDO_ACCOUNT_URL   account server base URL (server-to-server)
//	NEXTENDO_INTERNAL_KEY  the account server's off-network caller key (unset inside its network)
//	NPLN_JWT_KEY           path of the persisted ES256 signing key
//	NPLN_RELAY_HOST/PORT, NPLN_STUN_HOST/PORT, NPLN_TURN_HOST/PORT, NPLN_LATENCY_HOST  session endpoints
package main

import (
	"crypto/tls"
	"log"
	"net"
	"os"

	"google.golang.org/grpc/credentials"

	"github.com/NextendoNetwork/stardew-nextendo/internal/npln"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	addr := env("NPLN_LISTEN", ":18501")
	cert, err := tls.LoadX509KeyPair(env("CERT_FILE", "npln.pem"), env("KEY_FILE", "npln.key"))
	if err != nil {
		log.Fatalf("load TLS cert: %v", err)
	}
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2"},
		MinVersion:   tls.VersionTLS12,
		GetConfigForClient: func(hi *tls.ClientHelloInfo) (*tls.Config, error) {
			log.Printf("[TLS] ClientHello sni=%q alpn=%v", hi.ServerName, hi.SupportedProtos)
			return nil, nil
		},
	})
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	log.Printf("stardew-nextendo NPLN server listening on %s (gRPC/TLS) — tenant %s", addr, npln.Tenant)
	log.Fatal(npln.NewServer(creds).Serve(lis))
}
