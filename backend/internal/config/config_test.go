package config

import "testing"

func TestLoadPanicsWithoutJWTSecretInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "")
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when JWT_SECRET is empty in production")
		}
	}()
	Load()
}

func TestLoadDevDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("PUBLIC_URL", "")
	cfg := Load()
	if cfg.JWTSecret == "" {
		t.Error("expected a dev default JWT secret")
	}
	if cfg.PublicURL != "http://localhost:5173" {
		t.Errorf("PublicURL = %q", cfg.PublicURL)
	}
}

func TestLoadRejectsWeakJWTSecretInProduction(t *testing.T) {
	for _, secret := range []string{"short", "dummy-jwt-secret-change-me", "dev-only-secret-change-me"} {
		t.Run(secret, func(t *testing.T) {
			t.Setenv("APP_ENV", "production")
			t.Setenv("JWT_SECRET", secret)
			defer func() {
				if recover() == nil {
					t.Fatalf("expected panic for weak secret %q in production", secret)
				}
			}()
			Load()
		})
	}
}

func TestLoadAcceptsStrongJWTSecretInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef-strong")
	if cfg := Load(); cfg.JWTSecret == "" {
		t.Fatal("secret dropped")
	}
}

func TestLoadReplayLocalDir(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("REPLAY_LOCAL_DIR", "./.replay-data")
	if got := Load().ReplayLocalDir; got != "./.replay-data" {
		t.Fatalf("ReplayLocalDir = %q", got)
	}
	t.Setenv("REPLAY_LOCAL_DIR", "")
	if got := Load().ReplayLocalDir; got != "" {
		t.Fatalf("ReplayLocalDir default = %q, want empty", got)
	}
}

func TestLoadReplayRateRPS(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("REPLAY_RATE_RPS", "")
	if got := Load().ReplayRateRPS; got != 100 {
		t.Fatalf("ReplayRateRPS default = %v, want 100", got)
	}
	t.Setenv("REPLAY_RATE_RPS", "250")
	if got := Load().ReplayRateRPS; got != 250 {
		t.Fatalf("ReplayRateRPS = %v, want 250", got)
	}
}

func TestAlertsConfigDefaults(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_API_BASE", "")
	t.Setenv("ALERTS_WORKER", "")
	cfg := Load()
	if cfg.TelegramBotToken != "" {
		t.Errorf("TelegramBotToken = %q, want empty", cfg.TelegramBotToken)
	}
	if cfg.TelegramAPIBase != "https://api.telegram.org" {
		t.Errorf("TelegramAPIBase = %q", cfg.TelegramAPIBase)
	}
	if !cfg.AlertsWorker {
		t.Error("AlertsWorker should default to true")
	}
}

func TestAlertsWorkerParsing(t *testing.T) {
	for v, want := range map[string]bool{"true": true, "1": true, "false": false, "0": false} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("ALERTS_WORKER", v)
			if got := Load().AlertsWorker; got != want {
				t.Errorf("ALERTS_WORKER=%q -> %v, want %v", v, got, want)
			}
		})
	}
}

func TestTelegramConfigFromEnv(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	t.Setenv("TELEGRAM_API_BASE", "http://localhost:9")
	cfg := Load()
	if cfg.TelegramBotToken != "123:abc" || cfg.TelegramAPIBase != "http://localhost:9" {
		t.Errorf("cfg = %q %q", cfg.TelegramBotToken, cfg.TelegramAPIBase)
	}
}
