package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/tiofani03/artracker/backend/internal/analytics"
	"github.com/tiofani03/artracker/backend/internal/ingest"
)

type BucketPoint struct {
	Label    string `json:"label"`
	Sessions int    `json:"sessions"`
	Errors   int    `json:"errors"`
}

type ProjectStatsResponse struct {
	Range         string        `json:"range"`
	SessionsCount int64         `json:"sessions_count"`
	EventsCount   int64         `json:"events_count"`
	ErrorsCount   int64         `json:"errors_count"`
	ErrorFreePct  string        `json:"error_free_pct"`
	ErrorNote     string        `json:"error_note"`
	Buckets       []BucketPoint `json:"buckets"`
}

type TelemetryEventItem struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Name           string          `json:"name"`
	Level          string          `json:"level"`
	Message        string          `json:"message"`
	SessionID      string          `json:"session_id"`
	UserID         string          `json:"user_id"`
	Environment    string          `json:"environment"`
	ReleaseVersion string          `json:"release_version"`
	EventTimestamp time.Time       `json:"event_timestamp"`
	Attributes     json.RawMessage `json:"attributes"`
}

// GetProjectStats returns aggregated metrics and time-series buckets for the overview dashboard.
func (h *Handler) GetProjectStats(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	rangeKey := strings.ToLower(strings.TrimSpace(c.Query("range", "24h")))
	if rangeKey != "7d" && rangeKey != "30d" {
		rangeKey = "24h"
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db == nil {
		// In-memory or SKIP_DB fallback
		return c.JSON(h.statsFromMemory(projectID, rangeKey))
	}

	var intervalStr, bucketGenSQL string
	var sinceInterval string

	switch rangeKey {
	case "7d":
		sinceInterval = "7 days"
		intervalStr = "1 day"
		bucketGenSQL = `
			SELECT 
				to_char(b, 'Dy') as label,
				count(DISTINCT t.session_id) FILTER (WHERE t.session_id <> '') as sessions,
				count(t.id) FILTER (WHERE t.kind = 'log' AND t.level IN ('error', 'fatal')) as errors
			FROM generate_series(
				date_trunc('day', now() - interval '6 days'),
				date_trunc('day', now()),
				interval '1 day'
			) b
			LEFT JOIN telemetry_events t 
				ON t.project_id = $1 
				AND t.event_timestamp >= b 
				AND t.event_timestamp < b + interval '1 day'
			GROUP BY b
			ORDER BY b ASC`
	case "30d":
		sinceInterval = "30 days"
		intervalStr = "1 day"
		bucketGenSQL = `
			SELECT 
				to_char(b, 'DD Mon') as label,
				count(DISTINCT t.session_id) FILTER (WHERE t.session_id <> '') as sessions,
				count(t.id) FILTER (WHERE t.kind = 'log' AND t.level IN ('error', 'fatal')) as errors
			FROM generate_series(
				date_trunc('day', now() - interval '29 days'),
				date_trunc('day', now()),
				interval '1 day'
			) b
			LEFT JOIN telemetry_events t 
				ON t.project_id = $1 
				AND t.event_timestamp >= b 
				AND t.event_timestamp < b + interval '1 day'
			GROUP BY b
			ORDER BY b ASC`
	default: // 24h
		sinceInterval = "24 hours"
		intervalStr = "2 hours"
		bucketGenSQL = `
			SELECT 
				to_char(b, 'HH24:00') as label,
				count(DISTINCT t.session_id) FILTER (WHERE t.session_id <> '') as sessions,
				count(t.id) FILTER (WHERE t.kind = 'log' AND t.level IN ('error', 'fatal')) as errors
			FROM generate_series(
				date_trunc('hour', now() - interval '22 hours'),
				date_trunc('hour', now()),
				interval '2 hours'
			) b
			LEFT JOIN telemetry_events t 
				ON t.project_id = $1 
				AND t.event_timestamp >= b 
				AND t.event_timestamp < b + interval '2 hours'
			GROUP BY b
			ORDER BY b ASC`
	}
	_ = intervalStr

	var sessionsCount, eventsCount, errorsCount int64
	summarySQL := fmt.Sprintf(`
		SELECT 
			count(DISTINCT session_id) FILTER (WHERE session_id <> ''),
			count(id),
			count(id) FILTER (WHERE kind = 'log' AND level IN ('error', 'fatal'))
		FROM telemetry_events
		WHERE project_id = $1 AND event_timestamp >= now() - interval '%s'`, sinceInterval)

	err := h.db.QueryRow(ctx, summarySQL, projectID).Scan(&sessionsCount, &eventsCount, &errorsCount)
	if err != nil {
		return err
	}

	var errorSessionsCount int64
	errSessionsSQL := fmt.Sprintf(`
		SELECT count(DISTINCT session_id)
		FROM telemetry_events
		WHERE project_id = $1 
		  AND event_timestamp >= now() - interval '%s'
		  AND kind = 'log' AND level IN ('error', 'fatal')
		  AND session_id <> ''`, sinceInterval)
	_ = h.db.QueryRow(ctx, errSessionsSQL, projectID).Scan(&errorSessionsCount)

	errorFreePct := "100.0%"
	if sessionsCount > 0 {
		pct := (1.0 - (float64(errorSessionsCount) / float64(sessionsCount))) * 100.0
		if pct < 0 {
			pct = 0
		}
		errorFreePct = fmt.Sprintf("%.2f%%", pct)
	}

	rows, err := h.db.Query(ctx, bucketGenSQL, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()

	buckets := []BucketPoint{}
	for rows.Next() {
		var bp BucketPoint
		if err := rows.Scan(&bp.Label, &bp.Sessions, &bp.Errors); err != nil {
			return err
		}
		buckets = append(buckets, bp)
	}

	errorNote := "0 unhandled"
	if errorsCount > 0 {
		errorNote = fmt.Sprintf("%d unhandled", errorsCount)
	}

	return c.JSON(ProjectStatsResponse{
		Range:         rangeKey,
		SessionsCount: sessionsCount,
		EventsCount:   eventsCount,
		ErrorsCount:   errorsCount,
		ErrorFreePct:  errorFreePct,
		ErrorNote:     errorNote,
		Buckets:       buckets,
	})
}

// ListProjectEvents returns ingested events/logs for a project.
func (h *Handler) ListProjectEvents(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	kindFilter := strings.ToLower(strings.TrimSpace(c.Query("kind", "all")))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	if h.db == nil {
		return c.JSON(fiber.Map{"items": h.eventsFromMemory(projectID, kindFilter, limit)})
	}

	query := `
		SELECT 
			id::text, kind, name, level, message, session_id, user_id, 
			environment, release_version, event_timestamp, attributes
		FROM telemetry_events
		WHERE project_id = $1
		  AND ($2 = '' OR kind = $2)
		ORDER BY event_timestamp DESC
		LIMIT $3`

	dbKind := kindFilter
	if dbKind == "all" {
		dbKind = ""
	}

	rows, err := h.db.Query(ctx, query, projectID, dbKind, limit)
	if err != nil {
		return err
	}
	defer rows.Close()

	items := []TelemetryEventItem{}
	for rows.Next() {
		var item TelemetryEventItem
		var attrBytes []byte
		if err := rows.Scan(
			&item.ID,
			&item.Kind,
			&item.Name,
			&item.Level,
			&item.Message,
			&item.SessionID,
			&item.UserID,
			&item.Environment,
			&item.ReleaseVersion,
			&item.EventTimestamp,
			&attrBytes,
		); err != nil {
			return err
		}
		if len(attrBytes) > 0 {
			item.Attributes = attrBytes
		} else {
			item.Attributes = json.RawMessage("{}")
		}
		items = append(items, item)
	}

	return c.JSON(fiber.Map{"items": items})
}

func (h *Handler) statsFromMemory(projectID int64, rangeKey string) ProjectStatsResponse {
	var rows []ingest.TelemetryRow
	if h.ingestSvc != nil {
		if memSink, ok := h.ingestSvc.Sink().(*ingest.MemoryStorageSink); ok {
			for _, r := range memSink.Rows() {
				if r.ProjectID == projectID {
					rows = append(rows, r)
				}
			}
		}
	}

	sessionsMap := make(map[string]bool)
	var errorsCount int64
	for _, r := range rows {
		if r.SessionID != "" {
			sessionsMap[r.SessionID] = true
		}
		if r.Kind == "log" && (r.Level == "error" || r.Level == "fatal") {
			errorsCount++
		}
	}

	errorNote := "0 unhandled"
	if errorsCount > 0 {
		errorNote = fmt.Sprintf("%d unhandled", errorsCount)
	}

	return ProjectStatsResponse{
		Range:         rangeKey,
		SessionsCount: int64(len(sessionsMap)),
		EventsCount:   int64(len(rows)),
		ErrorsCount:   errorsCount,
		ErrorFreePct:  "100.0%",
		ErrorNote:     errorNote,
		Buckets:       []BucketPoint{},
	}
}

func (h *Handler) eventsFromMemory(projectID int64, kindFilter string, limit int) []TelemetryEventItem {
	var items []TelemetryEventItem
	if h.ingestSvc != nil {
		if memSink, ok := h.ingestSvc.Sink().(*ingest.MemoryStorageSink); ok {
			for _, r := range memSink.Rows() {
				if r.ProjectID != projectID {
					continue
				}
				if kindFilter != "all" && r.Kind != kindFilter {
					continue
				}
				items = append(items, TelemetryEventItem{
					ID:             r.ID.String(),
					Kind:           r.Kind,
					Name:           r.Name,
					Level:          r.Level,
					Message:        r.Message,
					SessionID:      r.SessionID,
					UserID:         r.UserID,
					Environment:    r.Environment,
					ReleaseVersion: r.ReleaseVersion,
					EventTimestamp: r.EventTimestamp,
					Attributes:     r.Attributes,
				})
				if len(items) >= limit {
					break
				}
			}
		}
	}
	return items
}

// SeedProjectMockData creates 50 realistic telemetry records for the project spanning the last 24h.
func (h *Handler) SeedProjectMockData(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	if h.ingestSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "ingest service unavailable")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	now := time.Now()
	sampleEvents := []struct {
		kind    string
		name    string
		level   string
		message string
	}{
		{"event", "page_view", "", ""},
		{"event", "view_product", "", ""},
		{"event", "add_to_cart", "", ""},
		{"event", "checkout_step_1", "", ""},
		{"event", "checkout_step_2", "", ""},
		{"event", "purchase_completed", "", ""},
		{"log", "", "info", "User authentication token refreshed"},
		{"log", "", "warn", "Slow query execution: latency 1,420ms"},
		{"log", "", "error", "TypeError: Cannot read properties of undefined (reading 'charge_id')"},
		{"log", "", "error", "POST /api/v1/charge → 502 Bad Gateway (upstream timeout)"},
	}

	rows := make([]ingest.TelemetryRow, 0, 50)
	for i := 0; i < 50; i++ {
		sample := sampleEvents[i%len(sampleEvents)]
		eventTime := now.Add(-time.Duration(i*28) * time.Minute)
		sessionID := fmt.Sprintf("ses_mock_%d", (i%7)+1)
		userID := fmt.Sprintf("usr_%d", (i%5)+1)

		rowID, err := uuid.NewV7()
		if err != nil {
			rowID = uuid.New()
		}

		attrMap := map[string]any{
			"page":       "/checkout",
			"index":      i + 1,
			"browser":    "Chrome 131",
			"cart_total": 250000 + (i * 15000),
		}
		attrBytes, _ := json.Marshal(attrMap)

		rows = append(rows, ingest.TelemetryRow{
			ID:              rowID,
			ProjectID:       projectID,
			Kind:            sample.kind,
			Name:            sample.name,
			Level:           sample.level,
			Message:         sample.message,
			SessionID:       sessionID,
			UserID:          userID,
			Environment:     "production",
			ReleaseVersion:  "2.4.1",
			ClientTimestamp: &eventTime,
			EventTimestamp:  eventTime,
			ReceivedAt:      eventTime,
			Attributes:      attrBytes,
			SDKMeta:         []byte(`{"name":"artracker-web","version":"1.2.0"}`),
		})
	}

	if err := h.ingestSvc.Sink().WriteBatch(ctx, rows); err != nil {
		return err
	}

	// Also seed Growth Analytics events & cohort history if analytics service is enabled
	if h.analyticsSvc != nil {
		analyticsRows := make([]analytics.EventRow, 0, 150)
		sources := []string{"google", "meta_ads", "twitter", "email", "organic"}
		browsers := []string{"Chrome", "Safari", "Firefox"}

		// Generate multi-week funnel conversions
		for w := 3; w >= 0; w-- {
			weekBase := now.Add(-time.Duration(w*7*24) * time.Hour)
			for u := 1; u <= 30; u++ {
				uID := fmt.Sprintf("usr_%d_%d", w, u)
				dID := fmt.Sprintf("dev_%d_%d", w, u)
				utm := sources[(w+u)%len(sources)]
				browser := browsers[u%len(browsers)]
				userStart := weekBase.Add(time.Duration(u*45) * time.Minute)

				rID1, _ := uuid.NewV7()
				analyticsRows = append(analyticsRows, analytics.EventRow{
					ID: rID1, ProjectID: projectID, SessionID: fmt.Sprintf("ses_%d_%d", w, u),
					UserID: uID, DeviceID: dID, ActorID: "u:" + uID, EventName: "landing_view",
					Properties: []byte(`{"plan":"pro"}`), UTMSource: utm, Browser: browser, OS: "Web",
					Timestamp: userStart,
				})

				// 75% view product
				if u%4 != 0 {
					rID2, _ := uuid.NewV7()
					analyticsRows = append(analyticsRows, analytics.EventRow{
						ID: rID2, ProjectID: projectID, SessionID: fmt.Sprintf("ses_%d_%d", w, u),
						UserID: uID, DeviceID: dID, ActorID: "u:" + uID, EventName: "view_product",
						Properties: []byte(`{"plan":"pro"}`), UTMSource: utm, Browser: browser, OS: "Web",
						Timestamp: userStart.Add(3 * time.Minute),
					})
				}

				// 45% add to cart
				if u%2 == 0 {
					rID3, _ := uuid.NewV7()
					analyticsRows = append(analyticsRows, analytics.EventRow{
						ID: rID3, ProjectID: projectID, SessionID: fmt.Sprintf("ses_%d_%d", w, u),
						UserID: uID, DeviceID: dID, ActorID: "u:" + uID, EventName: "add_to_cart",
						Properties: []byte(`{"plan":"pro"}`), UTMSource: utm, Browser: browser, OS: "Web",
						Timestamp: userStart.Add(7 * time.Minute),
					})
				}

				// 25% checkout
				if u%4 == 1 {
					rID4, _ := uuid.NewV7()
					analyticsRows = append(analyticsRows, analytics.EventRow{
						ID: rID4, ProjectID: projectID, SessionID: fmt.Sprintf("ses_%d_%d", w, u),
						UserID: uID, DeviceID: dID, ActorID: "u:" + uID, EventName: "checkout_start",
						Properties: []byte(`{"plan":"pro"}`), UTMSource: utm, Browser: browser, OS: "Web",
						Timestamp: userStart.Add(11 * time.Minute),
					})

					// 15% complete purchase
					if u%8 == 1 {
						rID5, _ := uuid.NewV7()
						analyticsRows = append(analyticsRows, analytics.EventRow{
							ID: rID5, ProjectID: projectID, SessionID: fmt.Sprintf("ses_%d_%d", w, u),
							UserID: uID, DeviceID: dID, ActorID: "u:" + uID, EventName: "purchase_completed",
							Properties: []byte(`{"plan":"pro","revenue":250000}`), UTMSource: utm, Browser: browser, OS: "Web",
							Timestamp: userStart.Add(15 * time.Minute),
						})
					}
				}

				// Returning events in subsequent weeks for realistic Cohort Retention
				for rWeek := 1; rWeek <= w; rWeek++ {
					if (u+rWeek)%2 == 0 {
						retTime := userStart.Add(time.Duration(rWeek*7*24)*time.Hour + time.Duration((u%24))*time.Hour)
						if retTime.Before(now) {
							retEvID, _ := uuid.NewV7()
							retSession := fmt.Sprintf("ses_ret_%s_%d", uID, rWeek)
							analyticsRows = append(analyticsRows, analytics.EventRow{
								ID: retEvID, ProjectID: projectID, SessionID: retSession,
								UserID: uID, DeviceID: dID, ActorID: "u:" + uID, EventName: "view_product",
								Properties: []byte(`{"source":"retention"}`), UTMSource: utm, Browser: browser, OS: "Web",
								Timestamp: retTime,
							})
						}
					}
				}
			}
		}

		_ = h.analyticsSvc.Sink().WriteBatch(ctx, analyticsRows, nil)

		// Seed default saved funnels if none exist
		existingFunnels, _ := h.analyticsSvc.ListSavedFunnels(ctx, projectID)
		if len(existingFunnels) == 0 {
			_, _ = h.analyticsSvc.CreateSavedFunnel(ctx, projectID, nil, analytics.SaveFunnelInput{
				Name:          "Acquisition & Checkout Funnel",
				Steps:         []string{"landing_view", "view_product", "add_to_cart", "checkout_start", "purchase_completed"},
				WindowSeconds: 86400,
				CountingMode:  "users",
				BreakdownBy:   "utm_source",
			})
			_, _ = h.analyticsSvc.CreateSavedFunnel(ctx, projectID, nil, analytics.SaveFunnelInput{
				Name:          "SaaS Product Activation",
				Steps:         []string{"landing_view", "view_product", "checkout_start", "purchase_completed"},
				WindowSeconds: 43200,
				CountingMode:  "users",
				BreakdownBy:   "browser",
			})
		}
	}

	// Also seed diagnostic issues and replay captures if database is connected
	if h.db != nil {
		var issueCount int64
		_ = h.db.QueryRow(ctx, `SELECT count(*) FROM issues WHERE project_id = $1`, projectID).Scan(&issueCount)
		if issueCount == 0 {
			var issueID int64
			_ = h.db.QueryRow(ctx, `
				INSERT INTO issues (project_id, fingerprint, title, type, status, first_seen, last_seen, count)
				VALUES ($1, 'fp_type_error_charge_id', 'TypeError: Cannot read properties of undefined (reading ''charge_id'')', 'TypeError', 'open', now() - interval '2 days', now() - interval '4 minutes', 28)
				ON CONFLICT DO NOTHING RETURNING id
			`, projectID).Scan(&issueID)
			if issueID > 0 {
				_, _ = h.db.Exec(ctx, `
					INSERT INTO issue_events (issue_id, session_id, user_id, stack_trace, message, breadcrumbs, device_info, release_version, created_at)
					VALUES ($1, 'ses_94k2p8', 'usr_1', 'TypeError: Cannot read properties of undefined (reading ''charge_id'')\n    at PaymentForm.tsx:142:19', 'Cannot read properties of undefined (reading ''charge_id'')', '[]'::jsonb, '{"browser":"Chrome 131"}'::jsonb, '2.4.1', now() - interval '4 minutes')
				`, issueID)
			}
			_, _ = h.db.Exec(ctx, `
				INSERT INTO issues (project_id, fingerprint, title, type, status, first_seen, last_seen, count)
				VALUES 
				($1, 'fp_net_502_charge', 'POST /api/v1/charge → 502 Bad Gateway (upstream timeout)', 'NetworkError', 'open', now() - interval '1 day', now() - interval '18 minutes', 19),
				($1, 'fp_unhandled_rejection_fetch', 'UnhandledRejection: NetworkError when attempting to fetch resource', 'UnhandledRejection', 'open', now() - interval '3 days', now() - interval '42 minutes', 14)
				ON CONFLICT DO NOTHING
			`, projectID)
		}

		var replayCount int64
		_ = h.db.QueryRow(ctx, `SELECT count(*) FROM replay_sessions WHERE project_id = $1`, projectID).Scan(&replayCount)
		if replayCount == 0 {
			_, _ = h.db.Exec(ctx, `
				INSERT INTO replay_sessions (id, project_id, user_id, environment, start_time, duration_ms, chunk_count, event_count, total_size_bytes, has_error, has_rage_click, has_dead_click, storage_prefix, created_at, updated_at)
				VALUES 
				('ses_94k2p8', $1, 'nadia.putri@fintech.id', 'production', now() - interval '42 minutes', 102000, 3, 480, 48200, true, true, false, 'projects/1/sessions/ses_94k2p8/', now(), now()),
				('ses_71m9x3', $1, 'rian.wijaya@commerce.co', 'production', now() - interval '2 hours', 138000, 4, 620, 52100, false, false, true, 'projects/1/sessions/ses_71m9x3/', now(), now()),
				('ses_58c4v1', $1, 'kevin.tan@saas.sg', 'production', now() - interval '5 hours', 185000, 5, 890, 68900, false, false, false, 'projects/1/sessions/ses_58c4v1/', now(), now())
				ON CONFLICT DO NOTHING
			`, projectID)
		}
	}

	return c.JSON(fiber.Map{
		"status": "ok",
		"seeded": len(rows),
	})
}

// SendProjectTestEvent ingests 1 real telemetry event into the pipeline and database.
func (h *Handler) SendProjectTestEvent(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	if h.ingestSvc == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "ingest service unavailable")
	}

	now := time.Now()
	rowID, err := uuid.NewV7()
	if err != nil {
		rowID = uuid.New()
	}

	job := ingest.BatchJob{
		ProjectID:  projectID,
		ReceivedAt: now,
		Envelope: ingest.IngestEnvelope{
			SentAt: &now,
			SDK:    ingest.SDKMeta{Name: "artracker-console", Version: "1.0.0"},
			Context: map[string]any{
				"environment": "production",
				"app_version": "1.0.0",
			},
			Items: []ingest.RawItem{
				{
					ID:         rowID.String(),
					Type:       "event",
					Name:       "manual_test_event",
					Timestamp:  &now,
					SessionID:  fmt.Sprintf("sess_%d", now.Unix()),
					Attributes: json.RawMessage(`{"triggered_from":"dashboard","source":"console_ping"}`),
				},
			},
		},
	}

	if !h.ingestSvc.WorkerPool().Enqueue(job) {
		return fiber.NewError(fiber.StatusServiceUnavailable, "ingest buffer full")
	}
	h.ingestSvc.Metrics().IncAcceptedRequests()
	h.ingestSvc.Metrics().AddEventsReceived(1)

	return c.JSON(fiber.Map{"status": "accepted", "event_id": rowID.String()})
}
