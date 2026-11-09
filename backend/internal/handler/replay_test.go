package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
	"github.com/tiofani03/artracker/backend/internal/ingest"
)

type mockReplayStore struct {
	putFn func(ctx context.Context, objectKey string, sizeBytes int64, expires time.Duration) (string, error)
	getFn func(ctx context.Context, objectKey string, expires time.Duration) (string, error)
	bucket string
}

func (m *mockReplayStore) GeneratePresignedPut(ctx context.Context, objectKey string, sizeBytes int64, expires time.Duration) (string, error) {
	if m.putFn != nil {
		return m.putFn(ctx, objectKey, sizeBytes, expires)
	}
	return "https://mock-r2.cloudflarestorage.com/" + objectKey + "?sig=mockput", nil
}

func (m *mockReplayStore) GeneratePresignedGet(ctx context.Context, objectKey string, expires time.Duration) (string, error) {
	if m.getFn != nil {
		return m.getFn(ctx, objectKey, expires)
	}
	return "https://mock-r2.cloudflarestorage.com/" + objectKey + "?sig=mockget", nil
}

func (m *mockReplayStore) Bucket() string {
	if m.bucket != "" {
		return m.bucket
	}
	return "test-bucket"
}

func (m *mockReplayStore) PutObject(ctx context.Context, objectKey string, data io.Reader, sizeBytes int64) error {
	return nil
}

func (m *mockReplayStore) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, string, error) {
	return io.NopCloser(bytes.NewReader([]byte("mock-gzip-data"))), 14, "application/gzip", nil
}

func setupReplayTestApp(t *testing.T, withStore bool) (*fiber.App, string) {
	t.Helper()

	cfg := ingest.Config{
		Workers:         2,
		QueueSize:       100,
		BatchSize:       10,
		FlushIntervalMs: 50,
		ProjectRPS:      100,
		ProjectEventsPS: 1000,
		KeyCacheTTLSec:  60,
	}
	sink := ingest.NewMemoryStorageSink()
	ingestSvc := ingest.NewService(cfg, nil, sink)

	plainKey := "art_live_test_replay_key_0123456789abcdef"
	memCache := ingestSvc.KeyCache().(*ingest.MemoryKeyCache)
	memCache.SeedKey(plainKey, ingest.KeyInfo{ProjectID: 88, KeyType: auth.KeyPublicClient}, time.Hour)

	appCfg := config.Config{
		Env:                 "development",
		PublicURL:           "https://test.artracker.example.com",
		ReplayMaxChunkBytes: 5 * 1024 * 1024,
		ReplayProxyUpload:   true,
	}

	h := New(nil, appCfg, auth.NewIssuer("test-jwt-secret-min-32-chars-long!", time.Hour), map[string]OAuthProvider{}).
		WithIngest(ingestSvc)

	if withStore {
		h = h.WithReplayStore(&mockReplayStore{})
	}

	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	h.Routes(app.Group("/api/v1"))

	return app, plainKey
}

func TestReplayChunkURLValidation(t *testing.T) {
	appWithStore, validKey := setupReplayTestApp(t, true)
	appWithoutStore, _ := setupReplayTestApp(t, false)

	// 1. Missing API Key -> 401
	{
		req := httptest.NewRequest("POST", "/api/v1/replay/chunk-url", bytes.NewReader([]byte(`{"session_id":"sess_12345678","chunk_seq":0,"size_bytes":1000}`)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := appWithStore.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for missing key, got %d", resp.StatusCode)
		}
	}

	// 2. Storage unconfigured -> 503
	{
		req := httptest.NewRequest("POST", "/api/v1/replay/chunk-url", bytes.NewReader([]byte(`{"session_id":"sess_12345678","chunk_seq":0,"size_bytes":1000}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Artracker-Key", validKey)
		resp, err := appWithoutStore.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusServiceUnavailable {
			t.Fatalf("expected 503 Service Unavailable when store is nil, got %d", resp.StatusCode)
		}
	}

	// 3. Invalid session_id (path traversal or invalid chars) -> 400
	invalidSessions := []string{"../traversal", "short", "has spaces inside", "invalid!@#$"}
	for _, sessID := range invalidSessions {
		payload := fmt.Sprintf(`{"session_id":%q,"chunk_seq":0,"size_bytes":1000}`, sessID)
		req := httptest.NewRequest("POST", "/api/v1/replay/chunk-url", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Artracker-Key", validKey)
		resp, err := appWithStore.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for session_id %q, got %d", sessID, resp.StatusCode)
		}
	}

	// 4. Invalid chunk_seq (< 0) -> 400
	{
		req := httptest.NewRequest("POST", "/api/v1/replay/chunk-url", bytes.NewReader([]byte(`{"session_id":"sess_valid_1234","chunk_seq":-1,"size_bytes":1000}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Artracker-Key", validKey)
		resp, err := appWithStore.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for negative chunk_seq, got %d", resp.StatusCode)
		}
	}

	// 5. Size exceeds limit -> 413
	{
		payload := `{"session_id":"sess_valid_1234","chunk_seq":0,"size_bytes":10000000}`
		req := httptest.NewRequest("POST", "/api/v1/replay/chunk-url", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Artracker-Key", validKey)
		resp, err := appWithStore.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413 Payload Too Large, got %d", resp.StatusCode)
		}
	}

	// 6. Valid request -> 200 OK with presigned upload_url
	{
		payload := `{"session_id":"sess_valid_123456","chunk_seq":0,"size_bytes":45000,"user_id":"usr_test","environment":"staging"}`
		req := httptest.NewRequest("POST", "/api/v1/replay/chunk-url", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Artracker-Key", validKey)
		resp, err := appWithStore.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 OK, got %d: %s", resp.StatusCode, string(body))
		}

		var res ReplayChunkURLResponse
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			t.Fatal(err)
		}
		if res.UploadURL == "" {
			t.Error("expected non-empty upload_url")
		}
		if res.ObjectKey != "projects/88/sessions/sess_valid_123456/chunk_0.gz" {
			t.Errorf("unexpected object_key: %s", res.ObjectKey)
		}
		if res.ExpiresIn <= 0 {
			t.Errorf("unexpected expires_in: %d", res.ExpiresIn)
		}
	}
}

func TestReplayChunkAckValidation(t *testing.T) {
	appWithStore, validKey := setupReplayTestApp(t, true)

	// 1. Missing Key -> 401
	{
		req := httptest.NewRequest("POST", "/api/v1/replay/chunk-ack", bytes.NewReader([]byte(`{"session_id":"sess_valid_123","chunk_seq":0}`)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := appWithStore.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", resp.StatusCode)
		}
	}

	// 2. Invalid session_id -> 400
	{
		req := httptest.NewRequest("POST", "/api/v1/replay/chunk-ack", bytes.NewReader([]byte(`{"session_id":"bad/sess","chunk_seq":0}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Artracker-Key", validKey)
		resp, err := appWithStore.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", resp.StatusCode)
		}
	}

	// 3. Valid ack -> 200 OK
	{
		req := httptest.NewRequest("POST", "/api/v1/replay/chunk-ack", bytes.NewReader([]byte(`{
			"session_id": "sess_valid_123456",
			"chunk_seq": 0,
			"size_bytes": 45000,
			"duration_ms": 15000,
			"event_count": 100,
			"has_error": true,
			"has_rage_click": false
		}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Artracker-Key", validKey)
		resp, err := appWithStore.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 OK, got %d: %s", resp.StatusCode, string(body))
		}

		var res ReplayChunkAckResponse
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			t.Fatal(err)
		}
		if res.Status != "acknowledged" {
			t.Errorf("expected status 'acknowledged', got %s", res.Status)
		}
		if res.SessionID != "sess_valid_123456" {
			t.Errorf("expected session_id 'sess_valid_123456', got %s", res.SessionID)
		}
	}
}

func TestReplayDatabaseIntegration(t *testing.T) {
	e := newTestEnv(t)
	e.h.WithReplayStore(&mockReplayStore{})

	uid, authHdr := e.addUser(t, "replayadmin")
	orgID := e.addOrg(t, "acme-replay")
	e.addMember(t, orgID, uid, "owner")
	pid := e.addProject(t, orgID, "replay-app")

	key := e.createKey(t, pid, "public_client", authHdr)
	keyHdr := map[string]string{
		"X-Artracker-Key": key.Secret,
		"Content-Type":    "application/json",
	}

	// 1. POST /replay/chunk-url
	sessID := "sess_test_12345678"
	urlPayload := fmt.Sprintf(`{"session_id":%q,"chunk_seq":0,"size_bytes":2048,"user_id":"u_99","environment":"staging"}`, sessID)
	resp := e.post(t, "/api/v1/replay/chunk-url", urlPayload, keyHdr)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("chunk-url status = %d", resp.StatusCode)
	}

	// 2. POST /replay/chunk-ack (chunk 0)
	ackPayload := fmt.Sprintf(`{"session_id":%q,"chunk_seq":0,"size_bytes":2048,"duration_ms":5000,"event_count":20,"has_error":true,"has_rage_click":true}`, sessID)
	resp = e.post(t, "/api/v1/replay/chunk-ack", ackPayload, keyHdr)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("chunk-ack status = %d", resp.StatusCode)
	}
	var ackRes ReplayChunkAckResponse
	_ = json.NewDecoder(resp.Body).Decode(&ackRes)
	if ackRes.ChunkCount != 1 {
		t.Errorf("expected chunk_count 1, got %d", ackRes.ChunkCount)
	}

	// 3. RETRY identical chunk-ack -> Idempotent!
	resp = e.post(t, "/api/v1/replay/chunk-ack", ackPayload, keyHdr)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("retry chunk-ack status = %d", resp.StatusCode)
	}
	_ = json.NewDecoder(resp.Body).Decode(&ackRes)
	if ackRes.ChunkCount != 1 {
		t.Errorf("idempotent retry expected chunk_count 1, got %d", ackRes.ChunkCount)
	}

	// 4. POST /replay/chunk-ack (chunk 1, has_error: false, has_dead_click: true -> sticky flags must hold)
	ackPayload2 := fmt.Sprintf(`{"session_id":%q,"chunk_seq":1,"size_bytes":4096,"duration_ms":12000,"event_count":40,"has_error":false,"has_dead_click":true}`, sessID)
	resp = e.post(t, "/api/v1/replay/chunk-ack", ackPayload2, keyHdr)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("chunk-ack 1 status = %d", resp.StatusCode)
	}
	_ = json.NewDecoder(resp.Body).Decode(&ackRes)
	if ackRes.ChunkCount != 2 {
		t.Errorf("expected chunk_count 2, got %d", ackRes.ChunkCount)
	}

	// 5. GET /projects/:projectID/sessions
	listResp := e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d/sessions?has_error=true&has_dead_click=true", pid), nil, authHdr)
	if listResp.StatusCode != fiber.StatusOK {
		t.Fatalf("list sessions status = %d", listResp.StatusCode)
	}
	var listRes struct {
		Items []ReplaySessionItem `json:"items"`
		Total int64               `json:"total"`
	}
	_ = json.NewDecoder(listResp.Body).Decode(&listRes)
	if len(listRes.Items) != 1 {
		t.Fatalf("expected 1 session item, got %d", len(listRes.Items))
	}
	if !listRes.Items[0].HasError || !listRes.Items[0].HasRageClick || !listRes.Items[0].HasDeadClick {
		t.Errorf("expected sticky flags true: got err=%v rage=%v dead=%v", listRes.Items[0].HasError, listRes.Items[0].HasRageClick, listRes.Items[0].HasDeadClick)
	}
	if listRes.Items[0].ChunkCount != 2 {
		t.Errorf("expected chunk_count 2, got %d", listRes.Items[0].ChunkCount)
	}
	if listRes.Items[0].DurationMs != 12000 {
		t.Errorf("expected duration_ms 12000, got %d", listRes.Items[0].DurationMs)
	}

	// 6. GET /projects/:projectID/sessions/:sessionID
	getResp := e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d/sessions/%s", pid, sessID), nil, authHdr)
	if getResp.StatusCode != fiber.StatusOK {
		t.Fatalf("get session status = %d", getResp.StatusCode)
	}
	var detailRes ReplaySessionDetailResponse
	_ = json.NewDecoder(getResp.Body).Decode(&detailRes)
	if detailRes.Session.ID != sessID {
		t.Errorf("expected session id %s, got %s", sessID, detailRes.Session.ID)
	}
	if len(detailRes.Chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(detailRes.Chunks))
	}
	if detailRes.Chunks[0].URL == "" || detailRes.Chunks[1].URL == "" {
		t.Errorf("expected presigned URLs on chunks")
	}
}

func TestReplayProxyUpload(t *testing.T) {
	appWithStore, validKey := setupReplayTestApp(t, true)

	// 1. Request chunk-url with proxy upload (default)
	payload := `{"session_id":"sess_proxy_test_123","chunk_seq":0,"size_bytes":100}`
	req := httptest.NewRequest("POST", "/api/v1/replay/chunk-url", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Artracker-Key", validKey)
	resp, err := appWithStore.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var urlRes ReplayChunkURLResponse
	if err := json.NewDecoder(resp.Body).Decode(&urlRes); err != nil {
		t.Fatal(err)
	}

	// Verify that upload_url starts with PublicURL when configured, or fallback
	expectedBase := "https://test.artracker.example.com"
	if !strings.HasPrefix(urlRes.UploadURL, expectedBase) {
		t.Fatalf("expected upload_url to start with %q, got %q", expectedBase, urlRes.UploadURL)
	}

	// 2. PUT binary chunk to the upload URL (extract relative path)
	parsedURL, err := url.Parse(urlRes.UploadURL)
	if err != nil {
		t.Fatal(err)
	}
	uploadPath := parsedURL.Path + "?" + parsedURL.RawQuery

	putReq := httptest.NewRequest("PUT", uploadPath, bytes.NewReader([]byte("dummy-gzipped-chunk-bytes")))
	putReq.Header.Set("Content-Type", "application/gzip")
	putResp, err := appWithStore.Test(putReq)
	if err != nil {
		t.Fatal(err)
	}
	if putResp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(putResp.Body)
		t.Fatalf("expected 200 OK from upload-chunk proxy, got %d: %s", putResp.StatusCode, string(body))
	}

	// 3. GET binary chunk from the download URL
	downloadPath := fmt.Sprintf("/api/v1/replay/download-chunk?%s", parsedURL.RawQuery)
	getReq := httptest.NewRequest("GET", downloadPath, nil)
	getResp, err := appWithStore.Test(getReq)
	if err != nil {
		t.Fatal(err)
	}
	if getResp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(getResp.Body)
		t.Fatalf("expected 200 OK from download-chunk proxy, got %d: %s", getResp.StatusCode, string(body))
	}
	if getResp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected CORS header on download response, got %s", getResp.Header.Get("Access-Control-Allow-Origin"))
	}
}

