// genrotation writes a rotation file for the server to serve.
//
//	go run ./cmd/genrotation -o rotation.json
//
// The server writes and renews its own (rotation.Keep); this is for a file made by hand, e.g. a
// longer or differently seeded one. Output is deterministic for a given -at and -seed, so
// regenerating twice gives the same file and a diff shows only the time move.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/openpak/npln/titles/splatoon-3/rotation"
)

func main() {
	out := flag.String("o", "rotation.json", "file to write")
	hours := flag.Int("hours", 72, "how many hours of rotation to generate, from now")
	stages := flag.Int("stages", rotation.DefaultStages, "number of stage ids to rotate through")
	seed := flag.Int64("seed", 1, "seed, so output is reproducible")
	atStr := flag.String("at", "", "anchor time (RFC3339); default now")
	flag.Parse()

	at := time.Now().UTC()
	if *atStr != "" {
		t, err := time.Parse(time.RFC3339, *atStr)
		if err != nil {
			fail("bad -at: %v", err)
		}
		at = t.UTC()
	}
	if *stages < 4 {
		fail("-stages must be at least 4: a versus window needs distinct stages for four ladders")
	}
	if *hours < 3 {
		fail("-hours must be at least 3: a rotation has to stay defined past the present")
	}

	f := rotation.Generate(at, *hours, *stages, *seed)
	if err := f.Validate(at); err != nil {
		// The generator is held to the same rule as the loader: it must not be possible to write
		// a file the server would then refuse.
		fail("generated an invalid rotation (this is a bug in Generate): %v", err)
	}
	if err := rotation.Write(*out, f, at); err != nil {
		fail("%v", err)
	}
	fmt.Printf("wrote %s: %d versus windows to %s\n", *out, len(f.Vs), f.VsEnd().Format(time.RFC3339))
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "genrotation: "+format+"\n", a...)
	os.Exit(1)
}
