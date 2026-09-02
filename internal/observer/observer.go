package observer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type Event struct {
	Timestamp string `json:"timestamp"`
	Transport string `json:"transport"`
	Service   string `json:"service"`
	Event     string `json:"event"`
	LocalAddr string `json:"local_addr"`
	Bytes     int    `json:"bytes"`
}

type Sink func(Event)

type Observer struct {
	MaxBytes    int
	ReadTimeout time.Duration
	Sink        Sink
}

func JSONSink(w io.Writer) Sink {
	var mu sync.Mutex
	return func(event Event) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(event)
	}
}

func (o Observer) ServeTCP(ctx context.Context, listener net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go o.observeTCP(conn)
	}
}

func (o Observer) observeTCP(conn net.Conn) {
	defer conn.Close()
	limit := o.limit()
	_ = conn.SetReadDeadline(time.Now().Add(o.timeout()))
	buffer := make([]byte, limit)
	n, _ := conn.Read(buffer)
	o.emit("tcp", conn.LocalAddr().String(), n)
}

func (o Observer) ServeUDP(ctx context.Context, conn net.PacketConn) error {
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	buffer := make([]byte, o.limit())
	for {
		n, _, err := conn.ReadFrom(buffer)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		o.emit("udp", conn.LocalAddr().String(), n)
	}
}

func (o Observer) emit(transport, localAddr string, bytes int) {
	if o.Sink == nil {
		return
	}
	o.Sink(Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Transport: transport,
		Service:   "unknown",
		Event:     "payload_observed",
		LocalAddr: localAddr,
		Bytes:     bytes,
	})
}

func (o Observer) limit() int {
	if o.MaxBytes <= 0 {
		return 4096
	}
	return o.MaxBytes
}

func (o Observer) timeout() time.Duration {
	if o.ReadTimeout <= 0 {
		return 2 * time.Second
	}
	return o.ReadTimeout
}
