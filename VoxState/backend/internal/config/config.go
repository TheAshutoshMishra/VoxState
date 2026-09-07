// Package config loads VoxState backend configuration from environment
// variables, falling back to sensible local defaults so the server is
// runnable without a .env file.
package config

import (
	"net"
	"os"
	"strconv"
)

const (
	defaultAppEnv   = "development"
	defaultHTTPHost = "0.0.0.0"
	defaultHTTPPort = "8080"

	// defaultRimeModelID/defaultRimeSpeaker/defaultRimeSampleRateHz are
	// non-secret M6 defaults so the server remains runnable without a
	// .env file, matching this Config's existing pattern for
	// AppEnv/HTTPHost/HTTPPort. Rime's own docs are inconsistent about
	// samplingRate's implicit default across pages (16000/22050/24000
	// have each been observed as "the default") — this value is always
	// sent explicitly on every request (see internal/voice/rime), so
	// picking one here is just this deployment's chosen operating point,
	// not an assumption about what Rime would otherwise do.
	defaultRimeModelID      = "coda"
	defaultRimeSpeaker      = "astra"
	defaultRimeSampleRateHz = 24000
)

// Config holds server configuration loaded from the environment.
// LiveKit/Rime/Deepgram fields were added in M6, the first milestone that
// actually consumes them — see docs/ARCHITECTURE.md's Voice transport
// section for how they're used.
type Config struct {
	AppEnv   string
	HTTPHost string
	HTTPPort string

	LiveKitURL       string
	LiveKitAPIKey    string
	LiveKitAPISecret string

	RimeAPIKey       string
	RimeModelID      string
	RimeSpeaker      string
	RimeSampleRateHz int

	DeepgramAPIKey string
}

// Load reads configuration from the environment, applying defaults for any
// value that is unset or empty.
func Load() Config {
	return Config{
		AppEnv:   getEnv("APP_ENV", defaultAppEnv),
		HTTPHost: getEnv("HTTP_HOST", defaultHTTPHost),
		HTTPPort: getEnv("HTTP_PORT", defaultHTTPPort),

		LiveKitURL:       getEnv("LIVEKIT_URL", ""),
		LiveKitAPIKey:    getEnv("LIVEKIT_API_KEY", ""),
		LiveKitAPISecret: getEnv("LIVEKIT_API_SECRET", ""),

		RimeAPIKey:       getEnv("RIME_API_KEY", ""),
		RimeModelID:      getEnv("RIME_MODEL_ID", defaultRimeModelID),
		RimeSpeaker:      getEnv("RIME_SPEAKER", defaultRimeSpeaker),
		RimeSampleRateHz: getEnvInt("RIME_SAMPLE_RATE_HZ", defaultRimeSampleRateHz),

		DeepgramAPIKey: getEnv("DEEPGRAM_API_KEY", ""),
	}
}

// Addr returns the host:port string suitable for http.Server.Addr.
func (c Config) Addr() string {
	return net.JoinHostPort(c.HTTPHost, c.HTTPPort)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
