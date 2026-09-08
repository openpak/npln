package npln

import (
	"context"
	"testing"

	commonpb "openpak/stardew-valley/proto/common"
	gspb "openpak/stardew-valley/proto/gamesync/v1"
	mmpb "openpak/stardew-valley/proto/matchmaking/v1"
)

// The host's lobby-data flush is a gamesync write of prp._Pia_SystemData (+ ip); it must land in
// the farm's GameSession so QueryGameSessions returns the updated blob.
func TestWriteMirrorsPrpIntoSession(t *testing.T) {
	mm := newSessionServer()
	mm.sessions["g1"] = &mmpb.GameSession{Name: "tenants/current/gameSessions/g1", Properties: &commonpb.MapValue{Fields: map[string]*commonpb.Value{
		"_Pia_SystemData": {ValueType: &commonpb.Value_BytesValue{BytesValue: []byte("old")}},
	}}}
	g := newGamesync(mm)
	uss := "11111111-2222-3333"
	g.sess[uss] = &gsSession{uss: uss, gsid: "g1"}
	doc := &commonpb.MapValue{Fields: map[string]*commonpb.Value{
		"prp": {ValueType: &commonpb.Value_MapValue{MapValue: &commonpb.MapValue{Fields: map[string]*commonpb.Value{
			"_Pia_SystemData": {ValueType: &commonpb.Value_BytesValue{BytesValue: []byte("new+appdata")}},
		}}}},
		"ip": {ValueType: &commonpb.Value_BooleanValue{BooleanValue: true}},
	}}
	op := &gspb.WriteOperation{OperationType: &gspb.WriteOperation_UpdateDocument{UpdateDocument: &gspb.UpdateDocumentRequest{
		Document: &gspb.Document{Name: "docs/__pgn/All/__stu/" + uss, Fields: doc}}}}
	g.apply(context.Background(), []*gspb.WriteOperation{op})
	s := mm.sessions["g1"]
	if got := string(s.GetProperties().GetFields()["_Pia_SystemData"].GetBytesValue()); got != "new+appdata" {
		t.Fatalf("_Pia_SystemData not mirrored: %q", got)
	}
	if !s.GetIsPublic() {
		t.Fatal("ip not mirrored into IsPublic")
	}
}
