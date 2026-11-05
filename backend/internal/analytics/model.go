package analytics

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	MaxBatchEvents      = 500
	MaxEventNameLen     = 255
	MaxPropertyBytes    = 32 * 1024 // 32 KB threshold
	DefaultWindowSecs   = 86400     // 1 day
	MinFunnelSteps      = 2
	MaxFunnelSteps      = 5
	DefaultCohortPeriod = 8
)

// EventEnvelope is the incoming batch envelope for POST /api/v1/events
type EventEnvelope struct {
	SentAt  *time.Time     `json:"sent_at,omitempty"`
	SDK     SDKMeta        `json:"sdk,omitempty"`
	Context map[string]any `json:"context,omitempty"`
	Events  []RawEvent     `json:"events"`
}

type SDKMeta struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type RawEvent struct {
	ID          string          `json:"id,omitempty"`
	EventName   string          `json:"event_name"`
	Timestamp   *time.Time      `json:"timestamp,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	UserID      string          `json:"user_id,omitempty"`
	DeviceID    string          `json:"device_id,omitempty"`
	UTMSource   string          `json:"utm_source,omitempty"`
	UTMMedium   string          `json:"utm_medium,omitempty"`
	UTMCampaign string          `json:"utm_campaign,omitempty"`
	Browser     string          `json:"browser,omitempty"`
	OS          string          `json:"os,omitempty"`
	Properties  json.RawMessage `json:"properties,omitempty"`
}

// EventRow is the normalized struct ready for database insertion.
type EventRow struct {
	ID          uuid.UUID
	ProjectID   int64
	SessionID   string
	UserID      string
	DeviceID    string
	ActorID     string
	EventName   string
	Properties  []byte
	UTMSource   string
	UTMMedium   string
	UTMCampaign string
	Browser     string
	OS          string
	Timestamp   time.Time
}

// IdentityAlias represents mapping from an anonymous ID to a canonical user ID.
type IdentityAlias struct {
	ProjectID int64
	AnonID    string // e.g. "d:device123" or "s:session123"
	UserID    string // e.g. "u:user123"
}

// BatchJob represents an unmarshaled envelope queued for worker processing.
type BatchJob struct {
	ProjectID  int64
	ReceivedAt time.Time
	Envelope   EventEnvelope
}

// FunnelQueryRequest defines parameters for the funnel calculation.
type FunnelQueryRequest struct {
	Steps           []string       `json:"steps"`
	From            *time.Time     `json:"from,omitempty"`
	To              *time.Time     `json:"to,omitempty"`
	WindowSeconds   int64          `json:"window_seconds,omitempty"`
	CountingMode    string         `json:"counting_mode,omitempty"` // "users" (default) or "sessions"
	BreakdownBy     string         `json:"breakdown_by,omitempty"`  // "utm_source", "browser", "os", or "prop:<key>"
	PropertyFilters map[string]any `json:"property_filters,omitempty"`
}

type FunnelStepResult struct {
	Step          int     `json:"step"`
	EventName     string  `json:"event_name"`
	Count         int64   `json:"count"`
	OverallPct    float64 `json:"overall_pct"`
	StepConvPct   float64 `json:"step_conv_pct"`
	DropOffCount  int64   `json:"drop_off_count"`
	DropOffPct    float64 `json:"drop_off_pct"`
	MedianSeconds float64 `json:"median_seconds"`
}

type FunnelBreakdown struct {
	Segment               string  `json:"segment"`
	OverallConversionPct  float64 `json:"overall_conversion_pct"`
	StepCounts            []int64 `json:"step_counts"`
}

type FunnelQueryResponse struct {
	ExecutionMs          int64              `json:"execution_ms"`
	WindowSeconds        int64              `json:"window_seconds"`
	CountingMode         string             `json:"counting_mode"`
	OverallConversionPct float64            `json:"overall_conversion_pct"`
	Steps                []FunnelStepResult `json:"steps"`
	Breakdowns           []FunnelBreakdown  `json:"breakdowns,omitempty"`
}

// Cohort retention models
type CohortQueryRequest struct {
	Granularity string     `json:"granularity"` // "day" or "week"
	Periods     int        `json:"periods"`     // e.g. 7-14 days or 4-12 weeks
	StartEvent  string     `json:"start_event"` // "" for any first seen event
	ReturnEvent string     `json:"return_event"` // "" for any active event
	Timezone    string     `json:"timezone"`    // e.g. "Asia/Jakarta" or "UTC"
	From        *time.Time `json:"from,omitempty"`
	To          *time.Time `json:"to,omitempty"`
}

type CohortCell struct {
	Period        int      `json:"period"`
	RetainedUsers int64    `json:"retained_users"`
	Percentage    *float64 `json:"percentage"` // null if is_future
	IsFuture      bool     `json:"is_future"`
}

type CohortRow struct {
	CohortLabel string       `json:"cohort_label"`
	CohortDate  time.Time    `json:"cohort_date"`
	CohortSize  int64        `json:"cohort_size"`
	Cells       []CohortCell `json:"cells"`
}

type CohortResponse struct {
	Granularity      string      `json:"granularity"`
	Timezone         string      `json:"timezone"`
	StartEvent       string      `json:"start_event"`
	ReturnEvent      string      `json:"return_event"`
	Cohorts          []CohortRow `json:"cohorts"`
	WeightedAverages []float64   `json:"weighted_averages"`
}

// Saved Funnels
type SavedFunnel struct {
	ID            int64     `json:"id"`
	ProjectID     int64     `json:"project_id"`
	Name          string    `json:"name"`
	Steps         []string  `json:"steps"`
	WindowSeconds int64     `json:"window_seconds"`
	CountingMode  string    `json:"counting_mode"`
	BreakdownBy   string    `json:"breakdown_by"`
	CreatedBy     *int64    `json:"created_by,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type SaveFunnelInput struct {
	Name          string   `json:"name"`
	Steps         []string `json:"steps"`
	WindowSeconds int64    `json:"window_seconds,omitempty"`
	CountingMode  string   `json:"counting_mode,omitempty"`
	BreakdownBy   string   `json:"breakdown_by,omitempty"`
}

// Catalog
type EventNameStat struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type CatalogResponse struct {
	EventNames       []EventNameStat `json:"event_names"`
	UTMSources       []string        `json:"utm_sources"`
	Browsers         []string        `json:"browsers"`
	OperatingSystems []string        `json:"operating_systems"`
	PropertyKeys     []string        `json:"property_keys"`
}
