package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/NextendoNetwork/stardew-nextendo/internal/observer"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	readTimeout, err := time.ParseDuration(env("STARDEW_OBSERVER_READ_TIMEOUT", "2s"))
	if err != nil {
		log.Fatalf("invalid STARDEW_OBSERVER_READ_TIMEOUT: %v", err)
	}
	maxBytes, err := strconv.Atoi(env("STARDEW_OBSERVER_MAX_BYTES", "4096"))
	if err != nil || maxBytes < 1 || maxBytes > 65535 {
		log.Fatal("STARDEW_OBSERVER_MAX_BYTES must be between 1 and 65535")
	}

	o := observer.Observer{MaxBytes: maxBytes, ReadTimeout: readTimeout, Sink: observer.JSONSink(os.Stdout)}
	errCh := make(chan error, 2)
	listeners := 0

	if addr := env("STARDEW_OBSERVER_TCP_ADDR", "127.0.0.1:18080"); addr != "" {
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatal(err)
		}
		listeners++
		log.Printf("TCP observer listening on %s", listener.Addr())
		go func() { errCh <- o.ServeTCP(ctx, listener) }()
	}
	if addr := env("STARDEW_OBSERVER_UDP_ADDR", "127.0.0.1:18080"); addr != "" {
		conn, err := net.ListenPacket("udp", addr)
		if err != nil {
			log.Fatal(err)
		}
		listeners++
		log.Printf("UDP observer listening on %s", conn.LocalAddr())
		go func() { errCh <- o.ServeUDP(ctx, conn) }()
	}
	if listeners == 0 {
		log.Fatal("at least one observer address must be enabled")
	}

	for completed := 0; completed < listeners; completed++ {
		if err := <-errCh; err != nil {
			fmt.Fprintln(os.Stderr, err)
			stop()
		}
	}
}

func env(name, fallback string) string {
	if value, exists := os.LookupEnv(name); exists {
		return value
	}
	return fallback
}
