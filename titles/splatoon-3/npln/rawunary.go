package npln

// rawunary — unary methods that must answer in a shape a typed grpc-go handler cannot produce.
//
// Nintendo's gateway answers some calls with NO response message at all: a HEADERS frame
// (content-type, npln-grpc-type: Unary, content-length: 0), no DATA, then the trailers. The client
// SDK reads that as "a default object" (UserScreening/GetViolation is one). A typed handler always
// sends one message, and a handler that sends nothing at all lets grpc-go fold everything into a
// single Trailers-Only frame, a shape the client is known not to accept. So these methods are
// registered as stream handlers that send the headers first and then decide.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	errpb "github.com/openpak/npln/proto/errdetails"
)

// rawHandler serves one unary method on its server stream.
type rawHandler func(srv any, ss grpc.ServerStream) error

// withRawUnary returns a copy of desc whose named unary methods are served by the given raw
// handlers instead of the generated typed ones. The other methods are untouched.
func withRawUnary(desc grpc.ServiceDesc, raw map[string]rawHandler) *grpc.ServiceDesc {
	out := desc
	out.Methods = nil
	for _, m := range desc.Methods {
		if _, ok := raw[m.MethodName]; !ok {
			out.Methods = append(out.Methods, m)
		}
	}
	out.Streams = append([]grpc.StreamDesc(nil), desc.Streams...)
	for name, h := range raw {
		out.Streams = append(out.Streams, grpc.StreamDesc{StreamName: name, Handler: grpc.StreamHandler(h)})
	}
	return &out
}

// sendEmptyHeaders sends the first of the two frames of a body-less answer, as Nintendo's gateway
// does. After it, returning nil ends the call with no message and OK; returning a status error
// ends it with that status in a separate trailers frame.
func sendEmptyHeaders(ss grpc.ServerStream) error {
	return ss.SendHeader(metadata.Pairs("content-length", "0"))
}

// notFound is NOT_FOUND the way Nintendo's services word it: the message names the missing
// resource, and the status details carry an nn.npln.errdetails.NError with a fresh trace id
// (field 1, 32 hex), a fresh span-like id (field 19, 16 hex; not in our schema) and the generic
// not-found detail code (field 6 = 0). Measured on a real console capture for an absent ugcstore
// document (docs/provenance.md); the game retried without end against a bare NOT_FOUND there.
func notFound(kind, name string) error {
	ne := &errpb.NError{TraceId: randHex(16), DetailCodeType: &errpb.NError_NotFoundDetailCode_{NotFoundDetailCode: errpb.NError_NOT_FOUND_GENERIC}}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(ne)
	if err != nil {
		return status.Error(codes.NotFound, "not found")
	}
	b = protowire.AppendTag(b, 19, protowire.BytesType)
	b = protowire.AppendString(b, randHex(8))
	msg := fmt.Sprintf(`NotFound. reason: "%s:"%s" Not Found"`, kind, name)
	return status.ErrorProto(&spb.Status{Code: int32(codes.NotFound), Message: msg,
		Details: []*anypb.Any{{TypeUrl: "type.googleapis.com/nn.npln.errdetails.NError", Value: b}}})
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
