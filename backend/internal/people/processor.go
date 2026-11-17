package people

import (
	"context"

	"github.com/tiofani03/artracker/backend/internal/ingest"
)

// Processor feeds flushed telemetry rows into the people store. It implements ingest.RowProcessor.
type Processor struct{ store *Store }

func NewProcessor(s *Store) *Processor { return &Processor{store: s} }

func (p *Processor) ProcessRows(ctx context.Context, rows []ingest.TelemetryRow) error {
	if p == nil || p.store == nil || len(rows) == 0 {
		return nil
	}
	return p.store.Apply(ctx, Aggregate(rows))
}

var _ ingest.RowProcessor = (*Processor)(nil)
