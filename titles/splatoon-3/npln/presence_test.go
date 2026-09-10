package npln

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"google.golang.org/protobuf/proto"

	commonpb "github.com/openpak/npln/proto/common"
	friendspb "github.com/openpak/npln/proto/friends/v1"
)

// The heartbeat is the contract the client times its ping against, and it is the one message we
// can check against measured bytes: a fresh account's stream opens with a heartbeat carrying
// interval 30s and deadline 50s, then an empty enumeration_done, and nothing in between.
func TestPresenceOpeningWire(t *testing.T) {
	hb := &friendspb.SubscribePresencesResponse{
		Response: &friendspb.SubscribePresencesResponse_Heartbeat{Heartbeat: presenceHeartbeat()},
	}
	b, err := proto.Marshal(hb)
	if err != nil {
		t.Fatal(err)
	}
	// field 3 (heartbeat), 8 bytes: interval Duration{30s}, deadline Duration{50s}.
	if got := hex.EncodeToString(b); got != "1a080a02081e12020832" {
		t.Fatalf("opening heartbeat is %s, want 1a080a02081e12020832", got)
	}

	done := &friendspb.SubscribePresencesResponse{
		Response: &friendspb.SubscribePresencesResponse_EnumerationDone{
			EnumerationDone: &friendspb.SubscribePresencesResponse_PresenceEnumerationDone{},
		},
	}
	b, _ = proto.Marshal(done)
	if got := hex.EncodeToString(b); got != "1200" {
		t.Fatalf("enumeration_done is %s, want 1200", got)
	}
}

// A player is online only while holding a stream, partial attribute updates merge rather than
// replace, and leaving clears them.
func TestPresenceRegistry(t *testing.T) {
	const uid = "u-testtesttesttesttest"
	goOffline(uid)

	if st, _ := stateOf(uid); st != friendspb.State_OFFLINE {
		t.Fatalf("unknown player should be OFFLINE, got %v", st)
	}
	goOnline(uid)
	if st, _ := stateOf(uid); st != friendspb.State_ONLINE {
		t.Fatalf("player holding a stream should be ONLINE, got %v", st)
	}

	str := func(s string) *commonpb.Value {
		return &commonpb.Value{ValueType: &commonpb.Value_StringValue{StringValue: s}}
	}
	publish(uid, map[string]*commonpb.Value{"PlayerName": str("A"), "SessionId": str("")})
	publish(uid, map[string]*commonpb.Value{"SessionId": str("s-1")}) // partial update
	_, attrs := stateOf(uid)
	if len(attrs) != 2 || attrs["PlayerName"].GetStringValue() != "A" || attrs["SessionId"].GetStringValue() != "s-1" {
		t.Fatalf("partial update did not merge: %v", attrs)
	}

	// The returned map must be a copy; a caller mutating it must not corrupt the registry.
	attrs["PlayerName"] = str("mutated")
	if _, again := stateOf(uid); again["PlayerName"].GetStringValue() != "A" {
		t.Fatal("stateOf handed out the live map")
	}

	goOffline(uid)
	if st, a := stateOf(uid); st != friendspb.State_OFFLINE || a != nil {
		t.Fatalf("leaving should clear the player, got %v %v", st, a)
	}
}

// The resume token describes the whole enumerated set. Deriving it from a delta instead has been
// measured emptying a large friend list with a communication error, so this is the check that
// stops that regression coming back.
func TestResumeTokenDescribesTheSet(t *testing.T) {
	p := func(uid string) *friendspb.Presence {
		return &friendspb.Presence{Name: Tenant + "/users/" + uid + "/presence"}
	}
	all := []*friendspb.Presence{p("u-aaa"), p("u-bbb"), p("u-ccc")}
	delta := []*friendspb.Presence{p("u-bbb")}

	full, part := resumeToken(all), resumeToken(delta)
	if full == part {
		t.Fatal("a delta and the full set produced the same token")
	}
	if resumeToken(all) != full {
		t.Fatal("token is not stable for the same set")
	}
	raw, err := base64.StdEncoding.DecodeString(full)
	if err != nil {
		t.Fatalf("token is not base64: %v", err)
	}
	// Every enumerated uid has to appear in the token; that is what "describes the set" means.
	for _, want := range []string{"u-aaa", "u-bbb", "u-ccc"} {
		if !contains(raw, want) {
			t.Fatalf("%s missing from the token", want)
		}
	}
}

func contains(hay []byte, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if string(hay[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}

// The whole friend graph in one message is a measured crash, so the stream is split by size.
func TestChunkAccountsStaysUnderBudget(t *testing.T) {
	var accounts []*friendspb.SubscribeFriendUsersResponse_FriendAccount
	for i := 0; i < 150; i++ {
		accounts = append(accounts, &friendspb.SubscribeFriendUsersResponse_FriendAccount{
			NsaId: "00112233445566" + string(rune('a'+i%26)),
			Users: []*friendspb.FriendUser{{
				Name:       Tenant + "/users/u-me/friendUsers/u-friend000000000000",
				FriendUser: Tenant + "/users/u-friend000000000000",
			}},
		})
	}
	batches := chunkAccounts(accounts, 4096)
	if len(batches) < 2 {
		t.Fatalf("150 friends should not go out in %d message(s)", len(batches))
	}
	seen := 0
	for _, b := range batches {
		seen += len(b)
		msg := &friendspb.SubscribeFriendUsersResponse{FriendAccounts: b}
		// One oversized account may exceed the budget alone; more than one must not.
		if n := proto.Size(msg); n > 4096 && len(b) > 1 {
			t.Fatalf("batch of %d is %d bytes, over budget", len(b), n)
		}
	}
	if seen != len(accounts) {
		t.Fatalf("chunking lost friends: %d of %d", seen, len(accounts))
	}
	if chunkAccounts(nil, 4096) != nil {
		t.Fatal("no friends should produce no batches")
	}
}
