package ingest

import "context"

// IssueProcessor optionally receives normalized telemetry rows after a
// successful flush so the error-tracking subsystem can group repeated crashes
// into a single issue. Implementations must be safe for concurrent use.
type IssueProcessor interface {
	ProcessRows(ctx context.Context, rows []TelemetryRow) error
}
