package config

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// alertsEnv isolates every variable the alerts config reads.
func alertsEnv(t *testing.T, appEnv, mode, key, keyFile, keyID string) {
	t.Helper()
	t.Setenv("APP_ENV", appEnv)
	t.Setenv("ALERTS_BOT_MODE", mode)
	t.Setenv("ALERTS_MASTER_KEY", key)
	t.Setenv("ALERTS_MASTER_KEY_FILE", keyFile)
	t.Setenv("ALERTS_MASTER_KEY_ID", keyID)
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_BOT_TOKEN_FILE", "")
	if appEnv == "production" {
		t.Setenv("JWT_SECRET", strings.Repeat("k", 40))
	} else {
		t.Setenv("JWT_SECRET", "")
	}
}

func rawKey(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i + 1)
	}
	return b
}

func b64Key(n int) string { return base64.StdEncoding.EncodeToString(rawKey(n)) }

func writeKeyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "master_key")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAlertsBotModeDefaultsToTenant(t *testing.T) {
	alertsEnv(t, "development", "", "", "", "")
	if got := Load().AlertsBotMode; got != "tenant" {
		t.Errorf("mode = %q, want tenant", got)
	}
}

func TestAlertsBotModeValues(t *testing.T) {
	for in, want := range map[string]string{"shared": "shared", " TENANT ": "tenant", "Shared": "shared"} {
		alertsEnv(t, "development", in, "", "", "")
		if got := Load().AlertsBotMode; got != want {
			t.Errorf("mode(%q) = %q, want %q", in, got, want)
		}
	}
	alertsEnv(t, "development", "bogus", "", "", "")
	msg := loadPanicText(t)
	if !strings.Contains(msg, "ALERTS_BOT_MODE") || !strings.Contains(msg, "tenant") || !strings.Contains(msg, "shared") {
		t.Errorf("panic should name ALERTS_BOT_MODE and the valid values: %q", msg)
	}
}

func TestMasterKeyFromEnv(t *testing.T) {
	alertsEnv(t, "development", "", " "+b64Key(32)+" \n", "", "")
	if got := Load().AlertsMasterKey; !bytes.Equal(got, rawKey(32)) {
		t.Errorf("key = %v, want the 32 decoded bytes", got)
	}
}

func TestMasterKeyFromFile(t *testing.T) {
	alertsEnv(t, "development", "", "", writeKeyFile(t, b64Key(32)+"\n"), "")
	if got := Load().AlertsMasterKey; !bytes.Equal(got, rawKey(32)) {
		t.Errorf("key = %v, want the 32 decoded bytes", got)
	}
}

func TestMasterKeyBothSetPanics(t *testing.T) {
	alertsEnv(t, "development", "", b64Key(32), writeKeyFile(t, b64Key(32)), "")
	msg := loadPanicText(t)
	if !strings.Contains(msg, "ALERTS_MASTER_KEY") {
		t.Errorf("expected a panic naming ALERTS_MASTER_KEY, got %q", msg)
	}
	if strings.Contains(msg, b64Key(32)) {
		t.Errorf("panic leaks the key: %q", msg)
	}
}

func TestMasterKeyMalformedPanics(t *testing.T) {
	cases := map[string]string{
		"bad base64": "!!!not-base64!!!",
		"31 bytes":   b64Key(31),
		"33 bytes":   b64Key(33),
	}
	for name, val := range cases {
		for _, appEnv := range []string{"development", "production"} {
			t.Run(name+"/env/"+appEnv, func(t *testing.T) {
				alertsEnv(t, appEnv, "shared", val, "", "")
				msg := loadPanicText(t)
				if !strings.Contains(msg, "ALERTS_MASTER_KEY") {
					t.Fatalf("expected a panic naming ALERTS_MASTER_KEY, got %q", msg)
				}
				if strings.Contains(msg, val) {
					t.Errorf("panic leaks the key: %q", msg)
				}
			})
			t.Run(name+"/file/"+appEnv, func(t *testing.T) {
				alertsEnv(t, appEnv, "shared", "", writeKeyFile(t, val+"\n"), "")
				msg := loadPanicText(t)
				if !strings.Contains(msg, "ALERTS_MASTER_KEY_FILE") {
					t.Fatalf("expected a panic naming ALERTS_MASTER_KEY_FILE, got %q", msg)
				}
				if strings.Contains(msg, val) {
					t.Errorf("panic leaks the key: %q", msg)
				}
			})
		}
	}
	for name, content := range map[string]string{"empty": "", "whitespace only": " \n"} {
		t.Run("file/"+name, func(t *testing.T) {
			alertsEnv(t, "development", "", "", writeKeyFile(t, content), "")
			if !strings.Contains(loadPanicText(t), "ALERTS_MASTER_KEY_FILE") {
				t.Fatal("expected a panic for an empty key file")
			}
		})
	}
}

func TestMasterKeyFileUnreadablePanics(t *testing.T) {
	alertsEnv(t, "development", "", "", filepath.Join(t.TempDir(), "does-not-exist"), "")
	if !strings.Contains(loadPanicText(t), "ALERTS_MASTER_KEY_FILE") {
		t.Fatal("expected a panic naming ALERTS_MASTER_KEY_FILE for an unreadable file")
	}
}

func TestMasterKeyRequiredInProductionTenantMode(t *testing.T) {
	alertsEnv(t, "production", "tenant", "", "", "")
	if !strings.Contains(loadPanicText(t), "ALERTS_MASTER_KEY") {
		t.Fatal("expected a panic naming ALERTS_MASTER_KEY in production tenant mode")
	}
	alertsEnv(t, "production", "", "", "", "")
	if !strings.Contains(loadPanicText(t), "ALERTS_MASTER_KEY") {
		t.Fatal("default mode is tenant, expected a panic in production")
	}
	alertsEnv(t, "production", "shared", "", "", "")
	if msg := loadPanicText(t); msg != "" {
		t.Fatalf("shared mode must not need a key, got panic %q", msg)
	}
	alertsEnv(t, "production", "tenant", b64Key(32), "", "")
	if msg := loadPanicText(t); msg != "" {
		t.Fatalf("unexpected panic with a key set: %q", msg)
	}
}

func TestMasterKeyOptionalInDevelopment(t *testing.T) {
	alertsEnv(t, "development", "tenant", "", "", "")
	var cfg Config
	if msg := loadPanicText(t); msg != "" {
		t.Fatalf("unexpected panic: %q", msg)
	}
	cfg = Load()
	if cfg.AlertsMasterKey != nil {
		t.Errorf("key = %v, want nil", cfg.AlertsMasterKey)
	}
}

func TestMasterKeyIDValidation(t *testing.T) {
	alertsEnv(t, "development", "", "", "", "")
	if got := Load().AlertsMasterKeyID; got != "v1" {
		t.Errorf("default key id = %q, want v1", got)
	}
	alertsEnv(t, "development", "", "", "", "prod-2.A_b")
	if got := Load().AlertsMasterKeyID; got != "prod-2.A_b" {
		t.Errorf("key id = %q", got)
	}
	for _, bad := range []string{"has space", "semi;colon", "a|b", strings.Repeat("x", 33), "ключ"} {
		alertsEnv(t, "development", "", "", "", bad)
		msg := loadPanicText(t)
		if !strings.Contains(msg, "ALERTS_MASTER_KEY_ID") {
			t.Errorf("key id %q: expected a panic naming ALERTS_MASTER_KEY_ID, got %q", bad, msg)
		} else if strings.Contains(msg, bad) {
			t.Errorf("panic repeats the value %q", bad)
		}
	}
}
