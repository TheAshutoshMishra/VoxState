// Package voice is the M6 realtime voice adapter around the existing,
// already-correct agent.Agent orchestration loop (M5). It owns none of
// the state/task/policy correctness guarantees itself — every instruction
// it produces is handed to agent.Agent.Run unchanged, and every response
// it speaks is derived from that Run's returned agent.Result exactly as
// TextResponse (response.go) dictates. This package's job is gluing
// STT -> agent.Run -> TTS/Transport together and segmenting the
// continuous inbound audio stream into discrete instructions.
//
// Dependency direction: api -> voice -> {livekit, rime, deepgram}, wired
// only in cmd/server/main.go — this package's own code (and its tests)
// never imports any of those three subpackages' third-party SDKs
// (server-sdk-go, deepgram-go-sdk). Each subpackage instead imports
// voice, to implement the interfaces declared here. This is the same
// inversion agent already uses for tools.Tool/agent.Planner: main.go
// constructs the concrete implementations and hands them to this
// package's types as interface values.
//
// M6 does not implement interruption/barge-in handling (detecting the
// user speaking over an in-progress response, cancelling in-flight work,
// replanning) — that is M7's job, per docs/ROADMAP.md. See session.go's
// doc comment for the specific behavior M6 has instead.
package voice

import "context"

// The four interfaces below are this package's only third-party
// boundaries (Deepgram, Rime, LiveKit's media plane, LiveKit's admin
// plane). Each has exactly one real implementation (in the
// livekit/rime/deepgram subpackages) and one test fake — this package
// deliberately does not introduce a separate interface for utterance
// segmentation (VAD), since that has exactly one caller and one
// implementation in M6 scope and is therefore a private algorithm
// (segmenter.go), not a swappable boundary.

// STT converts a chunk of inbound audio — already segmented into one
// utterance by segmenter.go — into transcript text. It has no knowledge
// of machines, agents, or sessions; it is a pure speech-to-text boundary.
type STT interface {
	Transcribe(ctx context.Context, audio []int16, sampleRateHz int) (string, error)
}

// TTS synthesizes speech audio for a line of already-approved response
// text. It has no knowledge of agent.Result or policy — by the time TTS
// is called, the decision to speak this text has already been made
// upstream by TextResponse (response.go). Output is PCM16 mono at
// whatever sampleRateHz the implementation actually produced; a real
// implementation must always report its true rate rather than a
// hardcoded assumption (see voice/rime's doc comment for why this
// matters).
type TTS interface {
	Synthesize(ctx context.Context, text string) (audio []int16, sampleRateHz int, err error)
}

// Transport is the realtime audio pipe a VoiceSession drives: it hands
// the session inbound PCM as it arrives and accepts outbound PCM to
// publish. It knows nothing about STT/TTS/agent — it is pure I/O.
type Transport interface {
	// Send publishes PCM16 mono audio at sampleRateHz.
	Send(ctx context.Context, audio []int16, sampleRateHz int) error
	// Frames returns a channel of inbound PCM16 frames as they arrive.
	// Closed when the transport disconnects.
	Frames() <-chan AudioFrame
	Close() error
}

// AudioFrame is one chunk of inbound PCM16 mono audio from a Transport.
type AudioFrame struct {
	Samples      []int16
	SampleRateHz int
}

// RoomProvisioner is the admin-plane boundary a Manager uses to stand up
// a realtime room for a new session and hand a human participant
// something they can join with. This is distinct from Transport
// (per-session realtime audio I/O once a room exists): provisioning is a
// one-shot admin API call (room create/delete, JWT minting), not a
// continuous audio stream, and it is exercised by Manager.Start/End, not
// by VoiceSession.Run.
type RoomProvisioner interface {
	// Provision ensures a room exists for sessionID and mints an access
	// token a human participant can use to join it. Returns the room
	// name/id, the realtime server URL to connect to, and the token.
	Provision(ctx context.Context, sessionID string) (roomName, serverURL, participantToken string, err error)
	// Teardown releases the room. Called when a session ends.
	Teardown(ctx context.Context, roomName string) error
}
