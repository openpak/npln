// Package rpclog puts the content of every gRPC call in the log, for every NPLN title, so a
// console session answers "what did it send?" without a redeploy.
//
// Fields whose name contains "token" are logged as their length and shape (JWT or not), never
// their value: those are bearer credentials. Fields the proto does not know are logged as hex,
// which is where a message-shape mismatch shows up.
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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
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
	c := proto.Clone(pm)
	var secrets []string
	u := unknown(c.ProtoReflect())
	redact(c.ProtoReflect(), "", &secrets) // also strips unknown bytes: prototext panics on malformed ones
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

// redact clears token fields (recording their shape in secrets) and unknown bytes, and recurses
// into messages.
func redact(m protoreflect.Message, path string, secrets *[]string) {
	m.SetUnknown(nil)
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		name := path + string(fd.Name())
		switch {
		case fd.Kind() == protoreflect.StringKind && !fd.IsList() && !fd.IsMap() &&
			strings.Contains(strings.ToLower(string(fd.Name())), "token"):
			*secrets = append(*secrets, fmt.Sprintf("%s=<%s>", name, shape(v.String())))
			m.Clear(fd)
		case fd.Kind() == protoreflect.MessageKind && !fd.IsList() && !fd.IsMap():
			redact(v.Message(), name+".", secrets)
		case fd.Kind() == protoreflect.MessageKind && fd.IsList():
			for i := 0; i < v.List().Len(); i++ {
				redact(v.List().Get(i).Message(), fmt.Sprintf("%s[%d].", name, i), secrets)
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
			got <- "(no message: " + err.Error() + ")"
			return
		}
		got <- "raw=" + hex.EncodeToString(e.ProtoReflect().GetUnknown())
	}()
	select {
	case b := <-got:
		log.Printf("[RPC>] %s %s", m, b)
	case <-time.After(time.Second):
		log.Printf("[RPC>] %s (no message within 1s)", m)
	}
}
