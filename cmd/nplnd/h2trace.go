package main

// h2trace — per-connection HTTP/2 frame accounting under TLS, for consoles that hang up without a
// visible RPC error. Logs frame types per direction, every GOAWAY and RST_STREAM with its error
// code, and who closed the connection first. Frame types and codes only: no headers, no payloads.

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/credentials"
)

var h2FrameNames = map[byte]string{0: "DATA", 1: "HEADERS", 2: "PRIORITY", 3: "RST", 4: "SETTINGS", 5: "PUSH",
	6: "PING", 7: "GOAWAY", 8: "WINDOW", 9: "CONT"}

var h2ConnSeq atomic.Int64

type traceCreds struct {
	credentials.TransportCredentials
}

func (c traceCreds) ServerHandshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, info, err := c.TransportCredentials.ServerHandshake(raw)
	if err != nil {
		log.Printf("[H2] TLS handshake failed: %s", strings.SplitN(err.Error(), ":", 2)[0])
		return conn, info, err
	}
	t := &traceConn{Conn: conn, id: h2ConnSeq.Add(1), start: time.Now()}
	t.in.skip = 24 // the client connection preface
	return t, info, nil
}

func (c traceCreds) Clone() credentials.TransportCredentials {
	return traceCreds{c.TransportCredentials.Clone()}
}

// h2Parser follows frame boundaries across Read/Write calls.
type h2Parser struct {
	skip   int    // bytes still to pass (preface or the rest of a payload)
	hdr    []byte // a partial 9-byte frame header
	want   int    // payload bytes still to capture for GOAWAY/RST
	cap    []byte
	ctype  byte
	counts map[string]int
}

func (p *h2Parser) feed(b []byte, dir string, id int64) {
	if p.counts == nil {
		p.counts = map[string]int{}
	}
	for len(b) > 0 {
		switch {
		case p.want > 0:
			n := min(p.want, len(b))
			p.cap = append(p.cap, b[:n]...)
			p.want -= n
			b = b[n:]
			if p.want == 0 {
				p.report(dir, id)
			}
		case p.skip > 0:
			n := min(p.skip, len(b))
			p.skip -= n
			b = b[n:]
		default:
			n := min(9-len(p.hdr), len(b))
			p.hdr = append(p.hdr, b[:n]...)
			b = b[n:]
			if len(p.hdr) < 9 {
				return
			}
			length := int(p.hdr[0])<<16 | int(p.hdr[1])<<8 | int(p.hdr[2])
			typ := p.hdr[3]
			p.hdr = p.hdr[:0]
			name := h2FrameNames[typ]
			if name == "" {
				name = fmt.Sprintf("T%d", typ)
			}
			p.counts[name]++
			p.skip = length
			if (typ == 7 && length >= 8) || (typ == 3 && length >= 4) {
				p.ctype, p.cap = typ, p.cap[:0]
				p.want = 8
				if typ == 3 {
					p.want = 4
				}
				p.skip = length - p.want
			}
		}
	}
}

func (p *h2Parser) report(dir string, id int64) {
	if p.ctype == 7 {
		log.Printf("[H2] #%d %s GOAWAY last_stream=%d code=%d", id, dir, binary.BigEndian.Uint32(p.cap[:4])&0x7fffffff, binary.BigEndian.Uint32(p.cap[4:8]))
	} else {
		log.Printf("[H2] #%d %s RST_STREAM code=%d", id, dir, binary.BigEndian.Uint32(p.cap[:4]))
	}
}

func (p *h2Parser) summary() string {
	keys := make([]string, 0, len(p.counts))
	for k, v := range p.counts {
		keys = append(keys, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(keys)
	return strings.Join(keys, " ")
}

type traceConn struct {
	net.Conn
	id      int64
	start   time.Time
	mu      sync.Mutex
	in, out h2Parser
	closer  string
	once    sync.Once
}

func (t *traceConn) Read(b []byte) (int, error) {
	n, err := t.Conn.Read(b)
	t.mu.Lock()
	t.in.feed(b[:n], "client->", t.id)
	if err != nil && t.closer == "" {
		t.closer = "client (" + strings.SplitN(err.Error(), ":", 2)[0] + ")"
	}
	t.mu.Unlock()
	return n, err
}

func (t *traceConn) Write(b []byte) (int, error) {
	n, err := t.Conn.Write(b)
	t.mu.Lock()
	t.out.feed(b[:n], "server->", t.id)
	t.mu.Unlock()
	return n, err
}

func (t *traceConn) Close() error {
	t.once.Do(func() {
		t.mu.Lock()
		closer := t.closer
		if closer == "" {
			closer = "server"
		}
		log.Printf("[H2] #%d closed after %s by %s; client frames: %s; server frames: %s",
			t.id, time.Since(t.start).Round(time.Millisecond), closer, t.in.summary(), t.out.summary())
		t.mu.Unlock()
	})
	return t.Conn.Close()
}
