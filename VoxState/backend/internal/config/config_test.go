package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	cfg := Load()

	if cfg.AppEnv != defaultAppEnv {
		t.Errorf("AppEnv = %q, want default %q", cfg.AppEnv, defaultAppEnv)
	}
	if cfg.HTTPHost != defaultHTTPHost {
		t.Errorf("HTTPHost = %q, want default %q", cfg.HTTPHost, defaultHTTPHost)
	}
	if cfg.HTTPPort != defaultHTTPPort {
		t.Errorf("HTTPPort = %q, want default %q", cfg.HTTPPort, defaultHTTPPort)
	}
	if cfg.RimeModelID != defaultRimeModelID {
		t.Errorf("RimeModelID = %q, want default %q", cfg.RimeModelID, defaultRimeModelID)
	}
	if cfg.RimeSpeaker != defaultRimeSpeaker {
		t.Errorf("RimeSpeaker = %q, want default %q", cfg.RimeSpeaker, defaultRimeSpeaker)
	}
	if cfg.RimeSampleRateHz != defaultRimeSampleRateHz {
		t.Errorf("RimeSampleRateHz = %d, want default %d", cfg.RimeSampleRateHz, defaultRimeSampleRateHz)
	}
	if cfg.LiveKitURL != "" || cfg.LiveKitAPIKey != "" || cfg.LiveKitAPISecret != "" || cfg.RimeAPIKey != "" || cfg.DeepgramAPIKey != "" {
		t.Errorf("expected empty secret defaults, got %+v", cfg)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("HTTP_HOST", "127.0.0.1")
	t.Setenv("HTTP_PORT", "9090")

	cfg := Load()

	if cfg.AppEnv != "production" {
		t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, "production")
	}
	if cfg.HTTPHost != "127.0.0.1" {
		t.Errorf("HTTPHost = %q, want %q", cfg.HTTPHost, "127.0.0.1")
	}
	if cfg.HTTPPort != "9090" {
		t.Errorf("HTTPPort = %q, want %q", cfg.HTTPPort, "9090")
	}
}

func TestLoad_VoiceEnvOverride(t *testing.T) {
	t.Setenv("LIVEKIT_URL", "wss://example.livekit.cloud")
	t.Setenv("LIVEKIT_API_KEY", "lk-key")
	t.Setenv("LIVEKIT_API_SECRET", "lk-secret")
	t.Setenv("RIME_API_KEY", "rime-key")
	t.Setenv("RIME_MODEL_ID", "mistv3")
	t.Setenv("RIME_SPEAKER", "luna")
	t.Setenv("RIME_SAMPLE_RATE_HZ", "16000")
	t.Setenv("DEEPGRAM_API_KEY", "dg-key")

	cfg := Load()

	if cfg.LiveKitURL != "wss://example.livekit.cloud" {
		t.Errorf("LiveKitURL = %q, want override", cfg.LiveKitURL)
	}
	if cfg.LiveKitAPIKey != "lk-key" || cfg.LiveKitAPISecret != "lk-secret" {
		t.Errorf("LiveKitAPIKey/Secret = %q/%q, want overrides", cfg.LiveKitAPIKey, cfg.LiveKitAPISecret)
	}
	if cfg.RimeAPIKey != "rime-key" || cfg.RimeModelID != "mistv3" || cfg.RimeSpeaker != "luna" {
		t.Errorf("Rime fields = %q/%q/%q, want overrides", cfg.RimeAPIKey, cfg.RimeModelID, cfg.RimeSpeaker)
	}
	if cfg.RimeSampleRateHz != 16000 {
		t.Errorf("RimeSampleRateHz = %d, want 16000", cfg.RimeSampleRateHz)
	}
	if cfg.DeepgramAPIKey != "dg-key" {
		t.Errorf("DeepgramAPIKey = %q, want override", cfg.DeepgramAPIKey)
	}
}

func TestLoad_RimeSampleRateHz_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("RIME_SAMPLE_RATE_HZ", "not-a-number")

	cfg := Load()

	if cfg.RimeSampleRateHz != defaultRimeSampleRateHz {
		t.Errorf("RimeSampleRateHz = %d, want default %d on invalid input", cfg.RimeSampleRateHz, defaultRimeSampleRateHz)
	}
}

func TestConfig_Addr(t *testing.T) {
	cfg := Config{HTTPHost: "0.0.0.0", HTTPPort: "8080"}
	want := "0.0.0.0:8080"
	if got := cfg.Addr(); got != want {
		t.Errorf("Addr() = %q, want %q", got, want)
	}
}
