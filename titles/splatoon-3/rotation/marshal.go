package rotation

import "encoding/json"

// marshal writes a rotation back out. Used by the generator's tests and by anything that wants to
// round-trip a file it has just edited.
func marshal(f *File) ([]byte, error) { return json.MarshalIndent(f, "", "  ") }
