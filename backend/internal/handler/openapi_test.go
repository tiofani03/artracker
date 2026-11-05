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
	}

	for _, p := range expectedPaths {
		if _, exists := paths[p]; !exists {
			t.Errorf("expected path %s missing in openapi spec", p)
		}
	}
}
