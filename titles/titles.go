// Package titles is the registry of every game the shared NPLN binary can host.
package titles

import (
	host "github.com/openpak/npln"
	bayonetta3 "github.com/openpak/npln/titles/bayonetta-3"
	"github.com/openpak/npln/titles/dinkum"
	endlessocean "github.com/openpak/npln/titles/endless-ocean-luminous"
	hamsterplayground "github.com/openpak/npln/titles/hamster-playground"
	humanfallflat "github.com/openpak/npln/titles/human-fall-flat"
	mhrise "github.com/openpak/npln/titles/monster-hunter-rise"
	arceus "github.com/openpak/npln/titles/pokemon-legends-arceus"
	scarletviolet "github.com/openpak/npln/titles/pokemon-scarlet-violet"
	splatoon3 "github.com/openpak/npln/titles/splatoon-3"
	stardew "github.com/openpak/npln/titles/stardew-valley"
	wonder "github.com/openpak/npln/titles/super-mario-bros-wonder"
	jamboree "github.com/openpak/npln/titles/super-mario-party-jamboree"
	wavecade "github.com/openpak/npln/titles/wavecade"
)

// All is every title, by the NPLN_TITLE value that selects it. Adding a game is one line here.
var All = map[string]host.Title{
	stardew.Title.Name:           stardew.Title,
	splatoon3.Title.Name:         splatoon3.Title,
	wonder.Title.Name:            wonder.Title,
	dinkum.Title.Name:            dinkum.Title,
	humanfallflat.Title.Name:     humanfallflat.Title,
	hamsterplayground.Title.Name: hamsterplayground.Title,
	wavecade.Title.Name:          wavecade.Title,
	jamboree.Title.Name:          jamboree.Title,
	scarletviolet.Title.Name:     scarletviolet.Title,
	arceus.Title.Name:            arceus.Title,
	mhrise.Title.Name:            mhrise.Title,
	bayonetta3.Title.Name:        bayonetta3.Title,
	endlessocean.Title.Name:      endlessocean.Title,
}
