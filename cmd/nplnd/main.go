// Command nplnd runs one NPLN title: TLS on the tenant port, a plain /health, and the
// title's own gRPC services. NPLN_TITLE picks the game (titles.All).
package main

import (
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"

	"google.golang.org/grpc/credentials"

	"github.com/openpak/npln/titles"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	name := os.Getenv("NPLN_TITLE")
	title, ok := titles.All[name]
	if !ok {
		log.Fatalf("NPLN_TITLE=%q is not a title this binary knows; one of: %s", name, strings.Join(names(), ", "))
	}
	addr := env("NPLN_LISTEN", title.Listen)

	cert, err := tls.LoadX509KeyPair(env("CERT_FILE", "npln.pem"), env("KEY_FILE", "npln.key"))
	if err != nil {
		log.Fatalf("load TLS cert: %v", err)
	}
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		// The console lists "grpc-exp" FIRST and "h2" second. Advertising only
		// h2 meant every console connection negotiated plain HTTP/2, created a
		// server transport, and was hung up on immediately without a single
		// RPC [runtime capture, 2026-09-12, after the certificate chain was
		// fixed]. grpc-exp is gRPC's own ALPN identifier and what a Nintendo
		// NPLN server answers with; offering it first is what the client asked
		// for. h2 stays for everything else, including our own probes.
		NextProtos: []string{"h2"},
		MinVersion: tls.VersionTLS12,
		GetConfigForClient: func(hi *tls.ClientHelloInfo) (*tls.Config, error) {
			log.Printf("[TLS] ClientHello sni=%q alpn=%v", hi.ServerName, hi.SupportedProtos)
			return nil, nil
		},
	})
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	if os.Getenv("NPLN_TURN_SECRET") == "" {
		log.Fatal("NPLN_TURN_SECRET is required: it must match coturn's --static-auth-secret")
	}
	serveHealth(env("HEALTH_LISTEN", title.HealthListen), title.Name)
	log.Printf("%s NPLN server listening on %s (gRPC/TLS)", title.Name, addr)
	log.Fatal(title.Run(creds, lis))
}

func serveHealth(addr, service string) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"` + service + `"}`))
	})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("health: cannot listen on %s: %v", addr, err)
		return
	}
	log.Printf("health endpoint on %s (/health)", addr)
	go func() { log.Printf("health: %v", http.Serve(ln, mux)) }()
}

func names() []string {
	out := make([]string, 0, len(titles.All))
	for name := range titles.All {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
