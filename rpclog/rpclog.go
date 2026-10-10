// Package rpclog puts the content of every gRPC call in the log, for every NPLN title, so a
// console session answers "what did it send?" without a redeploy.
//
// By default a message is logged as its outline: which fields are set, enum and bool values, the
// length of every string and bytes field, and nothing else -- a request carries names, ids, tokens
// and addresses, and a log keeps none of them. With NPLN_TRACE_BODIES=1 (never set in production)
// the whole message is logged: fields whose name contains "token" as their length and shape (JWT or
// not), never their value, and fields the proto does not know as hex, which is where a
// message-shape mismatch shows up.
//
// ponytail: every message of every stream is logged, both ways, gamesync included; a busy session is a
// busy log. Lines are capped at maxLine. Add a per-method filter if a title's traffic drowns it.
package rpclog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/openpak/npln/redact"
)

const maxLine = 4096

// Body renders m for the log: compact text, secrets reduced to their shape, unknown fields in hex.
func Body(m any) (out string) {
	pm, ok := m.(proto.Message)
	if !ok || pm == nil {
		return fmt.Sprintf("%T", m)
	}
	// Logging must never take a call down with it; a message that cannot be rendered says so.
	defer func() {
		if r := recover(); r != nil {
			out = fmt.Sprintf("%T (unrenderable: %v)", m, r)
		}
	}()
	if !redact.TraceBodies() {
		return outline(pm)
	}
	c := proto.Clone(pm)
	var secrets []string
	u := unknown(c.ProtoReflect())
	scrub(c.ProtoReflect(), "", &secrets) // also strips unknown bytes: prototext panics on malformed ones
	s := prototext.MarshalOptions{}.Format(c)
	if u != "" {
		s += " unknown=" + u
	}
	if len(secrets) > 0 {
		s += " " + strings.Join(secrets, " ")
	}
	if s == "" {
		s = "{}"
	}
	if len(s) > maxLine {
		s = s[:maxLine] + fmt.Sprintf("…(%d bytes)", len(s))
	}
	return s
}

// outline renders m without any value a person could be read from.
func outline(pm proto.Message) string {
	var b strings.Builder
	outlineMsg(&b, pm.ProtoReflect())
	if n := len(pm.ProtoReflect().GetUnknown()); n > 0 {
		fmt.Fprintf(&b, " unknown=<%d bytes>", n)
	}
	s := b.String()
	if len(s) > maxLine {
		s = s[:maxLine] + fmt.Sprintf("…(%d bytes)", len(s))
	}
	return s
}

func outlineMsg(b *strings.Builder, m protoreflect.Message) {
	b.WriteByte('{')
	fields := m.Descriptor().Fields()
	first := true
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if !m.Has(fd) {
			continue
		}
		if !first {
			b.WriteByte(' ')
		}
		first = false
		b.WriteString(string(fd.Name()) + "=")
		v := m.Get(fd)
		switch {
		case fd.IsMap():
			fmt.Fprintf(b, "map(%d)", v.Map().Len())
		case fd.IsList():
			fmt.Fprintf(b, "[%d]", v.List().Len())
		default:
			outlineValue(b, fd, v)
		}
	}
	b.WriteByte('}')
}

func outlineValue(b *strings.Builder, fd protoreflect.FieldDescriptor, v protoreflect.Value) {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		outlineMsg(b, v.Message())
	case protoreflect.StringKind:
		fmt.Fprintf(b, "<%d chars>", len(v.String()))
	case protoreflect.BytesKind:
		fmt.Fprintf(b, "<%d bytes>", len(v.Bytes()))
	case protoreflect.BoolKind:
		fmt.Fprint(b, v.Bool())
	case protoreflect.EnumKind:
		if ev := fd.Enum().Values().ByNumber(v.Enum()); ev != nil {
			b.WriteString(string(ev.Name()))
		} else {
			b.WriteString("<enum>")
		}
	default:
		b.WriteByte('#')
	}
}

// rawOutline lists the fields of an undecoded message as number and wire type, with the length of
// the length-delimited ones.
func rawOutline(raw []byte) string {
	var parts []string
	for len(raw) > 0 {
		num, typ, n := protowire.ConsumeTag(raw)
		if n < 0 {
			parts = append(parts, "<malformed>")
			break
		}
		raw = raw[n:]
		n = protowire.ConsumeFieldValue(num, typ, raw)
		if n < 0 {
			parts = append(parts, fmt.Sprintf("%d:<malformed>", num))
			break
		}
		switch typ {
		case protowire.BytesType:
			_, vn := protowire.ConsumeVarint(raw)
			parts = append(parts, fmt.Sprintf("%d:len%d", num, n-vn))
		case protowire.VarintType:
			parts = append(parts, fmt.Sprintf("%d:varint", num))
		default:
			parts = append(parts, fmt.Sprintf("%d:%d", num, typ))
		}
		raw = raw[n:]
	}
	s := strings.Join(parts, " ")
	if len(s) > maxLine {
		s = s[:maxLine] + "…"
	}
	return "fields=" + s
}

// scrub clears token fields (recording their shape in secrets) and unknown bytes, and recurses
// into messages.
func scrub(m protoreflect.Message, path string, secrets *[]string) {
	m.SetUnknown(nil)
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		name := path + string(fd.Name())
		switch {
		case fd.Kind() == protoreflect.StringKind && !fd.IsList() && !fd.IsMap() &&
			strings.Contains(strings.ToLower(string(fd.Name())), "token"):
			*secrets = append(*secrets, fmt.Sprintf("%s=<%s>", name, shape(v.String())))
			m.Clear(fd)
		case fd.Kind() == protoreflect.MessageKind && !fd.IsList() && !fd.IsMap():
			scrub(v.Message(), name+".", secrets)
		case fd.Kind() == protoreflect.MessageKind && fd.IsList():
			for i := 0; i < v.List().Len(); i++ {
				scrub(v.List().Get(i).Message(), fmt.Sprintf("%s[%d].", name, i), secrets)
			}
		}
		return true
	})
}

// shape describes a secret without revealing it: length, what it looks like, and a short
// fingerprint (first 8 hex of its SHA-256) to compare against a candidate value offline.
func shape(s string) string {
	kind := "not a jwt"
	switch {
	case strings.Count(s, ".") == 2 && strings.HasPrefix(s, "ey"):
		kind = "jwt"
	case s != "" && strings.Trim(s, "0123456789abcdefABCDEF") == "":
		kind = "not a jwt, hex"
	case s != "" && strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r > 0x7e }) < 0:
		kind = "not a jwt, printable"
	case s != "":
		kind = "not a jwt, binary"
	}
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%d bytes, %s, sha256 %x", len(s), kind, sum[:4])
}

// unknown collects the raw bytes of fields our proto does not define, at any depth.
func unknown(m protoreflect.Message) string {
	var out []string
	if u := m.GetUnknown(); len(u) > 0 {
		out = append(out, hex.EncodeToString(u))
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() == protoreflect.MessageKind && !fd.IsList() && !fd.IsMap() {
			if u := unknown(v.Message()); u != "" {
				out = append(out, string(fd.Name())+":"+u)
			}
		}
		return true
	})
	return strings.Join(out, " ")
}

// Unary logs the request and, when there is one, the response of every unary call.
func Unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	log.Printf("[RPC>] %s %s", info.FullMethod, Body(req))
	resp, err := h(ctx, req)
	if err == nil {
		log.Printf("[RPC<] %s %s", info.FullMethod, Body(resp))
	}
	return resp, err
}

// Stream logs every message on a stream, both ways.
func Stream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
	return h(srv, &streamLogger{ServerStream: ss, method: info.FullMethod})
}

type streamLogger struct {
	grpc.ServerStream
	method string
}

func (r *streamLogger) SendMsg(m any) error {
	log.Printf("[RPC<] %s %s", r.method, Body(m))
	return r.ServerStream.SendMsg(m)
}

func (r *streamLogger) RecvMsg(m any) error {
	err := r.ServerStream.RecvMsg(m)
	if err == nil {
		log.Printf("[RPC>] %s %s", r.method, Body(m))
	}
	return err
}

// Unimplemented logs the first message of a call nobody serves, decoded as raw fields, so the
// request of the next handler to write is in the log already. It waits at most a second: a
// client that sends nothing before our answer must still get its Unimplemented.
func Unimplemented(ss grpc.ServerStream) {
	m, _ := grpc.MethodFromServerStream(ss)
	got := make(chan string, 1)
	go func() {
		var e emptypb.Empty // every field lands in unknown bytes
		if err := ss.RecvMsg(&e); err != nil {
			got <- "(no message: " + redact.Err(err) + ")"
			return
		}
		if redact.TraceBodies() {
			got <- "raw=" + hex.EncodeToString(e.ProtoReflect().GetUnknown())
		} else {
			got <- rawOutline(e.ProtoReflect().GetUnknown())
		}
	}()
	select {
	case b := <-got:
		log.Printf("[RPC>] %s %s", m, b)
	case <-time.After(time.Second):
		log.Printf("[RPC>] %s (no message within 1s)", m)
	}
}
