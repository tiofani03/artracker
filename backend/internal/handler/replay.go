package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tiofani03/artracker/backend/internal/storage"
)

var sessionIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)

type ReplayChunkURLRequest struct {
	SessionID   string     `json:"session_id"`
	ChunkSeq    int        `json:"chunk_seq"`
	SizeBytes   int64      `json:"size_bytes"`
	UserID      string     `json:"user_id,omitempty"`
	Environment string     `json:"environment,omitempty"`
	StartTime   *time.Time `json:"start_time,omitempty"`
	Direct      *bool      `json:"direct,omitempty"`
}

type ReplayChunkURLResponse struct {
	UploadURL string `json:"upload_url"`
	ObjectKey string `json:"object_key"`
	ExpiresIn int    `json:"expires_in"` // seconds
}

type ReplayChunkAckRequest struct {
	SessionID    string `json:"session_id"`
	ChunkSeq     int    `json:"chunk_seq"`
	SizeBytes    int64  `json:"size_bytes"`
	DurationMs   int64  `json:"duration_ms"`
	EventCount   int    `json:"event_count"`
	HasError     bool   `json:"has_error"`
	HasRageClick bool   `json:"has_rage_click"`
	HasDeadClick bool   `json:"has_dead_click"`
}

type ReplayChunkAckResponse struct {
	Status     string `json:"status"`
	SessionID  string `json:"session_id"`
	ChunkSeq   int    `json:"chunk_seq"`
	ChunkCount int    `json:"chunk_count"`
}

type ReplaySessionItem struct {
	ID             string     `json:"id"`
	ProjectID      int64      `json:"project_id"`
	UserID         string     `json:"user_id"`
	Environment    string     `json:"environment"`
	StartTime      time.Time  `json:"start_time"`
	EndTime        *time.Time `json:"end_time"`
	DurationMs     int64      `json:"duration_ms"`
	ChunkCount     int        `json:"chunk_count"`
	EventCount     int64      `json:"event_count"`
	TotalSizeBytes int64      `json:"total_size_bytes"`
	HasError       bool       `json:"has_error"`
	HasRageClick   bool       `json:"has_rage_click"`
	HasDeadClick   bool       `json:"has_dead_click"`
	StoragePrefix  string     `json:"storage_prefix"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type ReplayChunkDetail struct {
	ChunkSeq   int       `json:"chunk_seq"`
	ObjectKey  string    `json:"object_key"`
	SizeBytes  int64     `json:"size_bytes"`
	EventCount int       `json:"event_count"`
	CreatedAt  time.Time `json:"created_at"`
	URL        string    `json:"url,omitempty"`
}

type ReplaySessionDetailResponse struct {
	Session        ReplaySessionItem    `json:"session"`
	Chunks         []ReplayChunkDetail  `json:"chunks"`
	TimelineEvents []TelemetryEventItem `json:"timeline_events"`
}

// GenerateChunkUploadURL handles POST /api/v1/replay/chunk-url.
func (h *Handler) GenerateChunkUploadURL(c *fiber.Ctx) error {
	projectID, ok := c.Locals("projectID").(int64)
	if !ok || projectID <= 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}

	if h.replayStore == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "session replay storage is not configured")
	}

	var req ReplayChunkURLRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}

	req.SessionID = strings.TrimSpace(req.SessionID)
	if !sessionIDRegex.MatchString(req.SessionID) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid session_id format (must be 8-128 chars: alphanumeric, _, -)")
	}

	if req.ChunkSeq < 0 {
		return fiber.NewError(fiber.StatusBadRequest, "chunk_seq must be >= 0")
	}

	maxChunk := h.cfg.ReplayMaxChunkBytes
	if maxChunk <= 0 {
		maxChunk = 5 * 1024 * 1024
	}

	if req.SizeBytes <= 0 {
		return fiber.NewError(fiber.StatusBadRequest, "size_bytes must be greater than 0")
	}
	if req.SizeBytes > maxChunk {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, fmt.Sprintf("chunk size exceeds maximum limit of %d bytes", maxChunk))
	}

	envName := strings.TrimSpace(req.Environment)
	if envName == "" {
		envName = "production"
	}

	storagePrefix := fmt.Sprintf("projects/%d/sessions/%s/", projectID, req.SessionID)
	objectKey := fmt.Sprintf("%schunk_%d.gz", storagePrefix, req.ChunkSeq)

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db != nil {
		startTime := time.Now()
		if req.StartTime != nil && !req.StartTime.IsZero() {
			startTime = *req.StartTime
		}

		_, err := h.db.Exec(ctx, `
			INSERT INTO replay_sessions (
				id, project_id, user_id, environment, start_time, storage_prefix, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, now()
			)
			ON CONFLICT (project_id, id) DO UPDATE SET
				user_id = CASE WHEN replay_sessions.user_id = '' AND $3 <> '' THEN $3 ELSE replay_sessions.user_id END,
				environment = CASE WHEN replay_sessions.environment = 'production' AND $4 <> '' THEN $4 ELSE replay_sessions.environment END,
				updated_at = now()`,
			req.SessionID, projectID, strings.TrimSpace(req.UserID), envName, startTime, storagePrefix)
		if err != nil {
			return fmt.Errorf("upsert replay session: %w", err)
		}
	}

	expires := 5 * time.Minute
	var uploadURL string

	useProxy := h.cfg.ReplayProxyUpload
	if req.Direct != nil {
		useProxy = !*req.Direct
	}

	if !useProxy {
		presignedURL, err := h.replayStore.GeneratePresignedPut(ctx, objectKey, req.SizeBytes, expires)
		switch {
		case errors.Is(err, storage.ErrPresignUnsupported):
			useProxy = true // e.g. local filesystem store: only the signed proxy can reach it
		case err != nil:
			return fmt.Errorf("generate presigned put: %w", err)
		default:
			uploadURL = presignedURL
		}
	}
	if useProxy {
		uploadURL = h.signedChunkURL(c, "upload-chunk", objectKey, expires)
	}

	return c.JSON(ReplayChunkURLResponse{
		UploadURL: uploadURL,
		ObjectKey: objectKey,
		ExpiresIn: int(expires.Seconds()),
	})
}

// UploadChunkProxy handles PUT /api/v1/replay/upload-chunk.
// It proxies the binary upload directly to Cloudflare R2 / S3, avoiding browser CORS preflight restrictions.
func (h *Handler) UploadChunkProxy(c *fiber.Ctx) error {
	if h.replayStore == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "session replay storage is not configured")
	}

	objectKey := c.Query("key")
	if objectKey == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing key parameter")
	}

	expStr := c.Query("exp")
	sig := c.Query("sig")
	if expStr == "" || sig == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "missing exp or sig authorization parameters")
	}

	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid exp parameter")
	}

	if !verifyChunkUploadSignature(h.replaySecret(), objectKey, exp, sig) {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid or expired upload signature")
	}

	body := c.Body()
	if len(body) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "empty chunk body")
	}

	maxChunk := h.cfg.ReplayMaxChunkBytes
	if maxChunk <= 0 {
		maxChunk = 5 * 1024 * 1024
	}
	if int64(len(body)) > maxChunk {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, fmt.Sprintf("chunk size exceeds maximum limit of %d bytes", maxChunk))
	}

	ctx, cancel := context.WithTimeout(c.Context(), 15*time.Second)
	defer cancel()

	if err := h.replayStore.PutObject(ctx, objectKey, bytes.NewReader(body), int64(len(body))); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, fmt.Sprintf("upload chunk to storage: %v", err))
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status": "ok",
		"key":    objectKey,
		"size":   len(body),
	})
}

// DownloadChunkProxy handles GET /api/v1/replay/download-chunk.
// It proxies the binary download directly from Cloudflare R2 / S3 with full CORS support.
func (h *Handler) DownloadChunkProxy(c *fiber.Ctx) error {
	if h.replayStore == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "session replay storage is not configured")
	}

	objectKey := c.Query("key")
	if objectKey == "" {
		return fiber.NewError(fiber.StatusBadRequest, "missing key parameter")
	}

	expStr := c.Query("exp")
	sig := c.Query("sig")
	if expStr == "" || sig == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "missing exp or sig authorization parameters")
	}

	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid exp parameter")
	}

	if !verifyChunkUploadSignature(h.replaySecret(), objectKey, exp, sig) {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid or expired download signature")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 15*time.Second)
	defer cancel()

	body, _, contentType, err := h.replayStore.GetObject(ctx, objectKey)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, fmt.Sprintf("chunk object not found: %v", err))
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, fmt.Sprintf("read chunk payload: %v", err))
	}

	c.Set("Content-Type", contentType)
	c.Set("Content-Length", strconv.Itoa(len(data)))
	origin := c.Get("Origin")
	if origin != "" {
		c.Set("Access-Control-Allow-Origin", origin)
		c.Set("Access-Control-Allow-Credentials", "true")
	} else {
		c.Set("Access-Control-Allow-Origin", "*")
	}

	return c.Send(data)
}

// signedChunkURL builds an HMAC-signed URL for the upload-chunk / download-chunk proxy routes.
func (h *Handler) signedChunkURL(c *fiber.Ctx, route, objectKey string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	sig := computeChunkUploadSignature(h.replaySecret(), objectKey, exp)
	return fmt.Sprintf("%s/api/v1/replay/%s?key=%s&exp=%d&sig=%s",
		h.getBaseURL(c), route, url.QueryEscape(objectKey), exp, sig)
}

func (h *Handler) replaySecret() string {
	if h.cfg.JWTSecret != "" {
		return h.cfg.JWTSecret
	}
	if h.cfg.R2AccessKeySecret != "" {
		return h.cfg.R2AccessKeySecret
	}
	return "artracker-replay-fallback-secret-2026"
}

func computeChunkUploadSignature(secret, objectKey string, expiresAt int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%s:%d", objectKey, expiresAt)))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyChunkUploadSignature(secret, objectKey string, expiresAt int64, signature string) bool {
	if time.Now().Unix() > expiresAt {
		return false
	}
	expected := computeChunkUploadSignature(secret, objectKey, expiresAt)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (h *Handler) getBaseURL(c *fiber.Ctx) string {
	if h.cfg.PublicURL != "" {
		return strings.TrimRight(h.cfg.PublicURL, "/")
	}
	host := c.Get("X-Forwarded-Host")
	if host == "" {
		host = c.Get("Host")
	}
	if host == "" {
		host = c.Hostname()
	}
	proto := c.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = c.Protocol()
	}
	return fmt.Sprintf("%s://%s", proto, host)
}

// AcknowledgeChunk handles POST /api/v1/replay/chunk-ack.
func (h *Handler) AcknowledgeChunk(c *fiber.Ctx) error {
	projectID, ok := c.Locals("projectID").(int64)
	if !ok || projectID <= 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}

	var req ReplayChunkAckRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}

	req.SessionID = strings.TrimSpace(req.SessionID)
	if !sessionIDRegex.MatchString(req.SessionID) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid session_id format")
	}

	if req.ChunkSeq < 0 {
		return fiber.NewError(fiber.StatusBadRequest, "chunk_seq must be >= 0")
	}
	if req.SizeBytes < 0 {
		return fiber.NewError(fiber.StatusBadRequest, "size_bytes must be >= 0")
	}
	if req.DurationMs < 0 {
		return fiber.NewError(fiber.StatusBadRequest, "duration_ms must be >= 0")
	}
	if req.EventCount < 0 {
		return fiber.NewError(fiber.StatusBadRequest, "event_count must be >= 0")
	}

	storagePrefix := fmt.Sprintf("projects/%d/sessions/%s/", projectID, req.SessionID)
	objectKey := fmt.Sprintf("%schunk_%d.gz", storagePrefix, req.ChunkSeq)

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	chunkCount := 1
	if h.db != nil {
		// Ensure session record exists
		if _, err := h.db.Exec(ctx, `
			INSERT INTO replay_sessions (id, project_id, storage_prefix, updated_at)
			VALUES ($1, $2, $3, now())
			ON CONFLICT (project_id, id) DO NOTHING`,
			req.SessionID, projectID, storagePrefix); err != nil {
			return fmt.Errorf("ensure replay session: %w", err)
		}

		// Atomic CTE: idempotent chunk insert + session aggregation update
		var currentCount int
		err := h.db.QueryRow(ctx, `
			WITH ins AS (
				INSERT INTO replay_chunks (project_id, session_id, chunk_seq, object_key, size_bytes, event_count)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (project_id, session_id, chunk_seq) DO NOTHING
				RETURNING 1
			)
			UPDATE replay_sessions
			SET chunk_count      = replay_sessions.chunk_count + (SELECT COUNT(*) FROM ins),
			    event_count      = replay_sessions.event_count + CASE WHEN EXISTS (SELECT 1 FROM ins) THEN $6 ELSE 0 END,
			    total_size_bytes = replay_sessions.total_size_bytes + CASE WHEN EXISTS (SELECT 1 FROM ins) THEN $5 ELSE 0 END,
			    duration_ms      = GREATEST(replay_sessions.duration_ms, $7),
			    has_error        = replay_sessions.has_error OR $8,
			    has_rage_click   = replay_sessions.has_rage_click OR $9,
			    has_dead_click   = replay_sessions.has_dead_click OR $10,
			    end_time         = GREATEST(COALESCE(replay_sessions.end_time, now()), now()),
			    updated_at       = now()
			WHERE project_id = $1 AND id = $2
			RETURNING chunk_count`,
			projectID, req.SessionID, req.ChunkSeq, objectKey, req.SizeBytes, req.EventCount,
			req.DurationMs, req.HasError, req.HasRageClick, req.HasDeadClick).Scan(&currentCount)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("acknowledge chunk: %w", err)
		}
		if currentCount > 0 {
			chunkCount = currentCount
		}
	}

	return c.JSON(ReplayChunkAckResponse{
		Status:     "acknowledged",
		SessionID:  req.SessionID,
		ChunkSeq:   req.ChunkSeq,
		ChunkCount: chunkCount,
	})
}

// ListProjectSessions handles GET /api/v1/projects/:projectID/sessions.
func (h *Handler) ListProjectSessions(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)

	limit := c.QueryInt("limit", 25)
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	offset := c.QueryInt("offset", 0)
	if offset < 0 {
		offset = 0
	}

	hasErrorParam := strings.TrimSpace(c.Query("has_error"))
	hasRageParam := strings.TrimSpace(c.Query("has_rage_click"))
	hasDeadParam := strings.TrimSpace(c.Query("has_dead_click"))
	userIDParam := strings.TrimSpace(c.Query("user_id"))
	envParam := strings.TrimSpace(c.Query("environment"))
	fromParam := strings.TrimSpace(c.Query("from"))
	toParam := strings.TrimSpace(c.Query("to"))

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db == nil {
		return c.JSON(fiber.Map{
			"items":  []ReplaySessionItem{},
			"total":  0,
			"limit":  limit,
			"offset": offset,
		})
	}

	query := `
		SELECT
			id, project_id, user_id, environment, start_time, end_time,
			duration_ms, chunk_count, event_count, total_size_bytes,
			has_error, has_rage_click, has_dead_click, storage_prefix, created_at, updated_at,
			COUNT(*) OVER() AS full_count
		FROM replay_sessions
		WHERE project_id = $1`

	args := []any{projectID}
	argIdx := 2

	if hasErrorParam != "" {
		hasErrorBool := hasErrorParam == "true" || hasErrorParam == "1"
		query += fmt.Sprintf(" AND has_error = $%d", argIdx)
		args = append(args, hasErrorBool)
		argIdx++
	}

	if hasRageParam != "" {
		hasRageBool := hasRageParam == "true" || hasRageParam == "1"
		query += fmt.Sprintf(" AND has_rage_click = $%d", argIdx)
		args = append(args, hasRageBool)
		argIdx++
	}

	if hasDeadParam != "" {
		hasDeadBool := hasDeadParam == "true" || hasDeadParam == "1"
		query += fmt.Sprintf(" AND has_dead_click = $%d", argIdx)
		args = append(args, hasDeadBool)
		argIdx++
	}

	if userIDParam != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argIdx)
		args = append(args, userIDParam)
		argIdx++
	}

	if envParam != "" {
		query += fmt.Sprintf(" AND environment = $%d", argIdx)
		args = append(args, envParam)
		argIdx++
	}

	if fromParam != "" {
		if t, err := time.Parse(time.RFC3339, fromParam); err == nil {
			query += fmt.Sprintf(" AND start_time >= $%d", argIdx)
			args = append(args, t)
			argIdx++
		}
	}

	if toParam != "" {
		if t, err := time.Parse(time.RFC3339, toParam); err == nil {
			query += fmt.Sprintf(" AND start_time <= $%d", argIdx)
			args = append(args, t)
			argIdx++
		}
	}

	query += fmt.Sprintf(" ORDER BY start_time DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("list replay sessions: %w", err)
	}
	defer rows.Close()

	items := make([]ReplaySessionItem, 0, limit)
	var total int64

	for rows.Next() {
		var item ReplaySessionItem
		var fullCount int64

		if err := rows.Scan(
			&item.ID, &item.ProjectID, &item.UserID, &item.Environment,
			&item.StartTime, &item.EndTime, &item.DurationMs, &item.ChunkCount,
			&item.EventCount, &item.TotalSizeBytes, &item.HasError, &item.HasRageClick, &item.HasDeadClick,
			&item.StoragePrefix, &item.CreatedAt, &item.UpdatedAt, &fullCount,
		); err != nil {
			return fmt.Errorf("scan replay session: %w", err)
		}

		total = fullCount
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows replay session: %w", err)
	}

	return c.JSON(fiber.Map{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetProjectSession handles GET /api/v1/projects/:projectID/sessions/:sessionID.
func (h *Handler) GetProjectSession(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	sessionID := strings.TrimSpace(c.Params("sessionID"))

	if !sessionIDRegex.MatchString(sessionID) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid session_id format")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db == nil {
		return fiber.NewError(fiber.StatusNotFound, "session not found")
	}

	var session ReplaySessionItem
	err := h.db.QueryRow(ctx, `
		SELECT
			id, project_id, user_id, environment, start_time, end_time,
			duration_ms, chunk_count, event_count, total_size_bytes,
			has_error, has_rage_click, has_dead_click, storage_prefix, created_at, updated_at
		FROM replay_sessions
		WHERE project_id = $1 AND id = $2`, projectID, sessionID).Scan(
		&session.ID, &session.ProjectID, &session.UserID, &session.Environment,
		&session.StartTime, &session.EndTime, &session.DurationMs, &session.ChunkCount,
		&session.EventCount, &session.TotalSizeBytes, &session.HasError, &session.HasRageClick, &session.HasDeadClick,
		&session.StoragePrefix, &session.CreatedAt, &session.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "session not found")
	}
	if err != nil {
		return fmt.Errorf("query session: %w", err)
	}

	// Fetch chunks
	chunkRows, err := h.db.Query(ctx, `
		SELECT chunk_seq, object_key, size_bytes, event_count, created_at
		FROM replay_chunks
		WHERE project_id = $1 AND session_id = $2
		ORDER BY chunk_seq ASC`, projectID, sessionID)
	if err != nil {
		return fmt.Errorf("query replay chunks: %w", err)
	}
	defer chunkRows.Close()

	chunks := make([]ReplayChunkDetail, 0)
	for chunkRows.Next() {
		var chunk ReplayChunkDetail
		if err := chunkRows.Scan(&chunk.ChunkSeq, &chunk.ObjectKey, &chunk.SizeBytes, &chunk.EventCount, &chunk.CreatedAt); err != nil {
			return fmt.Errorf("scan replay chunk: %w", err)
		}

		if h.replayStore != nil {
			if !h.cfg.ReplayProxyUpload {
				getURL, err := h.replayStore.GeneratePresignedGet(ctx, chunk.ObjectKey, 15*time.Minute)
				if err == nil {
					chunk.URL = getURL
				}
			}
			if chunk.URL == "" {
				chunk.URL = h.signedChunkURL(c, "download-chunk", chunk.ObjectKey, time.Hour)
			}
		}

		chunks = append(chunks, chunk)
	}
	if err := chunkRows.Err(); err != nil {
		return fmt.Errorf("chunk rows: %w", err)
	}

	// Fetch correlated telemetry events for player timeline markers
	eventRows, err := h.db.Query(ctx, `
		SELECT id, kind, name, level, message, session_id, user_id, environment, release_version, event_timestamp, attributes
		FROM telemetry_events
		WHERE project_id = $1 AND session_id = $2
		ORDER BY event_timestamp ASC
		LIMIT 200`, projectID, sessionID)

	timelineEvents := make([]TelemetryEventItem, 0)
	if err == nil {
		defer eventRows.Close()
		for eventRows.Next() {
			var ev TelemetryEventItem
			var rawID any
			if err := eventRows.Scan(
				&rawID, &ev.Kind, &ev.Name, &ev.Level, &ev.Message,
				&ev.SessionID, &ev.UserID, &ev.Environment, &ev.ReleaseVersion,
				&ev.EventTimestamp, &ev.Attributes,
			); err == nil {
				ev.ID = fmt.Sprintf("%v", rawID)
				timelineEvents = append(timelineEvents, ev)
			}
		}
	}

	return c.JSON(ReplaySessionDetailResponse{
		Session:        session,
		Chunks:         chunks,
		TimelineEvents: timelineEvents,
	})
}
