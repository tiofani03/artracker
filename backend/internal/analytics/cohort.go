package analytics

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CohortEngine struct {
	pool *pgxpool.Pool
	sink Sink
}

func NewCohortEngine(pool *pgxpool.Pool, sink Sink) *CohortEngine {
	return &CohortEngine{pool: pool, sink: sink}
}

type rawCohortStat struct {
	cohortPeriod time.Time
	cohortSize   int64
	periodOffset int
	retained     int64
}

func (ce *CohortEngine) CalculateCohort(ctx context.Context, projectID int64, req CohortQueryRequest) (*CohortResponse, error) {
	granularity := strings.ToLower(strings.TrimSpace(req.Granularity))
	if granularity != "day" {
		granularity = "week"
	}

	periods := req.Periods
	if periods <= 0 {
		if granularity == "day" {
			periods = 14
		} else {
			periods = DefaultCohortPeriod
		}
	}
	if periods > 30 {
		periods = 30
	}

	tzName := strings.TrimSpace(req.Timezone)
	if tzName == "" {
		tzName = "UTC"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		tzName = "UTC"
		loc = time.UTC
	}

	nowInTZ := time.Now().In(loc)
	to := nowInTZ
	if req.To != nil && !req.To.IsZero() {
		to = req.To.In(loc)
	}

	var periodDuration time.Duration
	var epochStepSeconds int64
	var periodFormat string

	if granularity == "day" {
		periodDuration = 24 * time.Hour
		epochStepSeconds = 86400
		periodFormat = "02 Jan"
	} else {
		periodDuration = 7 * 24 * time.Hour
		epochStepSeconds = 604800
		periodFormat = "02 Jan"
	}

	from := to.Add(-time.Duration(periods+1) * periodDuration)
	if req.From != nil && !req.From.IsZero() {
		from = req.From.In(loc)
	}

	startEvent := strings.TrimSpace(req.StartEvent)
	returnEvent := strings.TrimSpace(req.ReturnEvent)

	type rawCohortStatLocal struct {
		cohortPeriod time.Time
		cohortSize   int64
		periodOffset int
		retained     int64
	}

	var rawStats []rawCohortStat

	if ce.pool != nil {
		query := `
			WITH resolved_events AS (
				SELECT
					COALESCE(ia.user_id, e.actor_id) AS canonical_actor,
					e.event_name,
					date_trunc($2, e."timestamp" AT TIME ZONE $3) AS local_bucket
				FROM events e
				LEFT JOIN identity_aliases ia
				  ON ia.project_id = e.project_id AND ia.anon_id = e.actor_id
				WHERE e.project_id = $1
				  AND e."timestamp" >= $4 AND e."timestamp" < $5
			),
			cohort_base AS (
				SELECT
					canonical_actor,
					MIN(local_bucket) AS cohort_period
				FROM resolved_events
				WHERE ($6 = '' OR event_name = $6)
				GROUP BY canonical_actor
			),
			return_activity AS (
				SELECT DISTINCT
					re.canonical_actor,
					re.local_bucket AS activity_period
				FROM resolved_events re
				INNER JOIN cohort_base cb ON cb.canonical_actor = re.canonical_actor
				WHERE ($7 = '' OR re.event_name = $7)
				  AND re.local_bucket >= cb.cohort_period
			)
			SELECT
				cb.cohort_period,
				COUNT(DISTINCT cb.canonical_actor) AS cohort_size,
				ROUND(EXTRACT(EPOCH FROM (ra.activity_period - cb.cohort_period)) / $8)::int AS period_offset,
				COUNT(DISTINCT ra.canonical_actor) AS retained_users
			FROM cohort_base cb
			LEFT JOIN return_activity ra ON ra.canonical_actor = cb.canonical_actor
			GROUP BY cb.cohort_period, period_offset
			ORDER BY cb.cohort_period ASC, period_offset ASC;`

		rows, qerr := ce.pool.Query(ctx, query,
			projectID, granularity, tzName, from, to, startEvent, returnEvent, epochStepSeconds,
		)
		if qerr != nil {
			return nil, fmt.Errorf("query cohort retention: %w", qerr)
		}
		defer rows.Close()

		for rows.Next() {
			var s rawCohortStat
			if err := rows.Scan(&s.cohortPeriod, &s.cohortSize, &s.periodOffset, &s.retained); err != nil {
				return nil, err
			}
			rawStats = append(rawStats, s)
		}
	} else if memSink, ok := ce.sink.(*MemoryAnalyticsSink); ok {
		rawStats = ce.calculateMemoryCohort(memSink, projectID, granularity, loc, from, to, startEvent, returnEvent, epochStepSeconds)
	}

	// Group into rows
	cohortMap := make(map[string]*CohortRow)
	orderList := make([]string, 0)

	for _, st := range rawStats {
		key := st.cohortPeriod.Format("2006-01-02")
		row, exists := cohortMap[key]
		if !exists {
			label := st.cohortPeriod.Format(periodFormat)
			if granularity == "week" {
				label = "Wk of " + label
			}
			row = &CohortRow{
				CohortLabel: label,
				CohortDate:  st.cohortPeriod,
				CohortSize:  st.cohortSize,
				Cells:       make([]CohortCell, periods+1),
			}
			// Prepopulate default empty cells
			for p := 0; p <= periods; p++ {
				cellPeriodDate := st.cohortPeriod.Add(time.Duration(p) * periodDuration)
				isFuture := cellPeriodDate.After(nowInTZ)
				row.Cells[p] = CohortCell{
					Period:   p,
					IsFuture: isFuture,
				}
			}
			cohortMap[key] = row
			orderList = append(orderList, key)
		}

		if st.periodOffset >= 0 && st.periodOffset <= periods {
			pct := 0.0
			if row.CohortSize > 0 {
				pct = roundPct((float64(st.retained) / float64(row.CohortSize)) * 100.0)
			}
			row.Cells[st.periodOffset].RetainedUsers = st.retained
			if !row.Cells[st.periodOffset].IsFuture {
				row.Cells[st.periodOffset].Percentage = &pct
			}
		}
	}

	cohortRows := make([]CohortRow, 0, len(orderList))
	for _, k := range orderList {
		row := cohortMap[k]
		// Period 0 is always 100%
		p0 := 100.0
		row.Cells[0].RetainedUsers = row.CohortSize
		row.Cells[0].Percentage = &p0
		row.Cells[0].IsFuture = false
		cohortRows = append(cohortRows, *row)
	}

	// Calculate Weighted Averages for each period offset
	weightedAverages := make([]float64, periods+1)
	for p := 0; p <= periods; p++ {
		var totalRetained int64
		var totalEligibleSize int64

		for _, row := range cohortRows {
			cell := row.Cells[p]
			if !cell.IsFuture && row.CohortSize > 0 {
				totalRetained += cell.RetainedUsers
				totalEligibleSize += row.CohortSize
			}
		}

		if totalEligibleSize > 0 {
			weightedAverages[p] = roundPct((float64(totalRetained) / float64(totalEligibleSize)) * 100.0)
		} else if p == 0 && len(cohortRows) > 0 {
			weightedAverages[0] = 100.0
		}
	}

	return &CohortResponse{
		Granularity:      granularity,
		Timezone:         tzName,
		StartEvent:       startEvent,
		ReturnEvent:      returnEvent,
		Cohorts:          cohortRows,
		WeightedAverages: weightedAverages,
	}, nil
}

func (ce *CohortEngine) calculateMemoryCohort(
	memSink *MemoryAnalyticsSink,
	projectID int64,
	granularity string,
	loc *time.Location,
	from, to time.Time,
	startEvent, returnEvent string,
	epochStepSeconds int64,
) []rawCohortStat {
	aliasMap := memSink.Aliases()
	type eventItem struct {
		actor  string
		bucket time.Time
		name   string
	}

	var events []eventItem
	for _, r := range memSink.Rows() {
		if r.ProjectID == projectID && !r.Timestamp.Before(from) && !r.Timestamp.After(to) {
			canonical := aliasMap[r.ActorID]
			if canonical == "" {
				canonical = r.ActorID
			}
			tLocal := r.Timestamp.In(loc)
			var bucket time.Time
			if granularity == "day" {
				bucket = time.Date(tLocal.Year(), tLocal.Month(), tLocal.Day(), 0, 0, 0, 0, loc)
			} else {
				// Week starting Monday
				weekday := int(tLocal.Weekday())
				if weekday == 0 {
					weekday = 7 // Sunday
				}
				monday := tLocal.AddDate(0, 0, -weekday+1)
				bucket = time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, loc)
			}
			events = append(events, eventItem{actor: canonical, bucket: bucket, name: r.EventName})
		}
	}

	// 1. Cohort base: first bucket of start event
	cohortBase := make(map[string]time.Time)
	for _, e := range events {
		if startEvent == "" || e.name == startEvent {
			if cur, ok := cohortBase[e.actor]; !ok || e.bucket.Before(cur) {
				cohortBase[e.actor] = e.bucket
			}
		}
	}

	// 2. Count cohort sizes
	cohortSizes := make(map[time.Time]map[string]bool)
	for actor, firstB := range cohortBase {
		if cohortSizes[firstB] == nil {
			cohortSizes[firstB] = make(map[string]bool)
		}
		cohortSizes[firstB][actor] = true
	}

	// 3. Return activity
	retainedMap := make(map[time.Time]map[int]map[string]bool)
	for _, e := range events {
		firstB, inCohort := cohortBase[e.actor]
		if !inCohort {
			continue
		}
		if returnEvent != "" && e.name != returnEvent {
			continue
		}
		if !e.bucket.Before(firstB) {
			offset := int(e.bucket.Sub(firstB).Seconds() / float64(epochStepSeconds))
			if retainedMap[firstB] == nil {
				retainedMap[firstB] = make(map[int]map[string]bool)
			}
			if retainedMap[firstB][offset] == nil {
				retainedMap[firstB][offset] = make(map[string]bool)
			}
			retainedMap[firstB][offset][e.actor] = true
		}
	}

	var res []rawCohortStat

	for firstB, actors := range cohortSizes {
		size := int64(len(actors))
		offsets := retainedMap[firstB]
		for offset, retActors := range offsets {
			res = append(res, rawCohortStat{
				cohortPeriod: firstB,
				cohortSize:   size,
				periodOffset: offset,
				retained:     int64(len(retActors)),
			})
		}
	}

	sort.Slice(res, func(i, j int) bool {
		if res[i].cohortPeriod.Equal(res[j].cohortPeriod) {
			return res[i].periodOffset < res[j].periodOffset
		}
		return res[i].cohortPeriod.Before(res[j].cohortPeriod)
	})

	return res
}
