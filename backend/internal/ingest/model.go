package ingest

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	// Size limits for ingress security & zip-bomb protection
	MaxCompressedBytes         = 512 * 1024      // 512 KB
	MaxUncompressedBytes       = 2 * 1024 * 1024 // 2 MB
	MaxBatchItems              = 500             // Max items per batch envelope
	MaxStringNameLen           = 255             // Max length for event name
	MaxStringMsgLen            = 4096            // Max length for log message
	MaxStringIDLen             = 128             // Max length for session/user IDs
	MaxAttributeBytes          = 32 * 1024       // 32 KB attribute threshold before sanitization/truncation
	MaxKeptAttributeValueBytes = 1024            // when truncating, top-level values up to 1 KB are kept
)

// IngestEnvelope represents the incoming payload from client SDKs.
type IngestEnvelope struct {
	SentAt  *time.Time     `json:"sent_at,omitempty"`
	SDK     SDKMeta        `json:"sdk,omitempty"`
	Context map[string]any `json:"context,omitempty"`
	Items   []RawItem      `json:"items"`
}

// SDKMeta contains metadata about the client SDK.
type SDKMeta struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

// RawItem represents a single telemetry item (event, log, or session) from client.
type RawItem struct {
	ID         string          `json:"id,omitempty"`
	Type       string          `json:"type"` // "event", "log", "session"
	Name       string          `json:"name,omitempty"`
	Level      string          `json:"level,omitempty"`
	Message    string          `json:"message,omitempty"`
	Timestamp  *time.Time      `json:"timestamp,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	UserID     string          `json:"user_id,omitempty"`
	Attributes json.RawMessage `json:"attributes,omitempty"`
}

// TelemetryRow is the normalized struct ready for high-throughput pgx.CopyFrom binary insertion.
type TelemetryRow struct {
	ID              uuid.UUID
	ProjectID       int64
	Kind            string
	Name            string
	Level           string
	Message         string
	SessionID       string
	UserID          string
	Environment     string
	ReleaseVersion  string
	ClientTimestamp *time.Time
	EventTimestamp  time.Time
	ReceivedAt      time.Time
	Attributes      []byte // valid JSON bytes
	SDKMeta         []byte // valid JSON bytes
}

// BatchJob encapsulates an unmarshaled envelope queued for async worker processing.
type BatchJob struct {
	ProjectID  int64
	ReceivedAt time.Time
	Envelope   IngestEnvelope
}
