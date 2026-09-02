package probe

import (
	"bytes"
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

// syncBuffer is a concurrency-safe bytes.Buffer: the probe logs from per-
// connection goroutines while the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startTestProbe runs a probe server on a random loopback port, returning the
// address and a buffer capturing the sanitized log.
func startTestProbe(t *testing.T) (addr string, logs *syncBuffer) {
	t.Helper()
	cert, err := GenerateCert()
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	logs = &syncBuffer{}
	srv := &Server{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
			NextProtos:   []string{"h2"},
		},
		Logger: log.New(logs, "", 0),
	}
	go srv.Serve(ln)
	return ln.Addr().String(), logs
}

// TestEndToEndSanitizedLogging drives a real HTTP/2 client (as a stand-in for
// the guest's gRPC stack) against the probe and verifies both observation and
// redaction guarantees.
func TestEndToEndSanitizedLogging(t *testing.T) {
	addr, logs := startTestProbe(t)

	const (
		grpcPath = "/npln.fake.TenantService/FirstMethod"
		secret   = "SUPER_SECRET_TOKEN_VALUE_MUST_NEVER_APPEAR"
		payload  = "REQUEST_BODY_BYTES_MUST_NEVER_APPEAR"
	)

	tr := &http2.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // research client; real guest uses its own trust path
	}
	client := &http.Client{Transport: tr, Timeout: 2 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+addr+grpcPath, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = TenantHostname
	req.Header.Set("content-type", "application/grpc")
	req.Header.Set("te", "trailers")
	req.Header.Set("authorization", "Bearer "+secret)
	// The probe answers with a trailers-only gRPC response: 200 + grpc-status=12
	// (UNIMPLEMENTED). This round-trips our HPACK encoder through the real
	// x/net decoder and proves response writing works end to end.
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request should now succeed against the answering probe: %v\nprobe log:\n%s", err, logs.String())
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	// Trailers-only response: Go's client surfaces grpc-status in resp.Header
	// (with a separate DATA+trailers response it would be in resp.Trailer).
	gs := resp.Header.Get("Grpc-Status")
	if gs == "" {
		gs = resp.Trailer.Get("Grpc-Status")
	}
	if gs != "12" {
		t.Errorf("grpc-status = %q (header or trailer), want \"12\"", gs)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/grpc" {
		t.Errorf("content-type = %q, want application/grpc", ct)
	}
	client.CloseIdleConnections()

	// The probe never answers the request, so the client cancels its stream.
	// Either that RST or a server-side EOF proves the request was fully
	// observed; the client may also keep the connection open with keepalive
	// PINGs, so EOF is possible but not required.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) &&
		!strings.Contains(logs.String(), "rst error=") &&
		!strings.Contains(logs.String(), "client closed") {
		time.Sleep(20 * time.Millisecond)
	}
	got := logs.String()
	t.Logf("probe log:\n%s", got)

	// Observation guarantees: the target metadata is present.
	for _, want := range []string{
		"TLS established",
		"client preface ok",
		"SETTINGS",
		"HEADERS",
		":method=\"POST\"",
		":path=\"/npln.fake.TenantService/FirstMethod\"",
		":scheme=\"https\"",
		"DATA len=",
		"responded stream=1 grpc-status=12",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("log missing expected %q", want)
		}
	}

	// Redaction guarantees: header names are logged, values are not.
	if !strings.Contains(got, "header authorization=<redacted") {
		t.Errorf("log missing redacted authorization header record")
	}
	for _, forbidden := range []string{secret, payload} {
		if strings.Contains(got, forbidden) {
			t.Errorf("log leaked forbidden content %q", forbidden)
		}
	}
}

func TestStripHeadersPadding(t *testing.T) {
	// No flags: payload is the block.
	block := []byte{1, 2, 3}
	got, ok := stripHeadersPadding(block, 0)
	if !ok || !bytes.Equal(got, block) {
		t.Fatalf("plain block: got %v ok=%v", got, ok)
	}
	// PADDED: first byte is pad length.
	padded := append([]byte{2}, append(block, 0, 0)...)
	got, ok = stripHeadersPadding(padded, 0x08)
	if !ok || !bytes.Equal(got, block) {
		t.Fatalf("padded block: got %v ok=%v", got, ok)
	}
	// PADDED with inconsistent pad length must be rejected.
	if _, ok := stripHeadersPadding([]byte{9, 1}, 0x08); ok {
		t.Fatalf("inconsistent padding not rejected")
	}
	// PRIORITY: first 5 bytes stripped.
	pri := append([]byte{0, 0, 0, 0, 0}, block...)
	got, ok = stripHeadersPadding(pri, 0x20)
	if !ok || !bytes.Equal(got, block) {
		t.Fatalf("priority block: got %v ok=%v", got, ok)
	}
}
