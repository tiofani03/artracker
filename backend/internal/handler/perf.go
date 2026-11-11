package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/ingest"
)

// maxPerfBatchRows bounds how many stored batches one session read may merge.
const maxPerfBatchRows = 2000

// SessionPerfResponse is the payload of GET /projects/:projectID/sessions/:sessionID/perf.
type SessionPerfResponse struct {
	// IntervalMs is the spacing between the returned points (the SDK interval, scaled up when downsampled).
	IntervalMs  int                 `json:"interval_ms"`
	Device      ingest.PerfDevice   `json:"device"`
	Metrics     []string            `json:"metrics"`
	Samples     []ingest.PerfSample `json:"samples"`
	Downsampled bool                `json:"downsampled"`
}

func emptyPerfResponse() SessionPerfResponse {
	return SessionPerfResponse{Metrics: []string{}, Samples: []ingest.PerfSample{}}
}

// GetSessionPerf handles GET /api/v1/projects/:projectID/sessions/:sessionID/perf. It merges every stored
// perf batch of the session into one deduplicated series, capped at ingest.MaxPerfResponseSamples with the
// worst value per bucket kept. A session without profiling data answers 200 with empty arrays.
func (h *Handler) GetSessionPerf(c *fiber.Ctx) error {
	projectID := c.Locals("projectID").(int64)
	sessionID := strings.TrimSpace(c.Params("sessionID"))
	if !sessionIDRegex.MatchString(sessionID) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid session_id format")
	}
	if h.db == nil {
		return c.JSON(emptyPerfResponse())
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	rows, err := h.db.Query(ctx, `
		SELECT interval_ms, device, samples
		FROM session_perf_batches
		WHERE project_id = $1 AND session_id = $2
		ORDER BY start_ts ASC
		LIMIT $3`, projectID, sessionID, maxPerfBatchRows)
	if err != nil {
		return fmt.Errorf("query perf batches: %w", err)
	}
	defer rows.Close()

	resp := emptyPerfResponse()
	var batches [][]ingest.PerfSample
	for rows.Next() {
		var (
			interval              int
			deviceRaw, samplesRaw []byte
		)
		if err := rows.Scan(&interval, &deviceRaw, &samplesRaw); err != nil {
			return fmt.Errorf("scan perf batch: %w", err)
		}
		var samples []ingest.PerfSample
		if err := json.Unmarshal(samplesRaw, &samples); err != nil {
			continue // a corrupt batch must not hide the rest of the session
		}
		batches = append(batches, samples)
		resp.IntervalMs = interval
		var device ingest.PerfDevice
		if json.Unmarshal(deviceRaw, &device) == nil {
			resp.Device = device
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("perf batch rows: %w", err)
	}

	merged := ingest.MergePerfSamples(batches)
	resp.Metrics = ingest.PresentPerfMetrics(merged)
	resp.Samples, resp.Downsampled = ingest.DownsamplePerf(merged, ingest.MaxPerfResponseSamples)
	if resp.Downsampled {
		resp.IntervalMs = ingest.EffectivePerfInterval(resp.IntervalMs, len(merged), ingest.MaxPerfResponseSamples)
	}
	if resp.Samples == nil {
		resp.Samples = []ingest.PerfSample{}
	}
	return c.JSON(resp)
}
