package npln

// lockers — nn.npln.toyohr.v1.Locker and Canola, the indexes of player documents the plaza shows
// (lockers, and the posts on the plaza's board). Both list ugcstore documents by type.
//
// ponytail: nothing is indexed yet, so every selection is empty and registrations and likes are
// acknowledged without being kept. The plaza then shows no other players' lockers or posts, which
// is a correct state for the game; index the registered documents once ugcstore is served here.

import (
	"context"
	"log"

	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
)

type locker struct {
	toyohrpb.UnimplementedLockerServer
}

func (l *locker) SelectDocuments(context.Context, *toyohrpb.SelectDocumentsRequest) (*toyohrpb.SelectDocumentsResponse, error) {
	return &toyohrpb.SelectDocumentsResponse{}, nil
}

func (l *locker) IkasuDocument(context.Context, *toyohrpb.IkasuDocumentRequest) (*toyohrpb.IkasuDocumentResponse, error) {
	return &toyohrpb.IkasuDocumentResponse{}, nil
}

func (l *locker) RegisterDocument(context.Context, *toyohrpb.LockerRegisterDocumentRequest) (*toyohrpb.LockerRegisterDocumentResponse, error) {
	log.Printf("[Locker] RegisterDocument acknowledged (not indexed)")
	return &toyohrpb.LockerRegisterDocumentResponse{}, nil
}

type canola struct {
	toyohrpb.UnimplementedCanolaServer
}

func (c *canola) SelectDocuments(context.Context, *toyohrpb.SelectDocumentsRequest) (*toyohrpb.SelectDocumentsResponse, error) {
	return &toyohrpb.SelectDocumentsResponse{}, nil
}

func (c *canola) IkasuDocument(context.Context, *toyohrpb.IkasuDocumentRequest) (*toyohrpb.IkasuDocumentResponse, error) {
	return &toyohrpb.IkasuDocumentResponse{}, nil
}

func (c *canola) RegisterDocument(context.Context, *toyohrpb.CanolaRegisterDocumentRequest) (*toyohrpb.CanolaRegisterDocumentResponse, error) {
	log.Printf("[Canola] RegisterDocument acknowledged (not indexed)")
	return &toyohrpb.CanolaRegisterDocumentResponse{}, nil
}
