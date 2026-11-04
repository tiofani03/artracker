package ingest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSanitizeValidationAndDefaults(t *testing.T) {
	sanitizer := NewSanitizer()
	now := time.Now()

	job := BatchJob{
		ProjectID:  123,
		ReceivedAt: now,
		Envelope: IngestEnvelope{
			Context: map[string]any{
				"environment": "staging",
				"app_version": "v1.0.0",
			},
			SDK: SDKMeta{
				Name:    "artracker-go",
				Version: "0.1.0",
			},
			Items: []RawItem{
				{
					Type:    "event",
					Name:    "button_click",
					Message: "should not have message for event",
				},
				{
					Type:    "log",
					Level:   "INVALID_LEVEL",
					Message: "some log message",
				},
				{
					Type: "INVALID_KIND",
					Name: "ignored",
				},
			},
		},
	}

	rows, dropped, _ := sanitizer.SanitizeAndNormalize(job)
	if dropped != 1 {
		t.Fatalf("expected 1 dropped item, got %d", dropped)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 valid rows, got %d", len(rows))
	}

	// First row (event)
	if rows[0].Kind != "event" || rows[0].Name != "button_click" || rows[0].Environment != "staging" {
		t.Errorf("row 0 incorrect: %+v", rows[0])
	}
	if rows[0].ID == uuid.Nil {
		t.Error("expected valid generated UUID for row 0")
	}

	// Second row (log with defaulted level)
	if rows[1].Kind != "log" || rows[1].Level != "info" {
		t.Errorf("expected level to default to 'info', got %q", rows[1].Level)
	}
}

func TestSanitizeOversizedAttributes(t *testing.T) {
	sanitizer := NewSanitizer()
	now := time.Now()

	bigMap := map[string]string{
		"big": strings.Repeat("A", MaxAttributeBytes+1000),
	}
	bigJSON, _ := json.Marshal(bigMap)

	job := BatchJob{
		ProjectID:  123,
		ReceivedAt: now,
		Envelope: IngestEnvelope{
			Items: []RawItem{
				{
					Type:       "event",
					Name:       "big_event",
					Attributes: bigJSON,
				},
			},
		},
	}

	rows, _, sanitized := sanitizer.SanitizeAndNormalize(job)
	if sanitized != 1 {
		t.Fatalf("expected 1 sanitized item, got %d", sanitized)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	var parsed map[string]any
	if err := json.Unmarshal(rows[0].Attributes, &parsed); err != nil {
		t.Fatalf("attributes should be valid json: %v", err)
	}
	if parsed["_truncated"] != true {
		t.Errorf("expected _truncated=true in attributes, got %+v", parsed)
	}
}

func TestTimestampNormalizationClockSkew(t *testing.T) {
	sanitizer := NewSanitizer()
	receivedAt := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	// Device clock is skewed 1 year into future:
	// sent_at is 2027-11-04 15:00:10
	// item timestamp is 2027-11-04 15:00:00 (10s before send)
	sentAt := time.Date(2027, 11, 4, 15, 0, 10, 0, time.UTC)
	itemTime := time.Date(2027, 11, 4, 15, 0, 0, 0, time.UTC)

	job := BatchJob{
		ProjectID:  123,
		ReceivedAt: receivedAt,
		Envelope: IngestEnvelope{
			SentAt: &sentAt,
			Items: []RawItem{
				{
					Type:      "event",
					Name:      "skewed_clock_event",
					Timestamp: &itemTime,
				},
			},
		},
	}

	rows, _, _ := sanitizer.SanitizeAndNormalize(job)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	// Normalized timestamp should be receivedAt - 10s: 2026-11-04 11:59:50
	expected := receivedAt.Add(-10 * time.Second)
	if !rows[0].EventTimestamp.Equal(expected) {
		t.Fatalf("expected normalized timestamp %v, got %v", expected, rows[0].EventTimestamp)
	}
}
