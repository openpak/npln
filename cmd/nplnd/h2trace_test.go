package main

import "testing"

// Frames split at any byte still count, and GOAWAY/RST codes are read.
func TestH2Parser(t *testing.T) {
	preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	settings := []byte{0, 0, 6, 4, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 100}
	ping := []byte{0, 0, 8, 6, 0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8}
	goaway := []byte{0, 0, 8, 7, 0, 0, 0, 0, 0, 0, 0, 0, 5, 0, 0, 0, 11}
	all := append(append(append(append([]byte{}, preface...), settings...), ping...), goaway...)
	for split := 1; split < len(all); split++ {
		p := h2Parser{skip: 24}
		p.feed(all[:split], "client->", 1)
		p.feed(all[split:], "client->", 1)
		if p.counts["SETTINGS"] != 1 || p.counts["PING"] != 1 || p.counts["GOAWAY"] != 1 || p.skip != 0 || p.want != 0 {
			t.Fatalf("split %d: %v skip=%d want=%d", split, p.counts, p.skip, p.want)
		}
	}
}
