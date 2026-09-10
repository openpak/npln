// Package titles is the registry of every game the shared NPLN binary can host.
package titles

import (
	host "github.com/openpak/npln"
	splatoon3 "github.com/openpak/npln/titles/splatoon-3"
	stardew "github.com/openpak/npln/titles/stardew-valley"
)

// All is every title, by the NPLN_TITLE value that selects it. Adding a game is one line here.
var All = map[string]host.Title{
	stardew.Title.Name:   stardew.Title,
	splatoon3.Title.Name: splatoon3.Title,
}
