// Package probe implements a controlled TLS termination point for Stardew's
// NPLN tenant traffic. It terminates TLS locally with an in-memory,
// self-generated certificate and logs ONLY sanitized HTTP/2 metadata: frame
// types, flag names, stream IDs, byte counts, timing, the gRPC pseudo-headers
// (:method, :path, :scheme, :authority), and the NAMES of every other header.
//
// It never logs: payload bodies, non-pseudo header values (tokens, cookies,
// authorization data), TLS key material, remote addresses, or certificates.
// Nothing is written to disk by the probe itself; the process log is the only
// output and must be reviewed before any sanitized summary enters the
// repository.
package probe

import (
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"golang.org/x/net/http2/hpack"
)

const clientPreface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

// frameTypeNames maps HTTP/2 frame types (RFC 7540 section 6).
var frameTypeNames = map[byte]string{
	0x0: "DATA",
	0x1: "HEADERS",
	0x2: "PRIORITY",
	0x3: "RST_STREAM",
	0x4: "SETTINGS",
	0x5: "PUSH_PROMISE",
	0x6: "PING",
	0x7: "GOAWAY",
	0x8: "WINDOW_UPDATE",
	0x9: "CONTINUATION",
}

// frameFlagNames maps named per-type flags (RFC 7540 sections 6.1-6.10).
var frameFlagNames = map[byte]map[byte]string{
	0x1: {0x1: "END_STREAM", 0x4: "END_HEADERS", 0x8: "PADDED", 0x20: "PRIORITY"},
	0x0: {0x1: "END_STREAM", 0x8: "PADDED"},
	0x4: {0x1: "ACK"},
	0x6: {0x1: "ACK"},
	0x5: {0x4: "END_HEADERS", 0x8: "PADDED", 0x20: "PRIORITY"},
	0x9: {0x4: "END_HEADERS"},
}

// errorCodeNames maps HTTP/2 error codes (RFC 7540 section 7). Error codes are
// protocol metadata, not payload content.
var errorCodeNames = map[uint32]string{
	0x0: "NO_ERROR", 0x1: "PROTOCOL_ERROR", 0x2: "INTERNAL_ERROR",
	0x3: "FLOW_CONTROL_ERROR", 0x5: "STREAM_CLOSED", 0x6: "FRAME_SIZE_ERROR",
	0x7: "REFUSED_STREAM", 0x8: "CANCEL", 0xa: "CONNECT_ERROR",
	0xb: "ENHANCE_YOUR_CALM", 0xc: "INADEQUATE_SECURITY",
}

// settingNames maps HTTP/2 settings identifiers (RFC 7540 section 6.5.2).
// Only the identifier is logged, never the value.
var settingNames = map[uint16]string{
	0x1: "HEADER_TABLE_SIZE", 0x2: "ENABLE_PUSH", 0x3: "MAX_CONCURRENT_STREAMS",
	0x4: "INITIAL_WINDOW_SIZE", 0x5: "MAX_FRAME_SIZE", 0x6: "MAX_HEADER_LIST_SIZE",
}

// pseudoHeaders are the only header values the probe ever records. They are
// the experiment's target: the first gRPC method Stardew calls. The :path of
// a gRPC request is "/package.Service/Method" by construction.
var pseudoHeaders = map[string]bool{
	":method": true, ":path": true, ":scheme": true, ":authority": true,
}

// ConnHandler serves one terminated TLS connection.
type ConnHandler struct {
	id     int
	conn   net.Conn
	logger *log.Logger
	start  time.Time
	hdec   *hpack.Decoder
	// headersBuf accumulates a HEADERS block split across CONTINUATION frames.
	headersBuf []byte
	inHeaders  bool
	// streams tracks request streams awaiting a response (stream id -> seen END_STREAM).
	streamEnded map[uint32]bool
}

// HandleConn terminates TLS, speaks the minimum HTTP/2 server side needed to
// keep the peer talking (server preface, SETTINGS ACK, PING ACK), and logs
// sanitized metadata for everything the peer sends. It never sends an
// application response: the goal is to observe what the client does first.
func HandleConn(id int, raw net.Conn, tlsConn *tls.Conn, logger *log.Logger) {
	h := &ConnHandler{id: id, conn: tlsConn, logger: logger, start: time.Now(),
		streamEnded: make(map[uint32]bool)}

	hsDeadline := time.Now().Add(15 * time.Second)
	_ = tlsConn.SetDeadline(hsDeadline)
	handshakeStart := time.Now()
	if err := tlsConn.Handshake(); err != nil {
		h.logf("handshake failed after %s: %v", time.Since(handshakeStart), err)
		return
	}
	state := tlsConn.ConnectionState()
	alpn := state.NegotiatedProtocol
	if alpn == "" {
		alpn = "(none)"
	}
	h.logf("TLS established: version=%s cipher=%s alpn=%s handshake=%s",
		tlsVersionName(state.Version), tlsCipherName(state.CipherSuite), alpn,
		time.Since(handshakeStart))

	// Server connection preface (RFC 7540 section 3.5): SETTINGS. Mirrors a
	// default grpc-go server (a reference NPLN server is grpc.NewServer()
	// with no transport options): a single MAX_FRAME_SIZE=16384 setting, no
	// window updates, no MAX_CONCURRENT_STREAMS (server default is unlimited).
	_ = tlsConn.SetDeadline(time.Now().Add(120 * time.Second))
	settings := make([]byte, 6)
	binary.BigEndian.PutUint16(settings[0:2], 0x5) // MAX_FRAME_SIZE
	binary.BigEndian.PutUint32(settings[2:6], 16384)
	if err := writeFrame(tlsConn, 0x4, 0, 0, settings); err != nil {
		h.logf("write server preface failed: %v", err)
		return
	}

	h.hdec = hpack.NewDecoder(4096, h.headerField)

	if _, err := io.ReadFull(tlsConn, make([]byte, len(clientPreface))); err != nil {
		h.logf("no client preface: %v", err)
		return
	}
	h.logf("client preface ok (+%s)", time.Since(h.start).Round(time.Microsecond))

	buf := make([]byte, 9)
	for {
		if _, err := io.ReadFull(tlsConn, buf); err != nil {
			if err == io.EOF || isTimeout(err) {
				h.logf("client closed (+%s, EOF on read)", time.Since(h.start).Round(time.Millisecond))
			} else {
				h.logf("read error after %+v: %v", time.Since(h.start).Round(time.Millisecond), err)
			}
			return
		}
		length := int(buf[0])<<16 | int(buf[1])<<8 | int(buf[2])
		typ := buf[3]
		flags := buf[4]
		streamID := binary.BigEndian.Uint32(buf[5:9]) & 0x7fffffff
		payload := make([]byte, length)
		if length > 0 {
			if _, err := io.ReadFull(tlsConn, payload); err != nil {
				h.logf("read error (frame body) after %+v: %v", time.Since(h.start).Round(time.Millisecond), err)
				return
			}
		}
		if !h.frame(typ, flags, streamID, payload) {
			return
		}
	}
}

// frame processes one received frame. It returns false when the connection
// should be closed.
func (h *ConnHandler) frame(typ, flags byte, streamID uint32, payload []byte) bool {
	elapsed := time.Since(h.start).Round(time.Microsecond)
	name := frameTypeNames[typ]
	if name == "" {
		name = fmt.Sprintf("type_0x%02x", typ)
	}
	h.logf("%s len=%d flags=[%s] stream=%d (+%s)", name, len(payload),
		flagNames(typ, flags), streamID, elapsed)

	switch typ {
	case 0x1: // HEADERS
		block, ok := stripHeadersPadding(payload, flags)
		if !ok {
			h.logf("malformed HEADERS padding; closing")
			return false
		}
		if flags&0x04 != 0 { // END_HEADERS: decode now
			h.decodeHeaders(block)
		} else {
			h.headersBuf = append(h.headersBuf[:0], block...)
			h.inHeaders = true
		}
		// A request stream opened: remember it so we can answer after END_STREAM.
		h.streamSeen(streamID, flags&0x01 != 0)
	case 0x9: // CONTINUATION
		if h.inHeaders {
			h.headersBuf = append(h.headersBuf, payload...)
			if flags&0x04 != 0 {
				h.inHeaders = false
				h.decodeHeaders(h.headersBuf)
			}
		}
	case 0x4: // SETTINGS
		if flags&0x01 == 0 {
			if len(payload)%6 != 0 {
				h.logf("malformed SETTINGS; closing")
				return false
			}
			ids := make([]string, 0, len(payload)/6)
			for i := 0; i+6 <= len(payload); i += 6 {
				id := binary.BigEndian.Uint16(payload[i : i+2])
				if n, ok := settingNames[id]; ok {
					ids = append(ids, n)
				} else {
					ids = append(ids, fmt.Sprintf("0x%04x", id))
				}
			}
			h.logf("settings ids=[%s]", strings.Join(ids, ", "))
			if err := writeFrame(h.conn, 0x4, 0x1, 0, nil); err != nil {
				h.logf("write SETTINGS ack failed: %v", err)
				return false
			}
		}
	case 0x6: // PING
		if flags&0x01 == 0 && len(payload) == 8 {
			// Echo the opaque payload as ACK (spec requirement); never log it.
			if err := writeFrame(h.conn, 0x6, 0x1, 0, payload); err != nil {
				h.logf("write PING ack failed: %v", err)
				return false
			}
		}
	case 0x3: // RST_STREAM
		if len(payload) == 4 {
			h.logf("rst error=%s", errorCodeName(binary.BigEndian.Uint32(payload)))
		}
	case 0x7: // GOAWAY
		if len(payload) >= 8 {
			h.logf("goaway last_stream=%d error=%s",
				binary.BigEndian.Uint32(payload)&0x7fffffff,
				errorCodeName(binary.BigEndian.Uint32(payload[4:8])))
		}
	case 0x0, 0x2, 0x8:
		// DATA, PRIORITY, WINDOW_UPDATE: length/stream already logged above.
		// DATA payload content is never read into logs.
		if typ == 0x0 && flags&0x01 != 0 {
			h.streamSeen(streamID, true)
		}
	}
	return true
}

// streamSeen records request-stream progress and answers a completed request
// with a minimal, valid gRPC trailers-only response: HTTP/2 HEADERS carrying
// :status=200, content-type=application/grpc, grpc-status=12 (UNIMPLEMENTED)
// and END_STREAM. This is the standard gRPC answer for an unknown method —
// honest (the probe implements no service) and sufficient to learn whether
// the peer accepts application responses at all.
func (h *ConnHandler) streamSeen(streamID uint32, ended bool) {
	prev, exists := h.streamEnded[streamID]
	if !exists {
		h.streamEnded[streamID] = ended
		return
	}
	if prev || !ended {
		return // already answered, or still open
	}
	h.streamEnded[streamID] = true
	block := encodeLiteralHeaders([][2]string{
		{":status", "200"},
		{"content-type", "application/grpc"},
		{"grpc-status", "12"},
	})
	if err := writeFrame(h.conn, 0x1, 0x04|0x01, streamID, block); err != nil { // END_HEADERS|END_STREAM
		h.logf("write grpc response failed: %v", err)
		return
	}
	h.logf("responded stream=%d grpc-status=12 (trailers-only)", streamID)
}

// encodeLiteralHeaders serializes header pairs as HPACK literal header field
// without indexing, new name, no Huffman (RFC 7541 section 6.2.2) — the
// simplest valid encoding, accepted by every HPACK decoder.
func encodeLiteralHeaders(pairs [][2]string) []byte {
	var buf []byte
	for _, kv := range pairs {
		buf = append(buf, 0x00) // literal, no indexing, new name, no Huffman
		buf = appendHPACKString(buf, kv[0])
		buf = appendHPACKString(buf, kv[1])
	}
	return buf
}

func appendHPACKString(buf []byte, s string) []byte {
	// 7-bit length prefix, H bit = 0.
	l := len(s)
	if l < 127 {
		buf = append(buf, byte(l))
	} else {
		for l >= 128 {
			buf = append(buf, byte(l&0x7f)|0x80)
			l >>= 7
		}
		buf = append(buf, byte(l))
	}
	return append(buf, s...)
}

// decodeHeaders HPACK-decodes a completed header block through the sanitize
// rules in headerField.
func (h *ConnHandler) decodeHeaders(block []byte) {
	if _, err := h.hdec.Write(block); err != nil {
		// A decoding error can mean compression-state divergence; log the
		// failure without dumping the block.
		h.logf("hpack decode error: %v", err)
	}
}

// headerField is the single sanitize gate for every decoded header.
// Pseudo-headers (:method/:path/:scheme/:authority) are recorded with values
// because they ARE the experiment's target. Every other header is recorded by
// NAME and value length only; the value is never copied into the log.
func (h *ConnHandler) headerField(hf hpack.HeaderField) {
	name := hf.Name
	if pseudoHeaders[name] {
		value := hf.Value
		if len(value) > 512 {
			value = value[:512] + "...(truncated)"
		}
		h.logf("header %s=%q", name, value)
		return
	}
	h.logf("header %s=<redacted len=%d>", name, len(hf.Value))
}

func (h *ConnHandler) logf(format string, args ...any) {
	h.logger.Printf("conn=%d "+format, append([]any{h.id}, args...)...)
}

// stripHeadersPadding removes PADDED/PRIORITY framing from a HEADERS payload,
// returning the raw header block fragment.
func stripHeadersPadding(payload []byte, flags byte) ([]byte, bool) {
	block := payload
	if flags&0x08 != 0 { // PADDED
		if len(block) < 1 {
			return nil, false
		}
		pad := int(block[0])
		block = block[1:]
		if pad > len(block) {
			return nil, false
		}
		block = block[:len(block)-pad]
	}
	if flags&0x20 != 0 { // PRIORITY
		if len(block) < 5 {
			return nil, false
		}
		block = block[5:]
	}
	return block, true
}

func writeFrame(w io.Writer, typ, flags byte, streamID uint32, payload []byte) error {
	hdr := make([]byte, 9)
	n := len(payload)
	hdr[0], hdr[1], hdr[2] = byte(n>>16), byte(n>>8), byte(n)
	hdr[3], hdr[4] = typ, flags
	binary.BigEndian.PutUint32(hdr[5:9], streamID)
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if n > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func flagNames(typ, flags byte) string {
	names := frameFlagNames[typ]
	if flags == 0 {
		return "-"
	}
	var out []string
	for bit := byte(1); bit != 0; bit <<= 1 {
		if flags&bit != 0 {
			if n, ok := names[bit]; ok {
				out = append(out, n)
			} else {
				out = append(out, fmt.Sprintf("0x%02x", bit))
			}
		}
	}
	return strings.Join(out, "|")
}

func errorCodeName(code uint32) string {
	if n, ok := errorCodeNames[code]; ok {
		return n
	}
	return fmt.Sprintf("0x%08x", code)
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
