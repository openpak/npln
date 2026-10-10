package npln

import commonpb "github.com/openpak/npln/proto/common"

// Concrete typed Values. Every field of a document the game reads must carry one of these, never
// a type-less field, which crashes its worker (2162-0001).
func sv(s string) *commonpb.Value {
	return &commonpb.Value{ValueType: &commonpb.Value_StringValue{StringValue: s}}
}

func iv(i int64) *commonpb.Value {
	return &commonpb.Value{ValueType: &commonpb.Value_IntegerValue{IntegerValue: i}}
}
