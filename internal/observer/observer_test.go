package observer

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestJSONSinkUsesAllowlistedFields(t *testing.T) {
	var output bytes.Buffer
	sink := JSONSink(&output)
	sink(Event{Timestamp: "2026-08-31T00:00:00Z", Transport: "tcp", Service: "unknown", Event: "payload_observed", LocalAddr: "127.0.0.1:1", Bytes: 4})

	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"remote_addr", "payload", "token", "client_id"} {
		if _, exists := got[forbidden]; exists {
			t.Fatalf("sensitive field %q was logged", forbidden)
		}
	}
	if got["bytes"] != float64(4) {
		t.Fatalf("bytes = %v, want 4", got["bytes"])
	}
}

func TestTCPObservationIsBoundedAndPayloadFree(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan Event, 1)
	observer := Observer{MaxBytes: 4, ReadTimeout: time.Second, Sink: func(event Event) { events <- event }}
	go func() { _ = observer.ServeTCP(ctx, listener) }()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	secret := "synthetic-secret-value"
	if _, err := conn.Write([]byte(secret)); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	select {
	case event := <-events:
		if event.Transport != "tcp" || event.Bytes != 4 {
			t.Fatalf("unexpected event: %+v", event)
		}
		encoded, _ := json.Marshal(event)
		if strings.Contains(string(encoded), secret) {
			t.Fatal("payload leaked into event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TCP event")
	}
}

func TestUDPObservationIsBounded(t *testing.T) {
	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan Event, 1)
	observer := Observer{MaxBytes: 3, Sink: func(event Event) { events <- event }}
	go func() { _ = observer.ServeUDP(ctx, packetConn) }()

	client, err := net.Dial("udp", packetConn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write([]byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()

	select {
	case event := <-events:
		if event.Transport != "udp" || event.Bytes != 3 {
			t.Fatalf("unexpected event: %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for UDP event")
	}
}
