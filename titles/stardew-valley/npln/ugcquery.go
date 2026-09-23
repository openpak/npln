package npln

// ugcquery — evaluating a ugcstore StructuredQuery (Firestore's shape) over stored documents:
// collection selection, filters, ordering, cursors, offset, limit and projection. Values order
// by type first (null, boolean, number, timestamp, string, bytes, reference, array, map), then
// by value, as Firestore documents it.

import (
	"bytes"
	"math"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"

	commonpb "github.com/openpak/npln/proto/common"
	ugcpb "github.com/openpak/npln/proto/ugcstore/v1"
)

// fieldPath splits a dotted field path; each segment may be back-quoted (`ut`, `a.b`.c).
func fieldPath(p string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range p {
		switch {
		case r == '`':
			quoted = !quoted
		case r == '.' && !quoted:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(out, cur.String())
}

// lookupField finds a (possibly nested) field; nil when absent.
func lookupField(m *commonpb.MapValue, path string) *commonpb.Value {
	segs := fieldPath(path)
	for i, s := range segs {
		v := m.GetFields()[s]
		if v == nil || i == len(segs)-1 {
			return v
		}
		m = v.GetMapValue()
	}
	return nil
}

// setField sets a (possibly nested) field in place, creating maps on the way; nil deletes it.
func setField(m *commonpb.MapValue, path string, v *commonpb.Value) {
	segs := fieldPath(path)
	for _, s := range segs[:len(segs)-1] {
		if m.Fields == nil {
			m.Fields = map[string]*commonpb.Value{}
		}
		next := m.Fields[s].GetMapValue()
		if next == nil {
			next = &commonpb.MapValue{}
			m.Fields[s] = &commonpb.Value{ValueType: &commonpb.Value_MapValue{MapValue: next}}
		}
		m = next
	}
	last := segs[len(segs)-1]
	if v == nil {
		delete(m.Fields, last)
		return
	}
	if m.Fields == nil {
		m.Fields = map[string]*commonpb.Value{}
	}
	m.Fields[last] = v
}

func typeRank(v *commonpb.Value) int {
	switch v.GetValueType().(type) {
	case nil, *commonpb.Value_NullValue:
		return 0
	case *commonpb.Value_BooleanValue:
		return 1
	case *commonpb.Value_IntegerValue, *commonpb.Value_FloatValue, *commonpb.Value_DoubleValue:
		return 2
	case *commonpb.Value_TimestampValue:
		return 3
	case *commonpb.Value_StringValue:
		return 4
	case *commonpb.Value_BytesValue:
		return 5
	case *commonpb.Value_ReferenceValue:
		return 6
	case *commonpb.Value_ArrayValue:
		return 7
	}
	return 8
}

func number(v *commonpb.Value) float64 {
	switch x := v.GetValueType().(type) {
	case *commonpb.Value_IntegerValue:
		return float64(x.IntegerValue)
	case *commonpb.Value_FloatValue:
		return float64(x.FloatValue)
	case *commonpb.Value_DoubleValue:
		return x.DoubleValue
	}
	return 0
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compareValues orders two values; 0 means equal.
func compareValues(a, b *commonpb.Value) int {
	if c := cmpInt(typeRank(a), typeRank(b)); c != 0 {
		return c
	}
	switch typeRank(a) {
	case 1:
		return cmpInt(btoi(a.GetBooleanValue()), btoi(b.GetBooleanValue()))
	case 2:
		if ai, aok := a.GetValueType().(*commonpb.Value_IntegerValue); aok {
			if bi, bok := b.GetValueType().(*commonpb.Value_IntegerValue); bok {
				switch {
				case ai.IntegerValue < bi.IntegerValue:
					return -1
				case ai.IntegerValue > bi.IntegerValue:
					return 1
				}
				return 0
			}
		}
		x, y := number(a), number(b)
		switch {
		case math.IsNaN(x) && math.IsNaN(y):
			return 0
		case math.IsNaN(x) || x < y:
			return -1
		case math.IsNaN(y) || x > y:
			return 1
		}
		return 0
	case 3:
		ta, tb := a.GetTimestampValue(), b.GetTimestampValue()
		if c := cmpInt64(ta.GetSeconds(), tb.GetSeconds()); c != 0 {
			return c
		}
		return cmpInt(int(ta.GetNanos()), int(tb.GetNanos()))
	case 4:
		return strings.Compare(a.GetStringValue(), b.GetStringValue())
	case 5:
		return bytes.Compare(a.GetBytesValue(), b.GetBytesValue())
	case 6:
		return strings.Compare(a.GetReferenceValue(), b.GetReferenceValue())
	case 7:
		av, bv := a.GetArrayValue().GetValues(), b.GetArrayValue().GetValues()
		for i := 0; i < len(av) && i < len(bv); i++ {
			if c := compareValues(av[i], bv[i]); c != 0 {
				return c
			}
		}
		return cmpInt(len(av), len(bv))
	case 8:
		if proto.Equal(a, b) {
			return 0
		}
		ab, _ := proto.MarshalOptions{Deterministic: true}.Marshal(a)
		bb, _ := proto.MarshalOptions{Deterministic: true}.Marshal(b)
		return bytes.Compare(ab, bb)
	}
	return 0
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func inArray(arr *commonpb.ArrayValue, v *commonpb.Value) bool {
	for _, x := range arr.GetValues() {
		if compareValues(x, v) == 0 {
			return true
		}
	}
	return false
}

// matches evaluates a filter against a document's fields. A field filter on a missing field
// never matches (Firestore's rule), NOT_EQUAL and NOT_IN included.
func matches(f *ugcpb.StructuredQuery_Filter, fields *commonpb.MapValue) bool {
	switch x := f.GetFilterType().(type) {
	case nil:
		return true
	case *ugcpb.StructuredQuery_Filter_CompositeFilter:
		for _, sub := range x.CompositeFilter.GetFilters() {
			if !matches(sub, fields) {
				return false
			}
		}
		return true
	case *ugcpb.StructuredQuery_Filter_UnaryFilter:
		v := lookupField(fields, x.UnaryFilter.GetField().GetFieldPath())
		switch x.UnaryFilter.GetOp() {
		case ugcpb.StructuredQuery_UnaryFilter_IS_NULL:
			_, isNull := v.GetValueType().(*commonpb.Value_NullValue)
			return isNull
		case ugcpb.StructuredQuery_UnaryFilter_IS_NAN:
			return typeRank(v) == 2 && math.IsNaN(number(v))
		}
		return false
	case *ugcpb.StructuredQuery_Filter_FieldFilter:
		ff := x.FieldFilter
		v := lookupField(fields, ff.GetField().GetFieldPath())
		if v == nil {
			return false
		}
		want := ff.GetValue()
		c := compareValues(v, want)
		sameType := typeRank(v) == typeRank(want)
		switch ff.GetOp() {
		case ugcpb.StructuredQuery_FieldFilter_LESS_THAN:
			return sameType && c < 0
		case ugcpb.StructuredQuery_FieldFilter_LESS_THAN_OR_EQUAL:
			return sameType && c <= 0
		case ugcpb.StructuredQuery_FieldFilter_GREATER_THAN:
			return sameType && c > 0
		case ugcpb.StructuredQuery_FieldFilter_GREATER_THAN_OR_EQUAL:
			return sameType && c >= 0
		case ugcpb.StructuredQuery_FieldFilter_EQUAL:
			return c == 0
		case ugcpb.StructuredQuery_FieldFilter_NOT_EQUAL:
			return c != 0
		case ugcpb.StructuredQuery_FieldFilter_ARRAY_CONTAINS:
			return inArray(v.GetArrayValue(), want)
		case ugcpb.StructuredQuery_FieldFilter_IN:
			return inArray(want.GetArrayValue(), v)
		case ugcpb.StructuredQuery_FieldFilter_NOT_IN:
			return !inArray(want.GetArrayValue(), v)
		case ugcpb.StructuredQuery_FieldFilter_ARRAY_CONTAINS_ANY:
			for _, w := range want.GetArrayValue().GetValues() {
				if inArray(v.GetArrayValue(), w) {
					return true
				}
			}
		}
		return false
	}
	return false
}

// inCollection: path (a document path relative to …/documents) is in collection coll directly
// under parent, or anywhere below it when all is set.
func inCollection(path, parent, coll string, all bool) bool {
	rest := path
	if parent != "" {
		if !strings.HasPrefix(path, parent+"/") {
			return false
		}
		rest = path[len(parent)+1:]
	}
	segs := strings.Split(rest, "/")
	if len(segs) < 2 || len(segs)%2 != 0 {
		return false
	}
	if !all {
		return len(segs) == 2 && segs[0] == coll
	}
	return segs[len(segs)-2] == coll
}

type qdoc struct {
	path   string
	fields *commonpb.MapValue
	doc    *ugcpb.Document
}

// runQuery selects, filters, orders, pages and projects. parent is relative to …/documents
// ("" for the root). Returns the selected documents (projected copies) and how many the
// offset skipped.
func runQuery(q *ugcpb.StructuredQuery, parent string, all []qdoc, maxLimit int) ([]*ugcpb.Document, int32) {
	var sel []qdoc
	for _, d := range all {
		in := len(q.GetFrom()) == 0
		for _, c := range q.GetFrom() {
			if inCollection(d.path, parent, c.GetCollectionId(), c.GetAllDescendants()) {
				in = true
			}
		}
		if !in || !matches(q.GetWhere(), d.fields) {
			continue
		}
		ok := true
		for _, o := range q.GetOrderBy() { // ordering by a field leaves out documents without it
			if lookupField(d.fields, o.GetField().GetFieldPath()) == nil && o.GetField().GetFieldPath() != "__name__" {
				ok = false
			}
		}
		if ok {
			sel = append(sel, d)
		}
	}
	key := func(d qdoc, o *ugcpb.StructuredQuery_Order) *commonpb.Value {
		if o.GetField().GetFieldPath() == "__name__" {
			return &commonpb.Value{ValueType: &commonpb.Value_ReferenceValue{ReferenceValue: d.path}}
		}
		return lookupField(d.fields, o.GetField().GetFieldPath())
	}
	cmp := func(a, b qdoc) int {
		for _, o := range q.GetOrderBy() {
			c := compareValues(key(a, o), key(b, o))
			if o.GetDirection() == ugcpb.StructuredQuery_Order_DESCENDING {
				c = -c
			}
			if c != 0 {
				return c
			}
		}
		return strings.Compare(a.path, b.path)
	}
	sort.SliceStable(sel, func(i, j int) bool { return cmp(sel[i], sel[j]) < 0 })

	// cursor position: compare the document's order-by values with the cursor's
	cursorCmp := func(d qdoc, c *ugcpb.Cursor) int {
		for i, v := range c.GetValues() {
			if i >= len(q.GetOrderBy()) {
				break
			}
			o := q.GetOrderBy()[i]
			x := compareValues(key(d, o), v)
			if o.GetDirection() == ugcpb.StructuredQuery_Order_DESCENDING {
				x = -x
			}
			if x != 0 {
				return x
			}
		}
		return 0
	}
	if c := q.GetStartAt(); c != nil {
		kept := sel[:0]
		for _, d := range sel {
			x := cursorCmp(d, c)
			if x > 0 || (x == 0 && c.GetBefore()) {
				kept = append(kept, d)
			}
		}
		sel = kept
	}
	if c := q.GetEndAt(); c != nil {
		kept := sel[:0]
		for _, d := range sel {
			x := cursorCmp(d, c)
			if x < 0 || (x == 0 && !c.GetBefore()) {
				kept = append(kept, d)
			}
		}
		sel = kept
	}

	skipped := int32(0)
	if off := int(q.GetOffset()); off > 0 {
		if off > len(sel) {
			off = len(sel)
		}
		sel, skipped = sel[off:], int32(off)
	}
	limit := maxLimit
	if l := q.GetLimit(); l != nil && int(l.GetValue()) >= 0 && int(l.GetValue()) < limit {
		limit = int(l.GetValue())
	}
	if len(sel) > limit {
		sel = sel[:limit]
	}
	out := make([]*ugcpb.Document, 0, len(sel))
	for _, d := range sel {
		out = append(out, project(d.doc, q.GetSelect()))
	}
	return out, skipped
}

// project copies a document keeping only the selected fields; no projection keeps them all,
// an empty one keeps none (the name alone).
func project(d *ugcpb.Document, p *ugcpb.StructuredQuery_Projection) *ugcpb.Document {
	out := proto.Clone(d).(*ugcpb.Document)
	if p == nil {
		return out
	}
	kept := &commonpb.MapValue{}
	for _, f := range p.GetFields() {
		if v := lookupField(d.GetFields(), f.GetFieldPath()); v != nil {
			setField(kept, f.GetFieldPath(), proto.Clone(v).(*commonpb.Value))
		}
	}
	out.Fields = kept
	return out
}
