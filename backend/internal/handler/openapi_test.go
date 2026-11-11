package handler

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecValid(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal(openAPISpecBytes, &parsed); err != nil {
		t.Fatalf("openAPISpecBytes is invalid JSON: %v", err)
	}

	paths, ok := parsed["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths object missing in openapi spec")
	}

	expectedPaths := []string{
		"/ingest",
		"/events",
		"/projects/{projectID}/analytics/catalog",
		"/projects/{projectID}/analytics/funnel",
		"/projects/{projectID}/analytics/retention",
		"/projects/{projectID}/analytics/funnels",
		"/replay/config",
		"/projects/{projectID}/replay-settings",
		"/projects/{projectID}/sessions/{sessionID}/chunks",
		"/projects/{projectID}/sessions/{sessionID}/perf",
		"/orgs/{orgID}/alerts/telegram-bot",
		"/projects/{projectID}/alerts/channels",
	}

	for _, p := range expectedPaths {
		if _, exists := paths[p]; !exists {
			t.Errorf("expected path %s missing in openapi spec", p)
		}
	}
}

func TestOpenAPIDocumentsScreenshotReplay(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal(openAPISpecBytes, &parsed); err != nil {
		t.Fatal(err)
	}
	get := func(v any, keys ...string) any {
		for _, k := range keys {
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[k]
		}
		return v
	}

	if get(parsed, "components", "schemas", "ReplayChunkURLRequest", "properties", "format") == nil {
		t.Error("ReplayChunkURLRequest should document the optional format field")
	}
	if get(parsed, "paths", "/replay/chunk-url", "post", "responses", "403") == nil {
		t.Error("chunk-url should document 403 screenshot_disabled")
	}
	if get(parsed, "paths", "/projects/{projectID}/alerts/channels", "post", "responses", "201") == nil {
		t.Error("POST alerts/channels should be documented")
	}
	for _, m := range []string{"get", "put", "patch", "delete"} {
		if get(parsed, "paths", "/orgs/{orgID}/alerts/telegram-bot", m) == nil {
			t.Errorf("%s /orgs/{orgID}/alerts/telegram-bot should be documented", m)
		}
	}
	if get(parsed, "components", "schemas", "ReplayConfigResponse", "properties", "screenshot_enabled") == nil {
		t.Error("ReplayConfigResponse schema missing")
	}
}
