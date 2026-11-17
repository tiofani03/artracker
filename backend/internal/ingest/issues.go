package ingest

import "context"

// RowProcessor receives normalized telemetry rows after a successful flush (issue grouping, people profiles, …).
// Implementations must be safe for concurrent use. An error is logged and never fails the flush.
type RowProcessor interface {
	ProcessRows(ctx context.Context, rows []TelemetryRow) error
}

// IssueProcessor is the original name of RowProcessor, kept for existing callers.
type IssueProcessor = RowProcessor
