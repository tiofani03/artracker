package analytics

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Enricher struct {
	dedup Deduplicator
}

func NewEnricher(dedup Deduplicator) *Enricher {
	return &Enricher{dedup: dedup}
}

// ProcessBatch normalizes, enriches, deduplicates, and extracts identity aliases from a BatchJob.
func (e *Enricher) ProcessBatch(job BatchJob) ([]EventRow, []IdentityAlias, int) {
	events := job.Envelope.Events
	if len(events) == 0 {
		return nil, nil, 0
	}

	rows := make([]EventRow, 0, len(events))
	aliases := make([]IdentityAlias, 0)
	skippedDuplicates := 0

	// Extract context-level defaults
	ctx := job.Envelope.Context
	var ctxUTMSource, ctxUTMMedium, ctxUTMCampaign, ctxBrowser, ctxOS, ctxDeviceID string
	var ctxUserProps map[string]any

	if ctx != nil {
		ctxUTMSource = getString(ctx, "utm_source")
		ctxUTMMedium = getString(ctx, "utm_medium")
		ctxUTMCampaign = getString(ctx, "utm_campaign")
		ctxBrowser = getString(ctx, "browser")
		ctxOS = getString(ctx, "os")
		ctxDeviceID = getString(ctx, "device_id")

		if props, ok := ctx["user_properties"].(map[string]any); ok {
			ctxUserProps = props
		} else if traits, ok := ctx["traits"].(map[string]any); ok {
			ctxUserProps = traits
		}
	}

	for _, item := range events {
		eventName := strings.TrimSpace(item.EventName)
		if eventName == "" {
			continue
		}
		if len(eventName) > MaxEventNameLen {
			eventName = eventName[:MaxEventNameLen]
		}

		// Layer 1 In-Memory Deduplication
		if item.ID != "" && e.dedup != nil && e.dedup.IsDuplicate(job.ProjectID, item.ID) {
			skippedDuplicates++
			continue
		}

		// Timestamp Normalization (Clock-skew correction)
		eventTimestamp := normalizeTimestamp(job.Envelope.SentAt, item.Timestamp, job.ReceivedAt)

		// Parse / Merge Properties
		propsMap := make(map[string]any)
		if len(ctxUserProps) > 0 {
			for k, v := range ctxUserProps {
				propsMap[k] = v
			}
		}

		if len(item.Properties) > 0 && string(item.Properties) != "null" {
			var parsed map[string]any
			if err := json.Unmarshal(item.Properties, &parsed); err == nil {
				for k, v := range parsed {
					propsMap[k] = v
				}
			}
		}

		// Device ID & Session ID & User ID
		deviceID := strings.TrimSpace(item.DeviceID)
		if deviceID == "" {
			deviceID = ctxDeviceID
		}
		sessionID := strings.TrimSpace(item.SessionID)
		userID := strings.TrimSpace(item.UserID)

		// Check Identity Stitching candidate
		if userID != "" {
			if deviceID != "" {
				aliases = append(aliases, IdentityAlias{
					ProjectID: job.ProjectID,
					AnonID:    "d:" + deviceID,
					UserID:    "u:" + userID,
				})
			}
			if sessionID != "" {
				aliases = append(aliases, IdentityAlias{
					ProjectID: job.ProjectID,
					AnonID:    "s:" + sessionID,
					UserID:    "u:" + userID,
				})
			}
		}

		// Determine Actor ID
		actorID := resolveActorID(userID, deviceID, sessionID)

		// UTM extraction with fallback priority & URL query parsing
		utmSource, utmMedium, utmCampaign := extractUTM(item, propsMap, ctxUTMSource, ctxUTMMedium, ctxUTMCampaign)

		// Browser & OS extraction
		browser := strings.TrimSpace(item.Browser)
		if browser == "" {
			browser = getString(propsMap, "browser")
		}
		if browser == "" {
			browser = ctxBrowser
		}

		osName := strings.TrimSpace(item.OS)
		if osName == "" {
			osName = getString(propsMap, "os")
		}
		if osName == "" {
			osName = ctxOS
		}

		// Row ID
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

		if actorID == "" {
			actorID = "e:" + rowID.String()
		}

		// Serialize properties
		var propBytes []byte
		if len(propsMap) > 0 {
			propBytes, _ = json.Marshal(propsMap)
		}
		if len(propBytes) == 0 {
			propBytes = []byte("{}")
		}
		if len(propBytes) > MaxPropertyBytes {
			replacement, _ := json.Marshal(map[string]any{
				"_truncated":      true,
				"_original_bytes": len(propBytes),
			})
			propBytes = replacement
		}

		rows = append(rows, EventRow{
			ID:          rowID,
			ProjectID:   job.ProjectID,
			SessionID:   sessionID,
			UserID:      userID,
			DeviceID:    deviceID,
			ActorID:     actorID,
			EventName:   eventName,
			Properties:  propBytes,
			UTMSource:   utmSource,
			UTMMedium:   utmMedium,
			UTMCampaign: utmCampaign,
			Browser:     browser,
			OS:          osName,
			Timestamp:   eventTimestamp,
		})
	}

	return rows, aliases, skippedDuplicates
}

func resolveActorID(userID, deviceID, sessionID string) string {
	if userID != "" {
		return "u:" + userID
	}
	if deviceID != "" {
		return "d:" + deviceID
	}
	if sessionID != "" {
		return "s:" + sessionID
	}
	return ""
}

func extractUTM(item RawEvent, props map[string]any, ctxSource, ctxMedium, ctxCampaign string) (string, string, string) {
	src := strings.TrimSpace(item.UTMSource)
	med := strings.TrimSpace(item.UTMMedium)
	cmp := strings.TrimSpace(item.UTMCampaign)

	if src == "" {
		src = getString(props, "utm_source")
	}
	if med == "" {
		med = getString(props, "utm_medium")
	}
	if cmp == "" {
		cmp = getString(props, "utm_campaign")
	}

	// Try extracting from URL or deeplink if event-level fields are still missing
	if src == "" || med == "" || cmp == "" {
		for _, urlKey := range []string{"url", "page_url", "deeplink", "link"} {
			if uStr := getString(props, urlKey); uStr != "" {
				if parsedURL, err := url.Parse(uStr); err == nil {
					q := parsedURL.Query()
					if src == "" {
						src = q.Get("utm_source")
					}
					if med == "" {
						med = q.Get("utm_medium")
					}
					if cmp == "" {
						cmp = q.Get("utm_campaign")
					}
				}
				if src != "" && med != "" && cmp != "" {
					break
				}
			}
		}
	}

	// Finally fallback to batch context defaults if still empty
	if src == "" {
		src = ctxSource
	}
	if med == "" {
		med = ctxMedium
	}
	if cmp == "" {
		cmp = ctxCampaign
	}

	return src, med, cmp
}

func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func normalizeTimestamp(sentAt, itemTimestamp *time.Time, receivedAt time.Time) time.Time {
	if sentAt != nil && itemTimestamp != nil {
		drift := sentAt.Sub(*itemTimestamp)
		if drift >= 0 && drift <= 7*24*time.Hour {
			return receivedAt.Add(-drift)
		}
	}

	if itemTimestamp != nil {
		if itemTimestamp.After(receivedAt.Add(-30*24*time.Hour)) &&
			itemTimestamp.Before(receivedAt.Add(5*time.Minute)) {
			return *itemTimestamp
		}
	}

	return receivedAt
}
