package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type FunnelEngine struct {
	pool *pgxpool.Pool
	sink Sink
}

func NewFunnelEngine(pool *pgxpool.Pool, sink Sink) *FunnelEngine {
	return &FunnelEngine{pool: pool, sink: sink}
}

type funnelAttempt struct {
	startTs           time.Time
	currentStep       int
	stepTimes         []time.Time
	firstTouchSegment string
}

type entityState struct {
	attempts []funnelAttempt
}

// CalculateFunnel computes the conversion funnel according to request specifications.
func (fe *FunnelEngine) CalculateFunnel(ctx context.Context, projectID int64, req FunnelQueryRequest) (*FunnelQueryResponse, error) {
	startTimer := time.Now()

	// 1. Validate Steps
	cleanSteps := make([]string, 0, len(req.Steps))
	for _, s := range req.Steps {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" {
			cleanSteps = append(cleanSteps, trimmed)
		}
	}
	if len(cleanSteps) < MinFunnelSteps || len(cleanSteps) > MaxFunnelSteps {
		return nil, fmt.Errorf("funnel must have between %d and %d steps, got %d", MinFunnelSteps, MaxFunnelSteps, len(cleanSteps))
	}

	windowSecs := req.WindowSeconds
	if windowSecs <= 0 {
		windowSecs = DefaultWindowSecs
	}

	countingMode := strings.ToLower(strings.TrimSpace(req.CountingMode))
	if countingMode != "sessions" {
		countingMode = "users"
	}

	now := time.Now()
	to := now
	if req.To != nil && !req.To.IsZero() {
		to = *req.To
	}
	from := to.Add(-30 * 24 * time.Hour)
	if req.From != nil && !req.From.IsZero() {
		from = *req.From
	}

	breakdownBy := strings.TrimSpace(req.BreakdownBy)

	// Fetch event rows & identity aliases
	var rows []EventRow
	var aliasMap map[string]string

	if fe.pool != nil {
		var err error
		aliasMap, err = fe.fetchIdentityAliases(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("load identity aliases: %w", err)
		}

		rows, err = fe.fetchEventRows(ctx, projectID, cleanSteps, from, to, req.PropertyFilters)
		if err != nil {
			return nil, fmt.Errorf("query funnel events: %w", err)
		}
	} else if memSink, ok := fe.sink.(*MemoryAnalyticsSink); ok {
		aliasMap = memSink.Aliases()
		for _, r := range memSink.Rows() {
			if r.ProjectID == projectID && !r.Timestamp.Before(from) && !r.Timestamp.After(to) {
				for _, stepName := range cleanSteps {
					if r.EventName == stepName {
						rows = append(rows, r)
						break
					}
				}
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].Timestamp.Before(rows[j].Timestamp)
		})
	}

	// Step mapping
	numSteps := len(cleanSteps)
	stepIndices := make(map[string][]int)
	for idx, s := range cleanSteps {
		stepIndices[s] = append(stepIndices[s], idx+1) // 1-based step
	}

	// 2. Single-Pass Stream FSM per Entity
	entities := make(map[string]*entityState)

	for _, r := range rows {
		matchingSteps := stepIndices[r.EventName]
		if len(matchingSteps) == 0 {
			continue
		}

		var entityKey string
		if countingMode == "sessions" {
			entityKey = r.SessionID
			if entityKey == "" {
				continue
			}
		} else {
			// Resolve canonical user ID from identity aliases
			canonical, exists := aliasMap[r.ActorID]
			if exists && canonical != "" {
				entityKey = canonical
			} else {
				entityKey = r.ActorID
			}
		}

		state, exists := entities[entityKey]
		if !exists {
			state = &entityState{attempts: make([]funnelAttempt, 0, 2)}
			entities[entityKey] = state
		}

		segmentVal := extractSegmentValue(r, breakdownBy)

		for _, stepNum := range matchingSteps {
			if stepNum == 1 {
				// Start a new attempt
				stepTimes := make([]time.Time, numSteps)
				stepTimes[0] = r.Timestamp
				state.attempts = append(state.attempts, funnelAttempt{
					startTs:           r.Timestamp,
					currentStep:       1,
					stepTimes:         stepTimes,
					firstTouchSegment: segmentVal,
				})
			} else {
				// Try advancing existing attempts
				for i := range state.attempts {
					att := &state.attempts[i]
					if att.currentStep == stepNum-1 {
						// Check window constraint
						if r.Timestamp.Sub(att.startTs).Seconds() <= float64(windowSecs) &&
							!r.Timestamp.Before(att.stepTimes[stepNum-2]) {
							att.currentStep = stepNum
							att.stepTimes[stepNum-1] = r.Timestamp
						}
					}
				}
			}
		}
	}

	// 3. Aggregate Results
	stepCounts := make([]int64, numSteps)
	stepDurations := make([][]float64, numSteps) // durations between step i and i-1
	breakdownCounts := make(map[string][]int64)

	for _, state := range entities {
		if len(state.attempts) == 0 {
			continue
		}

		// Find highest step reached across attempts
		maxStep := 0
		var bestSegment string
		var bestTimes []time.Time

		for _, att := range state.attempts {
			if att.currentStep > maxStep {
				maxStep = att.currentStep
				bestSegment = att.firstTouchSegment
				bestTimes = att.stepTimes
			}
		}

		if maxStep == 0 {
			continue
		}

		for s := 1; s <= maxStep; s++ {
			stepCounts[s-1]++
			if s > 1 && len(bestTimes) >= s {
				dur := bestTimes[s-1].Sub(bestTimes[s-2]).Seconds()
				if dur >= 0 {
					stepDurations[s-1] = append(stepDurations[s-1], dur)
				}
			}
		}

		if breakdownBy != "" {
			if bestSegment == "" {
				bestSegment = "(none)"
			}
			bCounts, exists := breakdownCounts[bestSegment]
			if !exists {
				bCounts = make([]int64, numSteps)
			}
			for s := 1; s <= maxStep; s++ {
				bCounts[s-1]++
			}
			breakdownCounts[bestSegment] = bCounts
		}
	}

	// 4. Construct Step Results
	results := make([]FunnelStepResult, numSteps)
	var step1Count float64
	if numSteps > 0 {
		step1Count = float64(stepCounts[0])
	}

	for i := 0; i < numSteps; i++ {
		count := stepCounts[i]
		overallPct := 0.0
		if step1Count > 0 {
			overallPct = (float64(count) / step1Count) * 100.0
		}

		stepConvPct := 100.0
		dropOffCount := int64(0)
		dropOffPct := 0.0

		if i > 0 {
			prevCount := stepCounts[i-1]
			if prevCount > 0 {
				stepConvPct = (float64(count) / float64(prevCount)) * 100.0
				dropOffCount = prevCount - count
				dropOffPct = (float64(dropOffCount) / float64(prevCount)) * 100.0
			} else {
				stepConvPct = 0
				dropOffCount = 0
				dropOffPct = 0
			}
		}

		medianDur := 0.0
		if len(stepDurations[i]) > 0 {
			medianDur = calculateMedian(stepDurations[i])
		}

		results[i] = FunnelStepResult{
			Step:          i + 1,
			EventName:     cleanSteps[i],
			Count:         count,
			OverallPct:    roundPct(overallPct),
			StepConvPct:   roundPct(stepConvPct),
			DropOffCount:  dropOffCount,
			DropOffPct:    roundPct(dropOffPct),
			MedianSeconds: roundPct(medianDur),
		}
	}

	overallConv := 0.0
	if step1Count > 0 && numSteps > 0 {
		overallConv = roundPct((float64(stepCounts[numSteps-1]) / step1Count) * 100.0)
	}

	// 5. Construct Breakdown list (Top 10 + Other)
	breakdowns := make([]FunnelBreakdown, 0)
	if breakdownBy != "" && len(breakdownCounts) > 0 {
		type segSort struct {
			seg   string
			count int64
			conv  float64
		}
		var sortedSegs []segSort
		for seg, counts := range breakdownCounts {
			s1 := float64(counts[0])
			conv := 0.0
			if s1 > 0 {
				conv = (float64(counts[numSteps-1]) / s1) * 100.0
			}
			sortedSegs = append(sortedSegs, segSort{seg: seg, count: counts[0], conv: conv})
		}
		sort.Slice(sortedSegs, func(i, j int) bool {
			return sortedSegs[i].count > sortedSegs[j].count
		})

		const topLimit = 10
		for i, ss := range sortedSegs {
			if i < topLimit {
				breakdowns = append(breakdowns, FunnelBreakdown{
					Segment:              ss.seg,
					OverallConversionPct: roundPct(ss.conv),
					StepCounts:           breakdownCounts[ss.seg],
				})
			}
		}

		if len(sortedSegs) > topLimit {
			otherCounts := make([]int64, numSteps)
			for _, ss := range sortedSegs[topLimit:] {
				c := breakdownCounts[ss.seg]
				for s := 0; s < numSteps; s++ {
					otherCounts[s] += c[s]
				}
			}
			otherConv := 0.0
			if otherCounts[0] > 0 {
				otherConv = (float64(otherCounts[numSteps-1]) / float64(otherCounts[0])) * 100.0
			}
			breakdowns = append(breakdowns, FunnelBreakdown{
				Segment:              "Other",
				OverallConversionPct: roundPct(otherConv),
				StepCounts:           otherCounts,
			})
		}
	}

	return &FunnelQueryResponse{
		ExecutionMs:          time.Since(startTimer).Milliseconds(),
		WindowSeconds:        windowSecs,
		CountingMode:         countingMode,
		OverallConversionPct: overallConv,
		Steps:                results,
		Breakdowns:           breakdowns,
	}, nil
}

func (fe *FunnelEngine) fetchIdentityAliases(ctx context.Context, projectID int64) (map[string]string, error) {
	rows, err := fe.pool.Query(ctx, `SELECT anon_id, user_id FROM identity_aliases WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	aliases := make(map[string]string)
	for rows.Next() {
		var anonID, userID string
		if err := rows.Scan(&anonID, &userID); err != nil {
			return nil, err
		}
		aliases[anonID] = userID
	}
	return aliases, rows.Err()
}

func (fe *FunnelEngine) fetchEventRows(
	ctx context.Context,
	projectID int64,
	stepNames []string,
	from, to time.Time,
	propFilters map[string]any,
) ([]EventRow, error) {
	args := []any{projectID, from, to, stepNames}
	query := `
		SELECT id, actor_id, session_id, user_id, device_id, event_name, "timestamp",
		       utm_source, utm_medium, utm_campaign, browser, os, properties
		FROM events
		WHERE project_id = $1
		  AND "timestamp" >= $2 AND "timestamp" <= $3
		  AND event_name = ANY($4)`

	if len(propFilters) > 0 {
		filterJSON, err := json.Marshal(propFilters)
		if err == nil {
			args = append(args, filterJSON)
			query += fmt.Sprintf(` AND properties @> $%d::jsonb`, len(args))
		}
	}

	query += ` ORDER BY "timestamp" ASC`

	rows, err := fe.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []EventRow
	for rows.Next() {
		var r EventRow
		r.ProjectID = projectID
		if err := rows.Scan(
			&r.ID, &r.ActorID, &r.SessionID, &r.UserID, &r.DeviceID,
			&r.EventName, &r.Timestamp,
			&r.UTMSource, &r.UTMMedium, &r.UTMCampaign,
			&r.Browser, &r.OS, &r.Properties,
		); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func extractSegmentValue(r EventRow, breakdownBy string) string {
	if breakdownBy == "" {
		return ""
	}
	switch breakdownBy {
	case "utm_source":
		if r.UTMSource != "" {
			return r.UTMSource
		}
		return "(none)"
	case "utm_medium":
		if r.UTMMedium != "" {
			return r.UTMMedium
		}
		return "(none)"
	case "utm_campaign":
		if r.UTMCampaign != "" {
			return r.UTMCampaign
		}
		return "(none)"
	case "browser":
		if r.Browser != "" {
			return r.Browser
		}
		return "Unknown"
	case "os":
		if r.OS != "" {
			return r.OS
		}
		return "Unknown"
	default:
		// Property key, e.g. "prop:plan" or "plan"
		key := strings.TrimPrefix(breakdownBy, "prop:")
		if len(r.Properties) > 0 {
			var parsed map[string]any
			if err := json.Unmarshal(r.Properties, &parsed); err == nil {
				if v, ok := parsed[key]; ok {
					return fmt.Sprintf("%v", v)
				}
			}
		}
		return "(not set)"
	}
}

func calculateMedian(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2.0
}

func roundPct(v float64) float64 {
	return float64(int(v*100+0.5)) / 100.0
}
