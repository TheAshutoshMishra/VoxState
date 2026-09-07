// Package livekit is voice's only real integration with LiveKit. It
// implements voice.RoomProvisioner (room admin + human-participant token
// minting) and voice.Transport (the bot's own realtime audio I/O).
//
// LiveKit's "Agents" framework — the thing that would normally run
// STT/LLM/TTS orchestration for us inside a room — is Python and
// Node.js/TypeScript only; there is no Go support and none is planned
// (confirmed against LiveKit's own docs and both the livekit/agents and
// livekit/agents-js repos as of the M6 research pass). The only correct
// Go-native integration is therefore what this package does: join the
// room directly as a raw participant via server-sdk-go/v2, using its
// pkg/media helpers (lkmedia.PCMLocalTrack/PCMRemoteTrack) for audio
// encode/decode instead of a framework that doesn't exist for Go.
//
// This package imports voice (to implement its interfaces); voice never
// imports this package — see voice's package doc comment for the
// dependency direction and why (main.go is the only place that wires
// concrete implementations to voice's interface-typed fields).
package livekit

import (
	"context"
	"fmt"
	"strings"

	lksdk "github.com/livekit/server-sdk-go/v2"

	"github.com/livekit/protocol/auth"
	lkproto "github.com/livekit/protocol/livekit"

	"voxstate/backend/internal/voice"
)

// Provisioner implements voice.RoomProvisioner against a real LiveKit
// server's room admin API and JWT-based auth.
type Provisioner struct {
	rooms     *lksdk.RoomServiceClient
	serverURL string
	apiKey    string
	apiSecret string
}

// NewProvisioner constructs a Provisioner. serverURL, apiKey, and
// apiSecret correspond to the LIVEKIT_URL/LIVEKIT_API_KEY/
// LIVEKIT_API_SECRET env vars (see .env.example).
func NewProvisioner(serverURL, apiKey, apiSecret string) *Provisioner {
	return &Provisioner{
		rooms:     lksdk.NewRoomServiceClient(serverURL, apiKey, apiSecret),
		serverURL: serverURL,
		apiKey:    apiKey,
		apiSecret: apiSecret,
	}
}

var _ voice.RoomProvisioner = (*Provisioner)(nil)

// Provision creates a LiveKit room named after sessionID and mints an
// access token a human participant can use to join it (RoomJoin grant
// scoped to that one room).
func (p *Provisioner) Provision(ctx context.Context, sessionID string) (roomName, serverURL, participantToken string, err error) {
	roomName = roomNameFor(sessionID)

	if _, err = p.rooms.CreateRoom(ctx, &lkproto.CreateRoomRequest{Name: roomName}); err != nil {
		return "", "", "", fmt.Errorf("livekit: create room: %w", err)
	}

	token, err := auth.NewAccessToken(p.apiKey, p.apiSecret).
		SetIdentity("user-" + sessionID).
		SetVideoGrant(&auth.VideoGrant{RoomJoin: true, Room: roomName}).
		ToJWT()
	if err != nil {
		return "", "", "", fmt.Errorf("livekit: mint token: %w", err)
	}

	return roomName, p.serverURL, token, nil
}

// Teardown deletes the room. It is not an error for the room to already
// be gone (e.g. it emptied out and LiveKit's own EmptyTimeout already
// reclaimed it) — DeleteRoom against a nonexistent room is a no-op on
// LiveKit's server side.
func (p *Provisioner) Teardown(ctx context.Context, roomName string) error {
	_, err := p.rooms.DeleteRoom(ctx, &lkproto.DeleteRoomRequest{Room: roomName})
	if err != nil {
		return fmt.Errorf("livekit: delete room: %w", err)
	}
	return nil
}

func roomNameFor(sessionID string) string {
	return "voxstate-" + strings.TrimPrefix(sessionID, "voice-")
}
