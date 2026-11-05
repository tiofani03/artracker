package analytics

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestDeduplicator(t *testing.T) {
	dedup := NewMemoryDeduplicator(1 * time.Second)
	defer dedup.Close()

	if dedup.IsDuplicate(1, "evt_101") {
		t.Fatal("first time should not be duplicate")
	}
	if !dedup.IsDuplicate(1, "evt_101") {
		t.Fatal("second time within TTL should be duplicate")
	}
	if dedup.IsDuplicate(2, "evt_101") {
		t.Fatal("different project should not be duplicate")
	}
	if dedup.IsDuplicate(1, "evt_102") {
		t.Fatal("different event ID should not be duplicate")
	}
}

func TestEnricher(t *testing.T) {
	dedup := NewMemoryDeduplicator(1 * time.Minute)
	defer dedup.Close()
	enricher := NewEnricher(dedup)

	now := time.Now()
	sentAt := now
	itemTime := now.Add(-2 * time.Minute)

	envelope := EventEnvelope{
		SentAt: &sentAt,
		SDK:    SDKMeta{Name: "artracker-android", Version: "1.2.0"},
		Context: map[string]any{
			"device_id":       "dev_abc",
			"os":              "Android 15",
			"app_version":     "2.4.1",
			"utm_source":      "context_utm",
			"user_properties": map[string]any{"plan": "pro", "tier": "gold"},
		},
		Events: []RawEvent{
			{
				ID:        "evt_test_1",
				EventName: "landing_view",
				Timestamp: &itemTime,
				SessionID: "ses_1",
				Properties: json.RawMessage(`{"url":"https://example.com/shop?utm_source=url_utm&utm_medium=cpc"}`),
			},
			{
				ID:        "evt_test_2",
				EventName: "signup_completed",
				Timestamp: &now,
				SessionID: "ses_1",
				UserID:    "usr_99",
				Properties: json.RawMessage(`{"custom_prop":"val1"}`),
			},
		},
	}

	job := BatchJob{
		ProjectID:  10,
		ReceivedAt: now,
		Envelope:   envelope,
	}

	rows, aliases, skipped := enricher.ProcessBatch(job)
	if skipped != 0 {
		t.Fatalf("expected 0 skipped, got %d", skipped)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Row 1 checks
	r1 := rows[0]
	if r1.ActorID != "d:dev_abc" {
		t.Errorf("expected actor d:dev_abc, got %s", r1.ActorID)
	}
	// UTM was in URL query string, should be extracted
	if r1.UTMSource != "url_utm" {
		t.Errorf("expected utm_source url_utm, got %s", r1.UTMSource)
	}
	if r1.UTMMedium != "cpc" {
		t.Errorf("expected utm_medium cpc, got %s", r1.UTMMedium)
	}
	if r1.OS != "Android 15" {
		t.Errorf("expected OS Android 15, got %s", r1.OS)
	}

	// Verify user_properties were merged
	var p1 map[string]any
	_ = json.Unmarshal(r1.Properties, &p1)
	if p1["plan"] != "pro" || p1["tier"] != "gold" {
		t.Errorf("expected merged plan:pro, tier:gold in properties, got %v", p1)
	}

	// Row 2 checks
	r2 := rows[1]
	if r2.ActorID != "u:usr_99" {
		t.Errorf("expected actor u:usr_99, got %s", r2.ActorID)
	}
	// Context fallback for UTM
	if r2.UTMSource != "context_utm" {
		t.Errorf("expected context utm_source, got %s", r2.UTMSource)
	}

	// Verify identity aliases
	if len(aliases) < 2 {
		t.Fatalf("expected at least 2 aliases (device and session -> user), got %d", len(aliases))
	}
	foundDeviceAlias := false
	foundSessionAlias := false
	for _, a := range aliases {
		if a.AnonID == "d:dev_abc" && a.UserID == "u:usr_99" {
			foundDeviceAlias = true
		}
		if a.AnonID == "s:ses_1" && a.UserID == "u:usr_99" {
			foundSessionAlias = true
		}
	}
	if !foundDeviceAlias || !foundSessionAlias {
		t.Errorf("missing expected identity aliases: %+v", aliases)
	}
}

func TestFunnelEngine(t *testing.T) {
	sink := NewMemoryAnalyticsSink()
	engine := NewFunnelEngine(nil, sink)

	ctx := context.Background()
	now := time.Now()

	// Setup user who did step 1 on Monday, but timed out (> 1 day).
	// Then on Wednesday did Step 1 -> Step 2 -> Step 3 in 5 minutes!
	tMon := now.Add(-3 * 24 * time.Hour)
	tWed1 := now.Add(-1 * 24 * time.Hour)
	tWed2 := tWed1.Add(2 * time.Minute)
	tWed3 := tWed1.Add(5 * time.Minute)

	// User 1
	sink.WriteBatch(ctx, []EventRow{
		{ProjectID: 1, ActorID: "u:usr_1", EventName: "step_1", UTMSource: "google", Timestamp: tMon},
		{ProjectID: 1, ActorID: "u:usr_1", EventName: "step_1", UTMSource: "google", Timestamp: tWed1},
		{ProjectID: 1, ActorID: "u:usr_1", EventName: "extra_event", Timestamp: tWed1.Add(1 * time.Minute)},
		{ProjectID: 1, ActorID: "u:usr_1", EventName: "step_2", UTMSource: "google", Timestamp: tWed2},
		{ProjectID: 1, ActorID: "u:usr_1", EventName: "step_3", UTMSource: "google", Timestamp: tWed3},
	}, nil)

	// User 2: only did step 1 and step 2
	sink.WriteBatch(ctx, []EventRow{
		{ProjectID: 1, ActorID: "u:usr_2", EventName: "step_1", UTMSource: "facebook", Timestamp: tWed1},
		{ProjectID: 1, ActorID: "u:usr_2", EventName: "step_2", UTMSource: "facebook", Timestamp: tWed2},
	}, nil)

	// User 3: anonymous device who did step 1, then signed up at step 2
	sink.WriteBatch(ctx, []EventRow{
		{ProjectID: 1, ActorID: "d:device_anon", EventName: "step_1", UTMSource: "google", Timestamp: tWed1},
		{ProjectID: 1, ActorID: "u:usr_3", EventName: "step_2", UTMSource: "google", Timestamp: tWed2},
	}, []IdentityAlias{
		{ProjectID: 1, AnonID: "d:device_anon", UserID: "u:usr_3"},
	})

	from := now.Add(-7 * 24 * time.Hour)
	resp, err := engine.CalculateFunnel(ctx, 1, FunnelQueryRequest{
		Steps:         []string{"step_1", "step_2", "step_3"},
		WindowSeconds: 86400, // 1 day
		From:          &from,
		To:            &now,
		BreakdownBy:   "utm_source",
	})
	if err != nil {
		t.Fatalf("CalculateFunnel failed: %v", err)
	}

	if len(resp.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(resp.Steps))
	}

	// 3 users reached Step 1: usr_1, usr_2, and usr_3 (via device_anon stitched)
	if resp.Steps[0].Count != 3 {
		t.Errorf("expected 3 users at Step 1, got %d", resp.Steps[0].Count)
	}
	// All 3 reached Step 2
	if resp.Steps[1].Count != 3 {
		t.Errorf("expected 3 users at Step 2, got %d", resp.Steps[1].Count)
	}
	// Only usr_1 reached Step 3
	if resp.Steps[2].Count != 1 {
		t.Errorf("expected 1 user at Step 3, got %d", resp.Steps[2].Count)
	}

	if resp.OverallConversionPct != 33.33 {
		t.Errorf("expected ~33.33%% overall conversion, got %f", resp.OverallConversionPct)
	}

	// Check breakdown
	if len(resp.Breakdowns) == 0 {
		t.Fatal("expected breakdown results")
	}
}

func TestCohortEngine(t *testing.T) {
	sink := NewMemoryAnalyticsSink()
	engine := NewCohortEngine(nil, sink)

	ctx := context.Background()
	now := time.Now()

	// 2 weeks ago user A signed up
	sink.WriteBatch(ctx, []EventRow{
		{ProjectID: 1, ActorID: "u:user_a", EventName: "signup", Timestamp: now.Add(-14 * 24 * time.Hour)},
		{ProjectID: 1, ActorID: "u:user_a", EventName: "open_app", Timestamp: now.Add(-7 * 24 * time.Hour)},
		{ProjectID: 1, ActorID: "u:user_b", EventName: "signup", Timestamp: now.Add(-7 * 24 * time.Hour)},
	}, nil)

	from := now.Add(-30 * 24 * time.Hour)
	resp, err := engine.CalculateCohort(ctx, 1, CohortQueryRequest{
		Granularity: "week",
		Periods:     4,
		StartEvent:  "signup",
		Timezone:    "UTC",
		From:        &from,
		To:          &now,
	})
	if err != nil {
		t.Fatalf("CalculateCohort failed: %v", err)
	}

	if len(resp.Cohorts) == 0 {
		t.Fatal("expected cohorts returned")
	}
}
