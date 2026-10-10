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
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
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
