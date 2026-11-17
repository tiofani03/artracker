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
	"github.com/tiofani03/artracker/backend/internal/storage"
)

type mockReplayStore struct {
	putFn  func(ctx context.Context, objectKey string, sizeBytes int64, expires time.Duration) (string, error)
	getFn  func(ctx context.Context, objectKey string, expires time.Duration) (string, error)
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
	var store storage.ReplayStore
	if withStore {
		store = &mockReplayStore{}
	}
	return setupReplayTestAppWithStore(t, store)
}

func setupReplayTestAppWithStore(t *testing.T, store storage.ReplayStore) (*fiber.App, string) {
	t.Helper()
	app, _, key := setupReplayTestHandler(t, store)
	return app, key
}

// setupReplayTestHandler is setupReplayTestAppWithStore that also returns the Handler (project 88, key seeded).
func setupReplayTestHandler(t *testing.T, store storage.ReplayStore) (*fiber.App, *Handler, string) {
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

	if store != nil {
		h = h.WithReplayStore(store)
	}

	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	h.Routes(app.Group("/api/v1"))

	return app, h, plainKey
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

// A store without presign support (local filesystem) must fall back to the signed proxy even
// when the client asks for a direct upload URL.
func TestReplayChunkURLFallsBackToProxyWithoutPresign(t *testing.T) {
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, validKey := setupReplayTestAppWithStore(t, local)

	payload := `{"session_id":"sess_local_store_1","chunk_seq":0,"size_bytes":5,"direct":true}`
	req := httptest.NewRequest("POST", "/api/v1/replay/chunk-url", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Artracker-Key", validKey)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	var urlRes ReplayChunkURLResponse
	if err := json.NewDecoder(resp.Body).Decode(&urlRes); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(urlRes.UploadURL)
	if err != nil || u.Path != "/api/v1/replay/upload-chunk" {
		t.Fatalf("expected proxy upload URL, got %q", urlRes.UploadURL)
	}

	putReq := httptest.NewRequest("PUT", u.Path+"?"+u.RawQuery, bytes.NewReader([]byte("hello")))
	putResp, err := app.Test(putReq)
	if err != nil || putResp.StatusCode != fiber.StatusOK {
		t.Fatalf("upload failed: %v %v", err, putResp.StatusCode)
	}
	getResp, err := app.Test(httptest.NewRequest("GET", "/api/v1/replay/download-chunk?"+u.RawQuery, nil))
	if err != nil || getResp.StatusCode != fiber.StatusOK {
		t.Fatalf("download failed: %v %v", err, getResp.StatusCode)
	}
	body, _ := io.ReadAll(getResp.Body)
	if string(body) != "hello" {
		t.Fatalf("round trip body = %q", body)
	}
}

type sessionDetailBody struct {
	Session struct {
		ID          string `json:"id"`
		HasReplay   bool   `json:"has_replay"`
		HasError    bool   `json:"has_error"`
		UserID      string `json:"user_id"`
		Environment string `json:"environment"`
		EventCount  int64  `json:"event_count"`
	} `json:"session"`
	Chunks         []map[string]any `json:"chunks"`
	TimelineEvents []struct {
		ID          string    `json:"id"`
		Kind        string    `json:"kind"`
		Name        string    `json:"name"`
		Level       string    `json:"level"`
		Msg         string    `json:"message"`
		Environment string    `json:"environment"`
		TS          time.Time `json:"event_timestamp"`
	} `json:"timeline_events"`
}

func (s *sessionListEnv) detail(t *testing.T, pid int64, id string) (int, sessionDetailBody) {
	t.Helper()
	resp := s.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d/sessions/%s", pid, id), nil, s.hdr)
	var body sessionDetailBody
	if resp.StatusCode == 200 {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, body
}

func (s *sessionListEnv) addCrashAt(t *testing.T, pid int64, session string, ts time.Time) int64 {
	t.Helper()
	issue := s.mcpAddIssue(t, pid, "java.lang.NullPointerException: boom", "fatal_crash", "open", 1)
	var id int64
	if err := s.pool.QueryRow(context.Background(), `
		INSERT INTO issue_events(issue_id, session_id, user_id, message, created_at)
		VALUES ($1, $2, 'budi', 'npe at Foo.bar', $3) RETURNING id`, issue, session, ts).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGetSessionTelemetryOnly(t *testing.T) {
	s := newSessionListEnv(t, "dt")
	now := time.Now()
	s.addTelemetryAt(t, s.pid, "ses_tel_0002", "budi", "screen_view", "", now.Add(-3*time.Minute))
	s.addTelemetryAt(t, s.pid, "ses_tel_0002", "", "tap", "", now.Add(-1*time.Minute))
	crashID := s.addCrashAt(t, s.pid, "ses_tel_0002", now.Add(-2*time.Minute))

	status, body := s.detail(t, s.pid, "ses_tel_0002")
	if status != 200 {
		t.Fatalf("status %d", status)
	}
	if body.Session.ID != "ses_tel_0002" || body.Session.HasReplay || !body.Session.HasError || body.Session.UserID != "budi" {
		t.Fatalf("unexpected session: %+v", body.Session)
	}
	if body.Chunks == nil || len(body.Chunks) != 0 {
		t.Fatalf("chunks should be an empty array, got %v", body.Chunks)
	}
	if len(body.TimelineEvents) != 3 {
		t.Fatalf("want 3 timeline events, got %+v", body.TimelineEvents)
	}
	crash := body.TimelineEvents[1] // ordered by timestamp: tel, crash, tel
	if crash.Kind != "crash" || crash.Level != "fatal" || crash.ID != fmt.Sprintf("ie_%d", crashID) ||
		crash.Name != "fatal_crash" || crash.Msg != "npe at Foo.bar" {
		t.Fatalf("unexpected crash row: %+v", crash)
	}
}

func TestGetSessionOutsideListWindow(t *testing.T) {
	s := newSessionListEnv(t, "dw")
	s.addTelemetryAt(t, s.pid, "ses_old_0001", "budi", "tap", "", time.Now().Add(-30*24*time.Hour))
	if status, _ := s.detail(t, s.pid, "ses_old_0001"); status != 200 {
		t.Fatalf("deep link to an old telemetry-only session should open, got %d", status)
	}
}

func TestGetSessionReplayIncludesCrash(t *testing.T) {
	s := newSessionListEnv(t, "dr")
	s.addSessionForUser(t, s.pid, "ses_rep_0010", "budi", false)
	if _, err := s.pool.Exec(context.Background(), `UPDATE replay_sessions SET chunk_count = 2 WHERE id = 'ses_rep_0010'`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.addTelemetryAt(t, s.pid, "ses_rep_0010", "budi", "tap", "", now.Add(-2*time.Minute))
	s.addTelemetryAt(t, s.pid, "ses_rep_0010", "budi", "replay_chunk", "", now.Add(-90*time.Second))
	s.addCrashAt(t, s.pid, "ses_rep_0010", now.Add(-1*time.Minute))

	status, body := s.detail(t, s.pid, "ses_rep_0010")
	if status != 200 || !body.Session.HasReplay {
		t.Fatalf("status %d session %+v", status, body.Session)
	}
	if len(body.TimelineEvents) != 2 {
		t.Fatalf("want tap + crash (replay_chunk excluded), got %+v", body.TimelineEvents)
	}
	if last := body.TimelineEvents[1]; last.Kind != "crash" || !strings.HasPrefix(last.ID, "ie_") {
		t.Fatalf("crash missing from timeline: %+v", body.TimelineEvents)
	}
}

func TestGetSessionHasReplayFlag(t *testing.T) {
	s := newSessionListEnv(t, "df")
	s.addSessionForUser(t, s.pid, "ses_rep_0020", "budi", false) // chunk_count defaults to 0
	s.addSessionForUser(t, s.pid, "ses_rep_0021", "budi", false)
	if _, err := s.pool.Exec(context.Background(), `UPDATE replay_sessions SET chunk_count = 3 WHERE id = 'ses_rep_0021'`); err != nil {
		t.Fatal(err)
	}
	if _, body := s.detail(t, s.pid, "ses_rep_0020"); body.Session.HasReplay {
		t.Fatal("chunk_count 0 must report has_replay=false")
	}
	if _, body := s.detail(t, s.pid, "ses_rep_0021"); !body.Session.HasReplay {
		t.Fatal("chunk_count > 0 must report has_replay=true")
	}
}

func TestGetSessionUnknown404(t *testing.T) {
	s := newSessionListEnv(t, "du")
	if status, _ := s.detail(t, s.pid, "ses_nope_0001"); status != 404 {
		t.Fatalf("want 404, got %d", status)
	}
}

func TestGetSessionOtherProjectTelemetry404(t *testing.T) {
	s := newSessionListEnv(t, "do")
	s.addTelemetryAt(t, s.other, "ses_oth_0001", "budi", "tap", "", time.Now())
	if status, _ := s.detail(t, s.pid, "ses_oth_0001"); status != 404 {
		t.Fatalf("telemetry of another project must not resolve, got %d", status)
	}
}

// The 200-row cap applies to telemetry only: a crash that comes after more than 200 events must still be listed.
func TestGetSessionTimelineKeepsCrashBeyondTelemetryCap(t *testing.T) {
	s := newSessionListEnv(t, "dcap")
	base := time.Now().Add(-time.Hour)
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO telemetry_events(id, project_id, kind, name, level, session_id, user_id, event_timestamp)
		SELECT gen_random_uuid(), $1, 'event', 'tap', '', 'ses_cap_0001', 'budi', $2::timestamptz + make_interval(secs => g)
		FROM generate_series(1, 250) g`, s.pid, base); err != nil {
		t.Fatal(err)
	}
	crashID := s.addCrashAt(t, s.pid, "ses_cap_0001", base.Add(time.Hour/2))

	status, body := s.detail(t, s.pid, "ses_cap_0001")
	if status != 200 {
		t.Fatalf("status %d", status)
	}
	tel, crashes := 0, 0
	for i, ev := range body.TimelineEvents {
		if ev.Kind == "crash" {
			crashes++
			if ev.ID != fmt.Sprintf("ie_%d", crashID) {
				t.Fatalf("unexpected crash id %s", ev.ID)
			}
		} else {
			tel++
		}
		if i > 0 && body.TimelineEvents[i-1].TS.After(ev.TS) {
			t.Fatalf("timeline not sorted at %d", i)
		}
	}
	if tel != 200 || crashes != 1 {
		t.Fatalf("want 200 telemetry + 1 crash, got %d + %d", tel, crashes)
	}
	if body.Session.EventCount != 251 {
		t.Fatalf("session event_count = %d, want 251", body.Session.EventCount)
	}
	if last := body.TimelineEvents[len(body.TimelineEvents)-1]; last.Kind != "crash" {
		t.Fatalf("crash happens after every telemetry row, so it must be last: %+v", last)
	}
}

// Crash rows use the environment recorded on the issue event.
func TestGetSessionCrashEnvironment(t *testing.T) {
	s := newSessionListEnv(t, "denv")
	s.addCrashAt(t, s.pid, "ses_env_0001", time.Now().Add(-time.Minute))
	if _, err := s.pool.Exec(context.Background(), `UPDATE issue_events SET environment = 'staging' WHERE session_id = 'ses_env_0001'`); err != nil {
		t.Fatal(err)
	}
	status, body := s.detail(t, s.pid, "ses_env_0001")
	if status != 200 || len(body.TimelineEvents) != 1 || body.TimelineEvents[0].Environment != "staging" {
		t.Fatalf("status %d timeline %+v", status, body.TimelineEvents)
	}
	if body.Session.Environment != "staging" {
		t.Fatalf("session environment = %q", body.Session.Environment)
	}
	// Events without a recorded environment fall back to production.
	issue2 := s.mcpAddIssue(t, s.pid, "OOM", "fatal_crash", "open", 1)
	s.mcpAddEvent(t, issue2, mcpTestEvent{session: "ses_env_0002", ageMins: 1})
	if _, body := s.detail(t, s.pid, "ses_env_0002"); body.TimelineEvents[0].Environment != "production" {
		t.Fatalf("want production fallback, got %+v", body.TimelineEvents)
	}
}

// A timeline failure must not hide the replay chunks: the detail still answers, with an empty timeline.
func TestGetSessionTimelineErrorKeepsSessionAndChunks(t *testing.T) {
	s := newSessionListEnv(t, "derr")
	s.addSessionForUser(t, s.pid, "ses_rep_0030", "budi", false)
	if _, err := s.pool.Exec(context.Background(), `UPDATE replay_sessions SET chunk_count = 1 WHERE id = 'ses_rep_0030'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO replay_chunks(project_id, session_id, chunk_seq, object_key, size_bytes, event_count)
		VALUES ($1, 'ses_rep_0030', 0, 'k/0', 1, 1)`, s.pid); err != nil {
		t.Fatal(err)
	}
	// An infinite timestamp cannot be scanned into time.Time, so the timeline query's scan fails.
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO telemetry_events(id, project_id, kind, name, session_id, event_timestamp)
		VALUES (gen_random_uuid(), $1, 'event', 'tap', 'ses_rep_0030', 'infinity')`, s.pid); err != nil {
		t.Fatal(err)
	}
	status, body := s.detail(t, s.pid, "ses_rep_0030")
	if status != 200 || !body.Session.HasReplay || len(body.Chunks) != 1 {
		t.Fatalf("status %d session %+v chunks %v", status, body.Session, body.Chunks)
	}
	if body.TimelineEvents == nil || len(body.TimelineEvents) != 0 {
		t.Fatalf("timeline should be an empty array, got %+v", body.TimelineEvents)
	}
}
