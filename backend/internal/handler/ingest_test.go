package handler

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
	"github.com/tiofani03/artracker/backend/internal/ingest"
)

func gzipCompress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeBatchEnvelope(count int) ingest.IngestEnvelope {
	items := make([]ingest.RawItem, count)
	now := time.Now()
	for i := 0; i < count; i++ {
		items[i] = ingest.RawItem{
			Type:      "event",
			Name:      fmt.Sprintf("test_event_%d", i+1),
			Timestamp: &now,
			SessionID: "sess_test_123",
			UserID:    "user_test_456",
			Attributes: json.RawMessage(`{"index":` + fmt.Sprintf("%d", i+1) + `,"source":"mobile_sdk"}`),
		}
	}
	return ingest.IngestEnvelope{
		SentAt: &now,
		SDK: ingest.SDKMeta{
			Name:    "artracker-android",
			Version: "1.0.0",
		},
		Context: map[string]any{
			"environment": "production",
			"app_version": "2.1.0",
		},
		Items: items,
	}
}

func setupIngestEnv(t *testing.T, customCfg ...ingest.Config) (*fiber.App, *ingest.MemoryStorageSink, string) {
	t.Helper()

	cfg := ingest.Config{
		Workers:         2,
		QueueSize:       500,
		BatchSize:       50,
		FlushIntervalMs: 50,
		ProjectRPS:      100,
		ProjectEventsPS: 5000,
		KeyCacheTTLSec:  60,
	}
	if len(customCfg) > 0 {
		cfg = customCfg[0]
	}

	sink := ingest.NewMemoryStorageSink()
	ingestSvc := ingest.NewService(cfg, nil, sink)

	plainKey := "art_live_test_api_key_0123456789abcdef01234567"
	memCache := ingestSvc.KeyCache().(*ingest.MemoryKeyCache)
	memCache.SeedKey(plainKey, ingest.KeyInfo{ProjectID: 88, KeyType: auth.KeyPublicClient}, time.Hour)

	appCfg := config.Config{Env: "development"}
	h := New(nil, appCfg, auth.NewIssuer(testSecret, time.Hour), map[string]OAuthProvider{}).WithIngest(ingestSvc)

	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	app.Get("/healthz", h.Health)
	h.Routes(app.Group("/api/v1"))

	return app, sink, plainKey
}

func TestIngestBatch50EventsGzipAcceptedUnder25ms(t *testing.T) {
	app, sink, validKey := setupIngestEnv(t)

	// Create batch with 52 items (50+ events)
	envelope := makeBatchEnvelope(52)
	payloadJSON, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzipCompress(t, payloadJSON)

	req := httptest.NewRequest("POST", "/api/v1/ingest", bytes.NewReader(compressed))
	req.Header.Set("X-Artracker-Key", validKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")

	start := time.Now()
	resp, err := app.Test(req, -1)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Acceptance criteria: 202 Accepted
	if resp.StatusCode != fiber.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 202; body = %s", resp.StatusCode, string(body))
	}

	// Acceptance criteria: < 25ms response time
	if elapsed > 25*time.Millisecond {
		t.Errorf("response time %v exceeded 25ms SLA", elapsed)
	}

	var respBody struct {
		Status string `json:"status"`
		Queued int    `json:"queued"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatal(err)
	}
	if respBody.Status != "accepted" || respBody.Queued != 52 {
		t.Fatalf("unexpected response body: %+v", respBody)
	}

	// Verify asynchronous flush to storage sink
	time.Sleep(120 * time.Millisecond)
	if sink.Count() != 52 {
		t.Fatalf("expected 52 rows flushed to sink, got %d", sink.Count())
	}
}

func TestIngestInvalidAPIKeyRejected401(t *testing.T) {
	app, _, validKey := setupIngestEnv(t)

	envelope := makeBatchEnvelope(2)
	payloadJSON, _ := json.Marshal(envelope)

	cases := []struct {
		name string
		key  string
	}{
		{"missing key", ""},
		{"random fake key", "art_live_unrecognized_key_hash_not_found"},
		{"malformed prefix key", "invalid_prefix_123456789012345678901234"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/ingest", bytes.NewReader(payloadJSON))
			req.Header.Set("Content-Type", "application/json")
			if tc.key != "" {
				req.Header.Set("X-Artracker-Key", tc.key)
			}
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusUnauthorized {
				t.Errorf("%s = %d, want 401", tc.name, resp.StatusCode)
			}
		})
	}

	// Confirm valid key still works
	req := httptest.NewRequest("POST", "/api/v1/ingest", bytes.NewReader(payloadJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Artracker-Key", validKey)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != fiber.StatusAccepted {
		t.Errorf("valid key = %d, want 202", resp.StatusCode)
	}
}

func TestIngestPlainJSONAccepted(t *testing.T) {
	app, sink, validKey := setupIngestEnv(t)

	envelope := makeBatchEnvelope(5)
	payloadJSON, _ := json.Marshal(envelope)

	req := httptest.NewRequest("POST", "/api/v1/ingest", bytes.NewReader(payloadJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Artracker-Key", validKey)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	time.Sleep(100 * time.Millisecond)
	if sink.Count() != 5 {
		t.Fatalf("expected 5 flushed items, got %d", sink.Count())
	}
}

func TestIngestInvalidGzip400(t *testing.T) {
	app, _, validKey := setupIngestEnv(t)

	req := httptest.NewRequest("POST", "/api/v1/ingest", bytes.NewReader([]byte("not valid gzip stream")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("X-Artracker-Key", validKey)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestIngestEmptyItems400(t *testing.T) {
	app, _, validKey := setupIngestEnv(t)

	req := httptest.NewRequest("POST", "/api/v1/ingest", bytes.NewReader([]byte(`{"items":[]}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Artracker-Key", validKey)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestIngestExceedsMaxBatchItems400(t *testing.T) {
	app, _, validKey := setupIngestEnv(t)

	// MaxBatchItems is 500; send 501
	envelope := makeBatchEnvelope(501)
	payloadJSON, _ := json.Marshal(envelope)

	req := httptest.NewRequest("POST", "/api/v1/ingest", bytes.NewReader(payloadJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Artracker-Key", validKey)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestIngestRateLimit429(t *testing.T) {
	// Restrict project to 1 RPS (burst 2)
	cfg := ingest.Config{
		Workers:         1,
		QueueSize:       100,
		BatchSize:       10,
		FlushIntervalMs: 100,
		ProjectRPS:      1,
		ProjectEventsPS: 1000,
		KeyCacheTTLSec:  60,
	}
	app, _, validKey := setupIngestEnv(t, cfg)

	envelope := makeBatchEnvelope(1)
	payloadJSON, _ := json.Marshal(envelope)

	// Request 1 and 2 should be allowed by burst
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/api/v1/ingest", bytes.NewReader(payloadJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Artracker-Key", validKey)
		resp, _ := app.Test(req, -1)
		if resp.StatusCode != fiber.StatusAccepted {
			t.Fatalf("request #%d should be accepted, got %d", i+1, resp.StatusCode)
		}
	}

	// Request 3 should be 429
	req := httptest.NewRequest("POST", "/api/v1/ingest", bytes.NewReader(payloadJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Artracker-Key", validKey)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d", resp.StatusCode)
	}
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter == "" {
		t.Error("expected Retry-After header on 429 response")
	}
}

func TestIngestHealthcheckBufferStatus(t *testing.T) {
	app, _, _ := setupIngestEnv(t)

	req := httptest.NewRequest("GET", "/healthz", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	ingestMap, ok := body["ingest"].(map[string]any)
	if !ok || ingestMap == nil {
		t.Fatalf("missing ingest field in health response: %+v", body)
	}
	if ingestMap["status"] != "healthy" {
		t.Errorf("expected ingest.status=healthy, got %v", ingestMap["status"])
	}
	if ingestMap["queue_capacity"] != float64(500) {
		t.Errorf("expected queue_capacity=500, got %v", ingestMap["queue_capacity"])
	}
}
