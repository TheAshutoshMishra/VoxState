package livekit

import (
	"context"
	"fmt"

	"github.com/livekit/media-sdk"
	lkmedia "github.com/livekit/server-sdk-go/v2/pkg/media"

	protoLogger "github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"

	"voxstate/backend/internal/voice"
)

// inboundSampleRateHz is the rate the bot requests when decoding the
// human participant's inbound Opus track — 48kHz is LiveKit's own native
// media rate (lkmedia.DefaultOpusSampleRate), so no resampling happens on
// the inbound path; the caller passes this same rate to Deepgram's
// SampleRate option (Deepgram accepts an explicit rate rather than
// requiring one fixed value — see voice/deepgram).
const inboundSampleRateHz = lkmedia.DefaultOpusSampleRate

// Transport implements voice.Transport by joining a LiveKit room as a
// raw participant (see this package's doc comment for why: LiveKit's
// Agents framework has no Go support). Outbound audio is published via
// an lkmedia.PCMLocalTrack, constructed once at Connect time for a fixed
// source sample rate — server-sdk-go/v2 resamples internally to
// LiveKit's required 48kHz, so voice.Session never needs its own
// resampler for this path. Inbound audio from the first subscribed
// remote participant's audio track is decoded by an lkmedia.PCMRemoteTrack
// into PCM16 and forwarded onto Frames().
type Transport struct {
	room       *lksdk.Room
	localTrack *lkmedia.PCMLocalTrack
	frames     chan voice.AudioFrame
}

var _ voice.Transport = (*Transport)(nil)

// NewTransportFactory returns a voice.TransportFactory bound to a LiveKit
// server and a fixed outbound sample rate (the rate every TTS.Synthesize
// call in this deployment will report — see voice/rime, which always
// requests the same configured samplingRate). Each call to the returned
// factory joins roomName as a fresh participant with the given identity.
func NewTransportFactory(serverURL, apiKey, apiSecret string, outboundSampleRateHz int) voice.TransportFactory {
	return func(ctx context.Context, roomName, identity string) (voice.Transport, error) {
		return connect(ctx, serverURL, apiKey, apiSecret, roomName, identity, outboundSampleRateHz)
	}
}

func connect(ctx context.Context, serverURL, apiKey, apiSecret, roomName, identity string, outboundSampleRateHz int) (*Transport, error) {
	t := &Transport{
		frames: make(chan voice.AudioFrame, 32),
	}

	callback := &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed: func(track *webrtc.TrackRemote, _ *lksdk.RemoteTrackPublication, _ *lksdk.RemoteParticipant) {
				if track.Codec().MimeType != webrtc.MimeTypeOpus {
					return
				}
				// NewPCMRemoteTrack owns decode/jitter timing; we only
				// supply the writer it pushes finished PCM16 buffers
				// into. A publish error here (frames channel full) is
				// deliberately swallowed inside remoteWriter — see its
				// doc comment.
				if _, err := lkmedia.NewPCMRemoteTrack(track, &remoteWriter{frames: t.frames, sampleRateHz: inboundSampleRateHz}); err != nil {
					protoLogger.GetLogger().Errorw("voice: failed to decode inbound track", err)
				}
			},
		},
	}

	room, err := lksdk.ConnectToRoom(serverURL, lksdk.ConnectInfo{
		APIKey:              apiKey,
		APISecret:           apiSecret,
		RoomName:            roomName,
		ParticipantIdentity: identity,
	}, callback)
	if err != nil {
		return nil, fmt.Errorf("livekit: join room: %w", err)
	}
	t.room = room

	localTrack, err := lkmedia.NewPCMLocalTrack(outboundSampleRateHz, 1, protoLogger.GetLogger())
	if err != nil {
		room.Disconnect()
		return nil, fmt.Errorf("livekit: create local track: %w", err)
	}
	if _, err := room.LocalParticipant.PublishTrack(localTrack, &lksdk.TrackPublicationOptions{Name: "voxstate-agent"}); err != nil {
		room.Disconnect()
		return nil, fmt.Errorf("livekit: publish local track: %w", err)
	}
	t.localTrack = localTrack

	return t, nil
}

// Send writes audio to the published local track. sampleRateHz must
// match the rate the Transport was constructed for (every TTS call in a
// session uses the same configured Rime sample rate) — a mismatch is a
// caller bug, surfaced as an error rather than silently mis-resampled.
func (t *Transport) Send(_ context.Context, audio []int16, sampleRateHz int) error {
	if sampleRateHz != t.localTrack.SampleRate() {
		return fmt.Errorf("livekit: Send sample rate %d does not match track rate %d", sampleRateHz, t.localTrack.SampleRate())
	}
	return t.localTrack.WriteSample(media.PCM16Sample(audio))
}

func (t *Transport) Frames() <-chan voice.AudioFrame {
	return t.frames
}

func (t *Transport) Close() error {
	if t.localTrack != nil {
		t.localTrack.ClearQueue()
		t.localTrack.Close()
	}
	if t.room != nil {
		t.room.Disconnect()
	}
	return nil
}

// remoteWriter adapts an inbound Opus track's decoded PCM16 samples
// (delivered by lkmedia.PCMRemoteTrack) into this Transport's Frames()
// channel. It implements lkmedia.PCMRemoteTrackWriter.
type remoteWriter struct {
	frames       chan<- voice.AudioFrame
	sampleRateHz int
}

func (w *remoteWriter) WriteSample(sample media.PCM16Sample) error {
	samples := make([]int16, len(sample))
	copy(samples, sample)

	select {
	case w.frames <- voice.AudioFrame{Samples: samples, SampleRateHz: w.sampleRateHz}:
	default:
		// Drop rather than block: an overwhelmed segmenter/session must
		// never stall LiveKit's own decode goroutine.
	}
	return nil
}

func (w *remoteWriter) Close() error { return nil }
