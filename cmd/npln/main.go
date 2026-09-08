// npln serves Stardew Valley's NPLN control plane (nn.npln.*) over gRPC/TLS.
//
//	NPLN_LISTEN            listen address (default :21010, the Stardew tenant port in Openpak/ports.md)
//	CERT_FILE / KEY_FILE   TLS cert covering the tenant host t-9f607adf-lp1.lp1.t.npln.srv.nintendo.net
//	NX_INTERNAL_URL        nx-baas internal API (default http://127.0.0.1:20070): proves nnex, lists friends
//	NX_INTERNAL_KEY        nx-baas's NX_INTERNAL_KEY, sent as X-Internal-Key
//	NPLN_JWT_KEY           path of the persisted ES256 signing key
//	NPLN_RELAY_HOST/PORT, NPLN_STUN_HOST/PORT, NPLN_TURN_HOST/PORT, NPLN_LATENCY_HOST  session endpoints
package main

import (
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"os"

	"google.golang.org/grpc/credentials"

	"openpak/stardew-valley/internal/npln"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	addr := env("NPLN_LISTEN", ":21010")
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
	// A plain HTTP health port beside the gRPC one. The service port answers
	// only TLS+h2, so anything checking on it either has to speak gRPC or
	// learns nothing from a bare connection; this gives a status page a real
	// answer without a client certificate or a protocol implementation.
	serveHealth(env("HEALTH_LISTEN", ":21011"))
	log.Printf("stardew-valley NPLN server listening on %s (gRPC/TLS) — tenant %s", addr, npln.Tenant)
	log.Fatal(npln.NewServer(creds).Serve(lis))
}

// serveHealth runs the health endpoint in the background. An empty address
// disables it, and a port that will not open is logged rather than fatal: the
// game must not fail to start because its diagnostics could not.
func serveHealth(addr string) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"stardew-valley"}`))
	})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("health: cannot listen on %s: %v", addr, err)
		return
	}
	log.Printf("health endpoint on %s (/health)", addr)
	go func() { log.Printf("health: %v", http.Serve(ln, mux)) }()
}
