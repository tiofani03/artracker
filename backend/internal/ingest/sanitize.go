package ingest

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	validKinds = map[string]bool{
		"event":   true,
		"log":     true,
		"session": true,
	}
	validLogLevels = map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
		"fatal": true,
	}
)

type Sanitizer struct{}

func NewSanitizer() *Sanitizer {
	return &Sanitizer{}
}

// SanitizeAndNormalize transforms a BatchJob into a slice of TelemetryRow.
// Returns the rows, and count of invalid/dropped items and sanitized items.
func (s *Sanitizer) SanitizeAndNormalize(job BatchJob) ([]TelemetryRow, int, int) {
	items := job.Envelope.Items
	if len(items) == 0 {
		return nil, 0, 0
	}

	rows := make([]TelemetryRow, 0, len(items))
	droppedInvalid := 0
	sanitizedCount := 0

	// Extract envelope-level context
	envStr := "production"
	releaseVer := ""
	if job.Envelope.Context != nil {
		if envVal, ok := job.Envelope.Context["environment"].(string); ok && envVal != "" {
			envStr = truncateStr(envVal, 64)
		}
		if verVal, ok := job.Envelope.Context["app_version"].(string); ok && verVal != "" {
			releaseVer = truncateStr(verVal, 64)
		} else if verVal, ok := job.Envelope.Context["release_version"].(string); ok && verVal != "" {
			releaseVer = truncateStr(verVal, 64)
		}
	}

	// Serialize SDK meta once per batch
	var sdkMetaBytes []byte
	if job.Envelope.SDK.Name != "" || job.Envelope.SDK.Version != "" {
		sdkMetaBytes, _ = json.Marshal(job.Envelope.SDK)
	}
	if len(sdkMetaBytes) == 0 {
		sdkMetaBytes = []byte("{}")
	}

	for _, item := range items {
		kind := strings.ToLower(strings.TrimSpace(item.Type))
		if !validKinds[kind] {
			droppedInvalid++
			continue
		}

		// Level normalization
		level := strings.ToLower(strings.TrimSpace(item.Level))
		if kind == "log" {
			if !validLogLevels[level] {
				level = "info"
			}
		} else {
			level = ""
		}

		// String sanitization & truncation
		name := truncateStr(strings.TrimSpace(item.Name), MaxStringNameLen)
		message := truncateStr(strings.TrimSpace(item.Message), MaxStringMsgLen)
		sessionID := truncateStr(strings.TrimSpace(item.SessionID), MaxStringIDLen)
		userID := truncateStr(strings.TrimSpace(item.UserID), MaxStringIDLen)

		// Attribute validation & truncation
		attrs, isSanitized := s.sanitizeAttributes(item.Attributes)
		if isSanitized {
			sanitizedCount++
		}

		// Timestamp Normalization (Clock-skew correction)
		eventTimestamp := normalizeTimestamp(job.Envelope.SentAt, item.Timestamp, job.ReceivedAt)

		// UUIDv7 generation
		var rowID uuid.UUID
		if item.ID != "" {
			if parsed, err := uuid.Parse(item.ID); err == nil {
				rowID = parsed
			}
		}
		if rowID == uuid.Nil {
			var err error
			rowID, err = uuid.NewV7()
			if err != nil {
				rowID = uuid.New()
			}
		}

		rows = append(rows, TelemetryRow{
			ID:              rowID,
			ProjectID:       job.ProjectID,
			Kind:            kind,
			Name:            name,
			Level:           level,
			Message:         message,
			SessionID:       sessionID,
			UserID:          userID,
			Environment:     envStr,
			ReleaseVersion:  releaseVer,
			ClientTimestamp: item.Timestamp,
			EventTimestamp:  eventTimestamp,
			ReceivedAt:      job.ReceivedAt,
			Attributes:      attrs,
			SDKMeta:         sdkMetaBytes,
		})
	}

	return rows, droppedInvalid, sanitizedCount
}

func (s *Sanitizer) sanitizeAttributes(raw json.RawMessage) ([]byte, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return []byte("{}"), false
	}

	if len(raw) > MaxAttributeBytes {
		// Truncate oversized attributes to protect DB storage & RAM
		replacement, _ := json.Marshal(map[string]any{
			"_truncated":      true,
			"_original_bytes": len(raw),
		})
		return replacement, true
	}

	// Validate JSON syntax
	var js json.RawMessage
	if err := json.Unmarshal(raw, &js); err != nil {
		return []byte("{}"), true
	}

	return raw, false
}

func normalizeTimestamp(sentAt, itemTimestamp *time.Time, receivedAt time.Time) time.Time {
	// Case 1: Both SentAt and ItemTimestamp are provided
	if sentAt != nil && itemTimestamp != nil {
		drift := sentAt.Sub(*itemTimestamp)
		if drift >= 0 && drift <= 7*24*time.Hour {
			// Correct clock drift using server receive time minus device internal age
			return receivedAt.Add(-drift)
		}
	}

	// Case 2: Only item timestamp is provided
	if itemTimestamp != nil {
		// Accept if within realistic window: [-30 days, +5 minutes]
		if itemTimestamp.After(receivedAt.Add(-30*24*time.Hour)) &&
			itemTimestamp.Before(receivedAt.Add(5*time.Minute)) {
			return *itemTimestamp
		}
	}

	// Case 3: Missing or extreme skew -> fallback to server received time
	return receivedAt
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
