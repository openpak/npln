package rotation

import (
	"bytes"
	"io"
)

// newTrimReader lets a rotation file carry "//" line comments, so the generated file can explain
// itself and a hand edit can leave a note. JSON has no comments; stripping them here is cheaper
// than a second file format.
//
// ponytail: strips "//" only at the start of a line, so it cannot cut one out of a string value.
// Nothing in this format needs a trailing comment.
func newTrimReader(b []byte) io.Reader {
	var out bytes.Buffer
	for _, line := range bytes.Split(b, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimLeft(line, " \t"), []byte("//")) {
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return &out
}
