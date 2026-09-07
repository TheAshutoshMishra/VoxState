package voice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync"
	"time"

	"voxstate/backend/internal/agent"
	"voxstate/backend/internal/state"
)

// STTFactory constructs a fresh STT for one session — Deepgram's REST
// client is stateless per call, but the factory shape keeps Manager
// symmetric with TTSFactory/TransportFactory and leaves room for a future
// STT implementation that does hold per-session state.
type STTFactory func() STT

// TTSFactory constructs a fresh TTS for one session.
type TTSFactory func() TTS

// TransportFactory joins a realtime room as the bot participant for one
// session and returns the Transport driving its audio I/O.
type TransportFactory func(ctx context.Context, roomName, identity string) (Transport, error)

// SessionInfo is the subset of VoiceSession state safe to return across
// the api package boundary (no internal STT/TTS/Transport references).
type SessionInfo struct {
	ID            string
	MachineID     string
	LiveKitRoomID string
	LiveKitURL    string
	LiveKitToken  string
	UserID        string
	StartedAt     time.Time
	EndedAt       *time.Time
}

type managedSession struct {
	session *VoiceSession
	cancel  context.CancelFunc
	info    SessionInfo
}

// Manager owns the set of active VoiceSessions, mirroring tasks.Store's
// shape (a map behind one mutex) and its ephemeral, in-memory-only
// lifecycle — restarting the server loses active sessions, exactly as
// restarting it loses in-flight tasks today. There is no persistence
// here; that is consistent with the rest of the system, not a new gap
// M6 introduces.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*managedSession

	state        *state.Store
	agent        *agent.Agent
	provisioner  RoomProvisioner
	newSTT       STTFactory
	newTTS       TTSFactory
	newTransport TransportFactory
	logger       *slog.Logger
}

// NewManager constructs a Manager. All arguments are required except
// logger (nil is safe). stateStore is used only to validate a machine
// exists before provisioning a room for it — Manager holds it directly
// (not behind an interface), the same pattern tasks.Store already uses
// for its own read-only dependency on state.Store.
func NewManager(stateStore *state.Store, ag *agent.Agent, provisioner RoomProvisioner, newSTT STTFactory, newTTS TTSFactory, newTransport TransportFactory, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Manager{
		sessions:     make(map[string]*managedSession),
		state:        stateStore,
		agent:        ag,
		provisioner:  provisioner,
		newSTT:       newSTT,
		newTTS:       newTTS,
		newTransport: newTransport,
		logger:       logger,
	}
}

// Start provisions a room for machineID, mints a human-participant
// token, joins the bot as a participant, and starts the session's Run
// loop on a background goroutine. It returns immediately with connection
// details for a human client to join the same room.
func (m *Manager) Start(ctx context.Context, machineID, userID string) (SessionInfo, error) {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return SessionInfo{}, ErrMachineIDRequired
	}
	if _, err := m.state.GetCurrentState(machineID); err != nil {
		// Propagates state.ErrMachineNotFound unchanged — voice does not
		// redefine machine-lookup errors, same as tasks.CreateTask.
		return SessionInfo{}, err
	}

	id := generateSessionID()

	roomName, serverURL, token, err := m.provisioner.Provision(ctx, id)
	if err != nil {
		return SessionInfo{}, err
	}

	transport, err := m.newTransport(ctx, roomName, "voxstate-bot")
	if err != nil {
		return SessionInfo{}, err
	}

	session := NewSession(id, machineID, roomName, userID, m.agent, m.newSTT(), m.newTTS(), transport, m.logger)

	info := SessionInfo{
		ID:            id,
		MachineID:     machineID,
		LiveKitRoomID: roomName,
		LiveKitURL:    serverURL,
		LiveKitToken:  token,
		UserID:        userID,
		StartedAt:     session.StartedAt,
	}

	runCtx, cancel := context.WithCancel(context.Background())

	m.mu.Lock()
	m.sessions[id] = &managedSession{session: session, cancel: cancel, info: info}
	m.mu.Unlock()

	go func() {
		defer transport.Close()
		if err := session.Run(runCtx); err != nil && runCtx.Err() == nil {
			m.logger.Warn("voice session ended with error", "session_id", id, "error", err)
		}
	}()

	return info, nil
}

// End stops session id's Run loop, tears down its room, and marks it
// ended. Ending an already-ended session returns ErrSessionAlreadyEnded —
// a one-way transition, matching tasks.Store.CancelTask's terminal-state
// discipline.
func (m *Manager) End(ctx context.Context, id string) (SessionInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SessionInfo{}, ErrSessionIDRequired
	}

	m.mu.Lock()
	ms, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return SessionInfo{}, ErrSessionNotFound
	}
	if ms.info.EndedAt != nil {
		snapshot := ms.info
		m.mu.Unlock()
		return snapshot, ErrSessionAlreadyEnded
	}
	now := time.Now().UTC()
	ms.info.EndedAt = &now
	ms.session.EndedAt = &now
	snapshot := ms.info
	m.mu.Unlock()

	ms.cancel()
	if err := m.provisioner.Teardown(ctx, ms.info.LiveKitRoomID); err != nil {
		m.logger.Warn("voice session room teardown failed", "session_id", id, "error", err)
	}

	return snapshot, nil
}

// Get returns session id's current status.
func (m *Manager) Get(id string) (SessionInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SessionInfo{}, ErrSessionIDRequired
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ms, ok := m.sessions[id]
	if !ok {
		return SessionInfo{}, ErrSessionNotFound
	}
	return ms.info, nil
}

func generateSessionID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "voice-" + hex.EncodeToString(b)
}
