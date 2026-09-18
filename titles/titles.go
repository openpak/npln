// Package titles is the registry of every game the shared NPLN binary can host.
package titles

import (
	host "github.com/openpak/npln"
	"github.com/openpak/npln/titles/dinkum"
	humanfallflat "github.com/openpak/npln/titles/human-fall-flat"
	splatoon3 "github.com/openpak/npln/titles/splatoon-3"
	stardew "github.com/openpak/npln/titles/stardew-valley"
	wonder "github.com/openpak/npln/titles/super-mario-bros-wonder"
)

// All is every title, by the NPLN_TITLE value that selects it. Adding a game is one line here.
var All = map[string]host.Title{
	stardew.Title.Name:       stardew.Title,
	splatoon3.Title.Name:     splatoon3.Title,
	wonder.Title.Name:        wonder.Title,
	dinkum.Title.Name:        dinkum.Title,
	humanfallflat.Title.Name: humanfallflat.Title,
}
