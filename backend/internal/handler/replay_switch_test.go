package handler

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func switchTestRequest(t *testing.T, app *fiber.App, method, path, key, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("X-Artracker-Key", key)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(b))
}

const screenChunkBody = `{"session_id":"sess_test_12345678","chunk_seq":0,"size_bytes":2048,"format":"artracker-screen-v1"}`
const plainChunkBody = `{"session_id":"sess_test_12345678","chunk_seq":0,"size_bytes":2048}`

func TestReplayConfigDefault(t *testing.T) {
	app, _, key := setupReplayTestHandler(t, &mockReplayStore{})
	status, body := switchTestRequest(t, app, "GET", "/api/v1/replay/config", key, "")
	if status != 200 {
		t.Fatalf("status = %d, body %s", status, body)
	}
	if body != `{"screenshot_enabled":true,"max_fps":10}` {
		t.Fatalf("body = %s", body)
	}
	if status, _ := switchTestRequest(t, app, "GET", "/api/v1/replay/config", "", ""); status != 401 {
		t.Fatalf("no key status = %d, want 401", status)
	}
}

func TestReplayConfigReflectsSwitch(t *testing.T) {
	app, h, key := setupReplayTestHandler(t, &mockReplayStore{})
	h.replaySwitch.put(88, false)
	_, body := switchTestRequest(t, app, "GET", "/api/v1/replay/config", key, "")
	if body != `{"screenshot_enabled":false,"max_fps":10}` {
		t.Fatalf("body = %s", body)
	}
}

func TestChunkURLScreenFormatBlockedWhenSwitchOff(t *testing.T) {
	app, h, key := setupReplayTestHandler(t, &mockReplayStore{})
	h.replaySwitch.put(88, false)
	status, body := switchTestRequest(t, app, "POST", "/api/v1/replay/chunk-url", key, screenChunkBody)
	if status != 403 {
		t.Fatalf("status = %d, want 403", status)
	}
	if body != `{"error":"screenshot_disabled"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestChunkURLWithoutFormatUnaffected(t *testing.T) {
	app, h, key := setupReplayTestHandler(t, &mockReplayStore{})
	h.replaySwitch.put(88, false)
	if status, body := switchTestRequest(t, app, "POST", "/api/v1/replay/chunk-url", key, plainChunkBody); status != 200 {
		t.Fatalf("status = %d, body %s", status, body)
	}
}

func TestChunkURLScreenFormatAllowedWhenSwitchOn(t *testing.T) {
	app, h, key := setupReplayTestHandler(t, &mockReplayStore{})
	h.replaySwitch.put(88, true)
	if status, body := switchTestRequest(t, app, "POST", "/api/v1/replay/chunk-url", key, screenChunkBody); status != 200 {
		t.Fatalf("status = %d, body %s", status, body)
	}
}
