package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testToken = "123456:ABC-secret-token-value"

func writeTokenFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "telegram_token")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// loadPanicText runs Load and returns the panic message ("" when it did not panic).
func loadPanicText(t *testing.T) (msg string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	Load()
	return ""
}

func TestTelegramTokenFromEnv(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", testToken)
	t.Setenv("TELEGRAM_BOT_TOKEN_FILE", "")
	if got := Load().TelegramBotToken; got != testToken {
		t.Errorf("token = %q, want the env value", got)
	}
}

func TestTelegramTokenUnsetIsEmpty(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_BOT_TOKEN_FILE", "")
	if got := Load().TelegramBotToken; got != "" {
		t.Errorf("token = %q, want empty (Telegram off)", got)
	}
}

func TestTelegramTokenFromFileIsTrimmed(t *testing.T) {
	for name, content := range map[string]string{
		"trailing newline": testToken + "\n",
		"crlf":             testToken + "\r\n",
		"surrounding ws":   "  " + testToken + " \n\n",
		"no newline":       testToken,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TELEGRAM_BOT_TOKEN", "")
			t.Setenv("TELEGRAM_BOT_TOKEN_FILE", writeTokenFile(t, content))
			if got := Load().TelegramBotToken; got != testToken {
				t.Errorf("token = %q, want %q", got, testToken)
			}
		})
	}
}

func TestTelegramTokenBothSetPanicsWithoutLeaking(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", testToken)
	t.Setenv("TELEGRAM_BOT_TOKEN_FILE", writeTokenFile(t, "other-"+testToken))
	msg := loadPanicText(t)
	if msg == "" {
		t.Fatal("expected a panic when both TELEGRAM_BOT_TOKEN and TELEGRAM_BOT_TOKEN_FILE are set")
	}
	if strings.Contains(msg, testToken) {
		t.Errorf("panic message leaks the token: %q", msg)
	}
	if !strings.Contains(msg, "TELEGRAM_BOT_TOKEN_FILE") {
		t.Errorf("panic message should name the variable: %q", msg)
	}
}

func TestTelegramTokenFileMissingPanics(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_BOT_TOKEN_FILE", filepath.Join(t.TempDir(), "does-not-exist"))
	msg := loadPanicText(t)
	if msg == "" {
		t.Fatal("expected a panic when the token file cannot be read (must not silently run with Telegram off)")
	}
	if !strings.Contains(msg, "TELEGRAM_BOT_TOKEN_FILE") {
		t.Errorf("panic message should name the variable: %q", msg)
	}
}

func TestTelegramTokenFileEmptyPanics(t *testing.T) {
	for name, content := range map[string]string{"empty": "", "whitespace only": " \n\r\n"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TELEGRAM_BOT_TOKEN", "")
			t.Setenv("TELEGRAM_BOT_TOKEN_FILE", writeTokenFile(t, content))
			if loadPanicText(t) == "" {
				t.Fatal("expected a panic for an empty token file")
			}
		})
	}
}
