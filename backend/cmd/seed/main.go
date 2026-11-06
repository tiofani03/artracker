package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load(".env", "../.env", "../../.env")
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		dsn = "postgres://artracker:artracker@localhost:5433/artracker?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	defer pool.Close()

	// Find all projects
	rows, err := pool.Query(ctx, `SELECT id, name FROM projects ORDER BY id`)
	if err != nil {
		log.Fatalf("failed to query projects: %v", err)
	}
	defer rows.Close()

	type Proj struct {
		ID   int64
		Name string
	}
	var projects []Proj
	for rows.Next() {
		var p Proj
		if err := rows.Scan(&p.ID, &p.Name); err == nil {
			projects = append(projects, p)
		}
	}
	rows.Close()

	if len(projects) == 0 {
		log.Fatalf("no projects found in database to seed")
	}

	for _, p := range projects {
		log.Printf("===> Seeding rich mock data for Project %d (%s)...", p.ID, p.Name)
		if err := seedProject(ctx, pool, p.ID); err != nil {
			log.Printf("ERROR seeding project %d: %v", p.ID, err)
		} else {
			log.Printf("SUCCESS: Project %d (%s) seeded completely!", p.ID, p.Name)
		}
	}
}

func seedProject(ctx context.Context, pool *pgxpool.Pool, projectID int64) error {
	now := time.Now().UTC()
	rng := rand.New(rand.NewSource(projectID * 42))

	sources := []string{"google", "facebook", "instagram", "email", "github", "twitter"}
	mediums := []string{"cpc", "paid_social", "email", "referral", "organic"}
	campaigns := []string{"utm_launch_q4", "utm_ig_reels", "utm_newsletter_nov", "utm_github_readme"}
	browsers := []string{"Chrome 131", "Safari 18", "Firefox 133", "Edge 130"}
	oses := []string{"macOS", "iOS", "Android", "Windows", "Linux"}

	// 1. Seed Growth Analytics Events (events table)
	// Multi-week funnel + cohort returning activity across past 8 weeks
	var eventRows [][]any
	userCount := 80

	for w := 7; w >= 0; w-- {
		weekStart := now.Add(-time.Duration(w*7*24) * time.Hour)
		usersThisWeek := 10 + rng.Intn(5) // 10-15 new users per week

		for u := 0; u < usersThisWeek; u++ {
			uIndex := w*15 + u
			if uIndex >= userCount {
				uIndex = uIndex % userCount
			}
			userID := fmt.Sprintf("usr_%d", uIndex+1)
			deviceID := fmt.Sprintf("dev_%d", uIndex+1)
			actorID := "u:" + userID
			sessionID := fmt.Sprintf("ses_%d_%d", w, u)

			src := sources[rng.Intn(len(sources))]
			med := mediums[rng.Intn(len(mediums))]
			cmp := campaigns[rng.Intn(len(campaigns))]
			browser := browsers[rng.Intn(len(browsers))]
			osName := oses[rng.Intn(len(oses))]

			dayOffset := time.Duration(rng.Intn(6)) * 24 * time.Hour
			hourOffset := time.Duration(rng.Intn(18)) * time.Hour
			userStart := weekStart.Add(dayOffset).Add(hourOffset)
			if userStart.After(now) {
				userStart = now.Add(-time.Duration(rng.Intn(120)) * time.Minute)
			}

			// Funnel Step 1: landing_view (100%)
			prop1, _ := json.Marshal(map[string]any{"plan": "pro", "ref": src, "page": "/landing"})
			evID1, _ := uuid.NewV7()
			eventRows = append(eventRows, []any{
				evID1, projectID, sessionID, userID, deviceID, actorID,
				"landing_view", prop1, src, med, cmp, browser, osName, userStart,
			})

			// Funnel Step 2: view_product (75%)
			if rng.Float64() < 0.75 {
				evID2, _ := uuid.NewV7()
				t2 := userStart.Add(time.Duration(1+rng.Intn(3)) * time.Minute)
				prop2, _ := json.Marshal(map[string]any{"plan": "pro", "product_id": "prod_enterprise_99"})
				eventRows = append(eventRows, []any{
					evID2, projectID, sessionID, userID, deviceID, actorID,
					"view_product", prop2, src, med, cmp, browser, osName, t2,
				})

				// Funnel Step 3: add_to_cart (55%)
				if rng.Float64() < 0.70 {
					evID3, _ := uuid.NewV7()
					t3 := t2.Add(time.Duration(2+rng.Intn(5)) * time.Minute)
					prop3, _ := json.Marshal(map[string]any{"plan": "pro", "price": 250000, "qty": 1})
					eventRows = append(eventRows, []any{
						evID3, projectID, sessionID, userID, deviceID, actorID,
						"add_to_cart", prop3, src, med, cmp, browser, osName, t3,
					})

					// Funnel Step 4: checkout_start (40%)
					if rng.Float64() < 0.65 {
						evID4, _ := uuid.NewV7()
						t4 := t3.Add(time.Duration(3+rng.Intn(6)) * time.Minute)
						prop4, _ := json.Marshal(map[string]any{"plan": "pro", "cart_total": 250000})
						eventRows = append(eventRows, []any{
							evID4, projectID, sessionID, userID, deviceID, actorID,
							"checkout_start", prop4, src, med, cmp, browser, osName, t4,
						})

						// Funnel Step 5: purchase_completed (25%)
						if rng.Float64() < 0.60 {
							evID5, _ := uuid.NewV7()
							t5 := t4.Add(time.Duration(2+rng.Intn(4)) * time.Minute)
							prop5, _ := json.Marshal(map[string]any{"plan": "pro", "revenue": 250000, "currency": "IDR", "order_id": fmt.Sprintf("ord_%d", rng.Intn(99999))})
							eventRows = append(eventRows, []any{
								evID5, projectID, sessionID, userID, deviceID, actorID,
								"purchase_completed", prop5, src, med, cmp, browser, osName, t5,
							})
						}
					}
				}
			}

			// Generate returning events in subsequent weeks for realistic Cohort Retention
			// Decay rates: W+1: ~38%, W+2: ~27%, W+3: ~20%, W+4: ~15%, W+5: ~12%, W+6: ~9%, W+7: ~7%
			decayRates := []float64{0.38, 0.27, 0.20, 0.15, 0.12, 0.09, 0.07}
			for rWeek := 1; rWeek <= w; rWeek++ {
				prob := decayRates[rWeek-1]
				if rng.Float64() < prob {
					retTime := userStart.Add(time.Duration(rWeek*7*24)*time.Hour + time.Duration(rng.Intn(48))*time.Hour)
					if retTime.Before(now) {
						retEvID, _ := uuid.NewV7()
						retSession := fmt.Sprintf("ses_ret_%s_%d", userID, rWeek)
						retProp, _ := json.Marshal(map[string]any{"source": "retention", "days_since_first": rWeek * 7})
						eventRows = append(eventRows, []any{
							retEvID, projectID, retSession, userID, deviceID, actorID,
							"view_product", retProp, src, med, cmp, browser, osName, retTime,
						})
					}
				}
			}
		}
	}

	// Insert events in batch
	copyEventsSQL := `
		INSERT INTO events (
			id, project_id, session_id, user_id, device_id, actor_id,
			event_name, properties, utm_source, utm_medium, utm_campaign,
			browser, os, timestamp
		)
		SELECT * FROM UNNEST(
			$1::uuid[], $2::bigint[], $3::text[], $4::text[], $5::text[], $6::text[],
			$7::text[], $8::jsonb[], $9::text[], $10::text[], $11::text[],
			$12::text[], $13::text[], $14::timestamptz[]
		)
		ON CONFLICT DO NOTHING
	`
	colID := make([]uuid.UUID, len(eventRows))
	colPID := make([]int64, len(eventRows))
	colSess := make([]string, len(eventRows))
	colUser := make([]string, len(eventRows))
	colDev := make([]string, len(eventRows))
	colActor := make([]string, len(eventRows))
	colEvName := make([]string, len(eventRows))
	colProps := make([][]byte, len(eventRows))
	colUTMSrc := make([]string, len(eventRows))
	colUTMMed := make([]string, len(eventRows))
	colUTMCmp := make([]string, len(eventRows))
	colBrowser := make([]string, len(eventRows))
	colOS := make([]string, len(eventRows))
	colTime := make([]time.Time, len(eventRows))

	for i, r := range eventRows {
		colID[i] = r[0].(uuid.UUID)
		colPID[i] = r[1].(int64)
		colSess[i] = r[2].(string)
		colUser[i] = r[3].(string)
		colDev[i] = r[4].(string)
		colActor[i] = r[5].(string)
		colEvName[i] = r[6].(string)
		colProps[i] = r[7].([]byte)
		colUTMSrc[i] = r[8].(string)
		colUTMMed[i] = r[9].(string)
		colUTMCmp[i] = r[10].(string)
		colBrowser[i] = r[11].(string)
		colOS[i] = r[12].(string)
		colTime[i] = r[13].(time.Time)
	}

	if _, err := pool.Exec(ctx, copyEventsSQL,
		colID, colPID, colSess, colUser, colDev, colActor,
		colEvName, colProps, colUTMSrc, colUTMMed, colUTMCmp,
		colBrowser, colOS, colTime,
	); err != nil {
		return fmt.Errorf("insert events: %w", err)
	}
	log.Printf("  -> Inserted %d growth marketing events into 'events'", len(eventRows))

	// 2. Seed Default Saved Funnels
	_, err := pool.Exec(ctx, `
		INSERT INTO saved_funnels (project_id, name, steps, window_seconds, counting_mode, breakdown_by, created_at, updated_at)
		VALUES 
		($1, 'Acquisition & Checkout Funnel', '["landing_view", "view_product", "add_to_cart", "checkout_start", "purchase_completed"]'::jsonb, 86400, 'users', 'utm_source', now(), now()),
		($1, 'SaaS Product Activation', '["landing_view", "view_product", "checkout_start", "purchase_completed"]'::jsonb, 43200, 'users', 'browser', now(), now())
		ON CONFLICT DO NOTHING
	`, projectID)
	if err != nil {
		log.Printf("  -> Note: saved_funnels insert: %v", err)
	} else {
		log.Printf("  -> Seeded 2 saved funnel presets into 'saved_funnels'")
	}

	// 3. Seed Telemetry Events (telemetry_events)
	sampleTelemetry := []struct {
		kind, name, level, message string
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
		{"log", "", "warn", "console.warn: Slow template hydration exceeded 2000ms budget"},
		{"log", "", "error", "UnhandledRejection: NetworkError when attempting to fetch resource"},
	}

	var batch pgx.Batch
	telemetryInsertSQL := `
		INSERT INTO telemetry_events (
			id, project_id, kind, name, level, message, session_id, user_id,
			environment, release_version, client_timestamp, event_timestamp, received_at,
			attributes, sdk_meta
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT DO NOTHING
	`
	for i := 0; i < 75; i++ {
		sample := sampleTelemetry[i%len(sampleTelemetry)]
		evTime := now.Add(-time.Duration(i*19) * time.Minute)
		sID := fmt.Sprintf("ses_mock_%d", (i%9)+1)
		uID := fmt.Sprintf("usr_%d", (i%6)+1)
		rowID, _ := uuid.NewV7()
		attr, _ := json.Marshal(map[string]any{
			"page":       "/checkout",
			"index":      i + 1,
			"browser":    "Chrome 131",
			"cart_total": 250000 + (i * 12500),
		})
		sdkMeta := []byte(`{"name":"artracker-web","version":"1.2.0"}`)

		batch.Queue(telemetryInsertSQL,
			rowID, projectID, sample.kind, sample.name, sample.level, sample.message,
			sID, uID, "production", "2.4.1", evTime, evTime, evTime, attr, sdkMeta,
		)
	}
	br := pool.SendBatch(ctx, &batch)
	if err := br.Close(); err != nil {
		log.Printf("  -> Note: telemetry batch: %v", err)
	} else {
		log.Printf("  -> Inserted 75 telemetry events into 'telemetry_events'")
	}

	// 4. Seed Diagnostic Issues (issues and issue_events table)
	issuesList := []struct {
		fingerprint string
		title       string
		errType     string
		status      string
		count       int64
		msg         string
		stack       string
	}{
		{
			fingerprint: "fp_type_error_charge_id",
			title:       "TypeError: Cannot read properties of undefined (reading 'charge_id')",
			errType:     "TypeError",
			status:      "open",
			count:       28,
			msg:         "Cannot read properties of undefined (reading 'charge_id')",
			stack:       "TypeError: Cannot read properties of undefined (reading 'charge_id')\n    at PaymentForm.tsx:142:19\n    at HTMLButtonElement.dispatch (react-dom.production.min.js:189:24)",
		},
		{
			fingerprint: "fp_net_502_charge",
			title:       "POST /api/v1/charge → 502 Bad Gateway (upstream timeout)",
			errType:     "NetworkError",
			status:      "open",
			count:       19,
			msg:         "502 Bad Gateway: payment provider webhook gateway timed out after 5000ms",
			stack:       "NetworkError: POST /api/v1/charge returned 502\n    at request (api.ts:52:25)\n    at submitOrder (checkout.ts:89:12)",
		},
		{
			fingerprint: "fp_unhandled_rejection_fetch",
			title:       "UnhandledRejection: NetworkError when attempting to fetch resource",
			errType:     "UnhandledRejection",
			status:      "open",
			count:       14,
			msg:         "NetworkError when attempting to fetch resource",
			stack:       "UnhandledRejection: NetworkError when attempting to fetch resource\n    at window.fetch (bundle.js:41:10)\n    at syncTelemetry (artracker.js:105:14)",
		},
		{
			fingerprint: "fp_range_error_call_stack",
			title:       "RangeError: Maximum call stack size exceeded in tree recursion",
			errType:     "RangeError",
			status:      "resolved",
			count:       8,
			msg:         "Maximum call stack size exceeded",
			stack:       "RangeError: Maximum call stack size exceeded\n    at traverseNode (TreeView.tsx:88:12)\n    at traverseNode (TreeView.tsx:94:16)",
		},
	}

	for _, iss := range issuesList {
		var issueID int64
		firstSeen := now.Add(-time.Duration(iss.count*4) * time.Hour)
		lastSeen := now.Add(-time.Duration(rng.Intn(45)) * time.Minute)

		err := pool.QueryRow(ctx, `
			INSERT INTO issues (project_id, fingerprint, title, type, status, first_seen, last_seen, count)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (project_id, fingerprint) 
			DO UPDATE SET count = issues.count + EXCLUDED.count, last_seen = EXCLUDED.last_seen
			RETURNING id
		`, projectID, iss.fingerprint, iss.title, iss.errType, iss.status, firstSeen, lastSeen, iss.count).Scan(&issueID)
		if err != nil {
			log.Printf("  -> Note: issue insert %s: %v", iss.fingerprint, err)
			continue
		}

		// Insert occurrence events
		breadcrumbs, _ := json.Marshal([]map[string]any{
			{"category": "navigation", "message": "navigated to /checkout", "timestamp": lastSeen.Add(-2 * time.Minute)},
			{"category": "ui.click", "message": "clicked button#submit-payment", "timestamp": lastSeen.Add(-10 * time.Second)},
		})
		deviceInfo, _ := json.Marshal(map[string]any{
			"browser": "Chrome 131",
			"os":      "macOS 15.1",
			"screen":  "1728x1117",
		})

		_, _ = pool.Exec(ctx, `
			INSERT INTO issue_events (issue_id, session_id, user_id, stack_trace, message, breadcrumbs, device_info, release_version, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, issueID, "ses_mock_err_1", "usr_1", iss.stack, iss.msg, breadcrumbs, deviceInfo, "2.4.1", lastSeen)
	}
	log.Printf("  -> Seeded 4 crash diagnostic issues and events into 'issues'")

	// 5. Seed Session Replay records (replay_sessions) + correlated timeline telemetry_events
	replaySessions := []struct {
		id           string
		userID       string
		durationMS   int64
		chunkCount   int
		eventCount   int64
		hasError     bool
		hasRageClick bool
		hasDeadClick bool
	}{
		{"ses_94k2p8", "nadia.putri@fintech.id", 102000, 3, 480, true, true, false},
		{"ses_71m9x3", "rian.wijaya@commerce.co", 138000, 4, 620, false, false, true},
		{"ses_58c4v1", "kevin.tan@saas.sg", 185000, 5, 890, false, false, false},
		{"ses_32x8d4", "fani.pratama@startup.id", 94000, 2, 340, true, false, true},
	}

	for _, rs := range replaySessions {
		sStart := now.Add(-time.Duration(rng.Intn(12)) * time.Hour)
		sEnd := sStart.Add(time.Duration(rs.durationMS) * time.Millisecond)
		_, err := pool.Exec(ctx, `
			INSERT INTO replay_sessions (
				id, project_id, user_id, environment, start_time, end_time,
				duration_ms, chunk_count, event_count, total_size_bytes,
				has_error, has_rage_click, has_dead_click, storage_prefix, created_at, updated_at
			) VALUES (
				$1, $2, $3, 'production', $4, $5, $6, $7, $8, 48200, $9, $10, $11,
				$12, now(), now()
			)
			ON CONFLICT (project_id, id) DO UPDATE SET
				has_rage_click = EXCLUDED.has_rage_click,
				has_dead_click = EXCLUDED.has_dead_click,
				updated_at = now()
		`, rs.id, projectID, rs.userID, sStart, sEnd, rs.durationMS, rs.chunkCount, rs.eventCount,
			rs.hasError, rs.hasRageClick, rs.hasDeadClick, fmt.Sprintf("projects/%d/sessions/%s/", projectID, rs.id))
		if err != nil {
			log.Printf("  -> Note: replay insert %s: %v", rs.id, err)
			continue
		}

		// Seed correlated timeline_events for each replay session
		type tlEv struct {
			offsetMS int64
			kind     string
			name     string
			level    string
			message  string
		}
		tl := []tlEv{
			{8000, "event", "page_view", "info", "Navigated to /checkout/payment"},
		}
		if rs.hasRageClick {
			tl = append(tl, tlEv{46000, "log", "rage_click", "warn", "Rage click ×4 on button#pay-submit within 380ms"})
		}
		if rs.hasDeadClick {
			tl = append(tl, tlEv{52000, "log", "dead_click", "warn", "Dead click on button.apply-coupon (no DOM/network activity in 1.5s)"})
		}
		if rs.hasError {
			tl = append(tl,
				tlEv{64000, "log", "http_error", "error", "POST /api/v1/charge → 502 Bad Gateway (1,840ms)"},
				tlEv{66000, "log", "TypeError", "error", "TypeError: Cannot read properties of undefined (reading 'charge_id')"},
			)
		} else {
			tl = append(tl, tlEv{rs.durationMS - 12000, "event", "purchase_completed", "info", "Order confirmed"})
		}
		for _, item := range tl {
			evID, _ := uuid.NewV7()
			evTS := sStart.Add(time.Duration(item.offsetMS) * time.Millisecond)
			_, _ = pool.Exec(ctx, telemetryInsertSQL,
				evID, projectID, item.kind, item.name, item.level, item.message,
				rs.id, rs.userID, "production", "2.4.1", evTS, evTS, evTS,
				[]byte(`{"source":"replay_timeline"}`), []byte(`{"name":"artracker-web","version":"1.2.0"}`),
			)
		}
	}
	log.Printf("  -> Seeded 4 session replay records into 'replay_sessions'")

	return nil
}
