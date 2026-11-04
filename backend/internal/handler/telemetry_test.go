package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
	"github.com/tiofani03/artracker/backend/internal/ingest"
)

func TestTelemetryQueryEndpoints(t *testing.T) {
	sink := ingest.NewMemoryStorageSink()
	ingestCfg := ingest.Config{
		Workers:         1,
		QueueSize:       100,
		BatchSize:       10,
		FlushIntervalMs: 50,
		ProjectRPS:      100,
		ProjectEventsPS: 1000,
		KeyCacheTTLSec:  60,
	}
	ingestSvc := ingest.NewService(ingestCfg, nil, sink)

	appCfg := config.Config{Env: "development"}
	issuer := auth.NewIssuer(testSecret, time.Hour)
	h := New(nil, appCfg, issuer, map[string]OAuthProvider{}).WithIngest(ingestSvc)

	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	// Mock middleware to set projectID = 10
	app.Use("/api/v1/projects/:projectID/*", func(c *fiber.Ctx) error {
		c.Locals("projectID", int64(10))
		return c.Next()
	})
	app.Get("/api/v1/projects/:projectID/stats", h.GetProjectStats)
	app.Get("/api/v1/projects/:projectID/events", h.ListProjectEvents)

	// Ingest some items into sink
	now := time.Now()
	_ = sink.WriteBatch(cBackground(), []ingest.TelemetryRow{
		{
			ID:             [16]byte{1},
			ProjectID:      10,
			Kind:           "event",
			Name:           "page_view",
			SessionID:      "sess_1",
			EventTimestamp: now,
		},
		{
			ID:             [16]byte{2},
			ProjectID:      10,
			Kind:           "log",
			Level:          "error",
			Message:        "database connection timeout",
			SessionID:      "sess_1",
			EventTimestamp: now,
		},
		{
			ID:             [16]byte{3},
			ProjectID:      99, // other project
			Kind:           "event",
			Name:           "other_event",
			SessionID:      "sess_2",
			EventTimestamp: now,
		},
	})

	// 1. Test Stats
	req := httptest.NewRequest("GET", "/api/v1/projects/10/stats?range=24h", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("stats status = %d, want 200", resp.StatusCode)
	}
	var stats ProjectStatsResponse
	_ = json.NewDecoder(resp.Body).Decode(&stats)
	if stats.SessionsCount != 1 {
		t.Errorf("expected 1 session, got %d", stats.SessionsCount)
	}
	if stats.EventsCount != 2 {
		t.Errorf("expected 2 events for project 10, got %d", stats.EventsCount)
	}
	if stats.ErrorsCount != 1 {
		t.Errorf("expected 1 error, got %d", stats.ErrorsCount)
	}

	// 2. Test Events List
	req2 := httptest.NewRequest("GET", "/api/v1/projects/10/events?kind=all", nil)
	resp2, err := app.Test(req2, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.StatusCode != 200 {
		t.Fatalf("events status = %d, want 200", resp2.StatusCode)
	}
	var out struct {
		Items []TelemetryEventItem `json:"items"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&out)
	// 3. Test Seed Mock Data
	app.Post("/api/v1/projects/:projectID/seed-mock", h.SeedProjectMockData)
	req3 := httptest.NewRequest("POST", "/api/v1/projects/10/seed-mock", nil)
	resp3, err := app.Test(req3, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp3.StatusCode != 200 {
		t.Fatalf("seed-mock status = %d, want 200", resp3.StatusCode)
	}
	var seedResp struct {
		Status string `json:"status"`
		Seeded int    `json:"seeded"`
	}
	_ = json.NewDecoder(resp3.Body).Decode(&seedResp)
	if seedResp.Seeded != 50 {
		t.Errorf("expected 50 seeded items, got %d", seedResp.Seeded)
	}
}

func cBackground() context.Context {
	return context.Background()
}
