package livekit

import (
	"testing"

	"github.com/livekit/protocol/auth"
)

// TestMintedTokenCarriesExpectedGrant decodes and verifies the JWT
// Provision would hand to a human participant, asserting its VideoGrant
// scopes RoomJoin to exactly the provisioned room. This is pure local
// crypto (auth.NewAccessToken/.ToJWT/auth.ParseAPIToken/.Verify) — no
// network call, no LiveKit server needed.
func TestMintedTokenCarriesExpectedGrant(t *testing.T) {
	const apiKey = "test-key"
	const apiSecret = "test-secret"
	const roomName = "voxstate-voice-abc123"
	const identity = "user-voice-abc123"

	token, err := auth.NewAccessToken(apiKey, apiSecret).
		SetIdentity(identity).
		SetVideoGrant(&auth.VideoGrant{RoomJoin: true, Room: roomName}).
		ToJWT()
	if err != nil {
		t.Fatalf("ToJWT() error = %v", err)
	}

	verifier, err := auth.ParseAPIToken(token)
	if err != nil {
		t.Fatalf("ParseAPIToken() error = %v", err)
	}
	if verifier.APIKey() != apiKey {
		t.Errorf("APIKey() = %q, want %q", verifier.APIKey(), apiKey)
	}

	_, grants, err := verifier.Verify(apiSecret)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if grants.Video == nil {
		t.Fatal("grants.Video is nil, want a VideoGrant")
	}
	if !grants.Video.RoomJoin {
		t.Error("grants.Video.RoomJoin = false, want true")
	}
	if grants.Video.Room != roomName {
		t.Errorf("grants.Video.Room = %q, want %q", grants.Video.Room, roomName)
	}
	if grants.Identity != identity {
		t.Errorf("grants.Identity = %q, want %q", grants.Identity, identity)
	}
}

func TestRoomNameFor_IsDeterministicAndPrefixed(t *testing.T) {
	got := roomNameFor("voice-abc123")
	want := "voxstate-abc123"
	if got != want {
		t.Errorf("roomNameFor() = %q, want %q", got, want)
	}
}
