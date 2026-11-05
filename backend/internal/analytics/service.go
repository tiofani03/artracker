package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Workers         int
	QueueSize       int
	BatchSize       int
	FlushIntervalMs int
	ProjectRPS      float64
	ProjectEventsPS float64
}

type Service struct {
	pool         *pgxpool.Pool
	sink         Sink
	dedup        *MemoryDeduplicator
	enricher     *Enricher
	workerPool   *WorkerPool
	rateLimiter  *ProjectRateLimiter
	metrics      *Metrics
	funnelEngine *FunnelEngine
	cohortEngine *CohortEngine

	// In-memory saved funnels for testing / SKIP_DB
	memMu           sync.RWMutex
	memSavedFunnels map[int64][]SavedFunnel
	memFunnelIDSeq  int64
}

func NewService(cfg Config, pool *pgxpool.Pool, customSink ...Sink) *Service {
	metrics := NewMetrics(cfg.QueueSize, cfg.Workers)
	dedup := NewMemoryDeduplicator(15 * time.Minute)
	enricher := NewEnricher(dedup)
	rateLimiter := NewProjectRateLimiter(cfg.ProjectRPS, cfg.ProjectEventsPS)

	var sink Sink
	if len(customSink) > 0 && customSink[0] != nil {
		sink = customSink[0]
	} else if pool != nil {
		sink = NewPgxAnalyticsSink(pool)
	} else {
		sink = NewMemoryAnalyticsSink()
	}

	workerPool := NewWorkerPool(
		cfg.QueueSize,
		cfg.Workers,
		cfg.BatchSize,
		cfg.FlushIntervalMs,
		sink,
		enricher,
		metrics,
	)

	funnelEngine := NewFunnelEngine(pool, sink)
	cohortEngine := NewCohortEngine(pool, sink)

	return &Service{
		pool:            pool,
		sink:            sink,
		dedup:           dedup,
		enricher:        enricher,
		workerPool:      workerPool,
		rateLimiter:     rateLimiter,
		metrics:         metrics,
		funnelEngine:    funnelEngine,
		cohortEngine:    cohortEngine,
		memSavedFunnels: make(map[int64][]SavedFunnel),
	}
}

func (s *Service) WorkerPool() *WorkerPool         { return s.workerPool }
func (s *Service) RateLimiter() *ProjectRateLimiter { return s.rateLimiter }
func (s *Service) FunnelEngine() *FunnelEngine     { return s.funnelEngine }
func (s *Service) CohortEngine() *CohortEngine     { return s.cohortEngine }
func (s *Service) Metrics() *Metrics               { return s.metrics }
func (s *Service) Sink() Sink                      { return s.sink }

func (s *Service) Shutdown(ctx context.Context) error {
	s.rateLimiter.Stop()
	s.dedup.Close()
	return s.workerPool.Shutdown(ctx)
}

func (s *Service) GetCatalog(ctx context.Context, projectID int64) (*CatalogResponse, error) {
	if s.pool != nil {
		resp := &CatalogResponse{
			EventNames:       make([]EventNameStat, 0),
			UTMSources:       make([]string, 0),
			Browsers:         make([]string, 0),
			OperatingSystems: make([]string, 0),
			PropertyKeys:     make([]string, 0),
		}

		// 1. Event names
		eventRows, err := s.pool.Query(ctx, `
			SELECT event_name, count(*) 
			FROM events 
			WHERE project_id = $1 
			GROUP BY event_name 
			ORDER BY count(*) DESC 
			LIMIT 50`, projectID)
		if err == nil {
			defer eventRows.Close()
			for eventRows.Next() {
				var st EventNameStat
				if err := eventRows.Scan(&st.Name, &st.Count); err == nil {
					resp.EventNames = append(resp.EventNames, st)
				}
			}
		}

		// 2. UTM sources
		utmRows, err := s.pool.Query(ctx, `
			SELECT DISTINCT utm_source 
			FROM events 
			WHERE project_id = $1 AND utm_source <> '' 
			LIMIT 20`, projectID)
		if err == nil {
			defer utmRows.Close()
			for utmRows.Next() {
				var utm string
				if err := utmRows.Scan(&utm); err == nil {
					resp.UTMSources = append(resp.UTMSources, utm)
				}
			}
		}

		// 3. Browsers
		browserRows, err := s.pool.Query(ctx, `
			SELECT DISTINCT browser 
			FROM events 
			WHERE project_id = $1 AND browser <> '' 
			LIMIT 20`, projectID)
		if err == nil {
			defer browserRows.Close()
			for browserRows.Next() {
				var b string
				if err := browserRows.Scan(&b); err == nil {
					resp.Browsers = append(resp.Browsers, b)
				}
			}
		}

		// 4. OS
		osRows, err := s.pool.Query(ctx, `
			SELECT DISTINCT os 
			FROM events 
			WHERE project_id = $1 AND os <> '' 
			LIMIT 20`, projectID)
		if err == nil {
			defer osRows.Close()
			for osRows.Next() {
				var o string
				if err := osRows.Scan(&o); err == nil {
					resp.OperatingSystems = append(resp.OperatingSystems, o)
				}
			}
		}

		// 5. Property keys
		propRows, err := s.pool.Query(ctx, `
			SELECT DISTINCT jsonb_object_keys(properties) 
			FROM (SELECT properties FROM events WHERE project_id = $1 LIMIT 500) sub 
			LIMIT 30`, projectID)
		if err == nil {
			defer propRows.Close()
			for propRows.Next() {
				var key string
				if err := propRows.Scan(&key); err == nil && !strings.HasPrefix(key, "_") {
					resp.PropertyKeys = append(resp.PropertyKeys, key)
				}
			}
		}

		return resp, nil
	}

	// Memory fallback
	memSink, ok := s.sink.(*MemoryAnalyticsSink)
	if !ok {
		return &CatalogResponse{}, nil
	}

	eventsMap := make(map[string]int64)
	utmSet := make(map[string]bool)
	browserSet := make(map[string]bool)
	osSet := make(map[string]bool)
	propSet := make(map[string]bool)

	for _, r := range memSink.Rows() {
		if r.ProjectID == projectID {
			eventsMap[r.EventName]++
			if r.UTMSource != "" {
				utmSet[r.UTMSource] = true
			}
			if r.Browser != "" {
				browserSet[r.Browser] = true
			}
			if r.OS != "" {
				osSet[r.OS] = true
			}
			if len(r.Properties) > 0 {
				var m map[string]any
				if err := json.Unmarshal(r.Properties, &m); err == nil {
					for k := range m {
						if !strings.HasPrefix(k, "_") {
							propSet[k] = true
						}
					}
				}
			}
		}
	}

	names := make([]EventNameStat, 0, len(eventsMap))
	for n, c := range eventsMap {
		names = append(names, EventNameStat{Name: n, Count: c})
	}
	sort.Slice(names, func(i, j int) bool { return names[i].Count > names[j].Count })

	toSlice := func(m map[string]bool) []string {
		res := make([]string, 0, len(m))
		for k := range m {
			res = append(res, k)
		}
		sort.Strings(res)
		return res
	}

	return &CatalogResponse{
		EventNames:       names,
		UTMSources:       toSlice(utmSet),
		Browsers:         toSlice(browserSet),
		OperatingSystems: toSlice(osSet),
		PropertyKeys:     toSlice(propSet),
	}, nil
}

func (s *Service) ListSavedFunnels(ctx context.Context, projectID int64) ([]SavedFunnel, error) {
	if s.pool != nil {
		rows, err := s.pool.Query(ctx, `
			SELECT id, project_id, name, steps, window_seconds, counting_mode, breakdown_by, created_by, created_at, updated_at
			FROM saved_funnels
			WHERE project_id = $1
			ORDER BY updated_at DESC`, projectID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var funnels []SavedFunnel
		for rows.Next() {
			var f SavedFunnel
			var stepsBytes []byte
			if err := rows.Scan(
				&f.ID, &f.ProjectID, &f.Name, &stepsBytes,
				&f.WindowSeconds, &f.CountingMode, &f.BreakdownBy,
				&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
			); err != nil {
				return nil, err
			}
			_ = json.Unmarshal(stepsBytes, &f.Steps)
			funnels = append(funnels, f)
		}
		return funnels, rows.Err()
	}

	s.memMu.RLock()
	defer s.memMu.RUnlock()
	copied := make([]SavedFunnel, len(s.memSavedFunnels[projectID]))
	copy(copied, s.memSavedFunnels[projectID])
	return copied, nil
}

func (s *Service) CreateSavedFunnel(ctx context.Context, projectID int64, userID *int64, input SaveFunnelInput) (*SavedFunnel, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("funnel name cannot be empty")
	}
	if len(input.Steps) < MinFunnelSteps || len(input.Steps) > MaxFunnelSteps {
		return nil, fmt.Errorf("funnel must have between %d and %d steps", MinFunnelSteps, MaxFunnelSteps)
	}
	windowSecs := input.WindowSeconds
	if windowSecs <= 0 {
		windowSecs = DefaultWindowSecs
	}
	mode := strings.ToLower(strings.TrimSpace(input.CountingMode))
	if mode != "sessions" {
		mode = "users"
	}

	stepsBytes, err := json.Marshal(input.Steps)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	if s.pool != nil {
		var f SavedFunnel
		var outSteps []byte
		err := s.pool.QueryRow(ctx, `
			INSERT INTO saved_funnels (project_id, name, steps, window_seconds, counting_mode, breakdown_by, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
			RETURNING id, project_id, name, steps, window_seconds, counting_mode, breakdown_by, created_by, created_at, updated_at`,
			projectID, name, stepsBytes, windowSecs, mode, input.BreakdownBy, userID, now,
		).Scan(
			&f.ID, &f.ProjectID, &f.Name, &outSteps,
			&f.WindowSeconds, &f.CountingMode, &f.BreakdownBy,
			&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal(outSteps, &f.Steps)
		return &f, nil
	}

	s.memMu.Lock()
	defer s.memMu.Unlock()
	s.memFunnelIDSeq++
	f := SavedFunnel{
		ID:            s.memFunnelIDSeq,
		ProjectID:     projectID,
		Name:          name,
		Steps:         input.Steps,
		WindowSeconds: windowSecs,
		CountingMode:  mode,
		BreakdownBy:   input.BreakdownBy,
		CreatedBy:     userID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	s.memSavedFunnels[projectID] = append([]SavedFunnel{f}, s.memSavedFunnels[projectID]...)
	return &f, nil
}

func (s *Service) DeleteSavedFunnel(ctx context.Context, projectID int64, funnelID int64) error {
	if s.pool != nil {
		cmd, err := s.pool.Exec(ctx, `DELETE FROM saved_funnels WHERE id = $1 AND project_id = $2`, funnelID, projectID)
		if err != nil {
			return err
		}
		if cmd.RowsAffected() == 0 {
			return fmt.Errorf("funnel not found")
		}
		return nil
	}

	s.memMu.Lock()
	defer s.memMu.Unlock()
	list := s.memSavedFunnels[projectID]
	newList := make([]SavedFunnel, 0, len(list))
	found := false
	for _, f := range list {
		if f.ID == funnelID {
			found = true
			continue
		}
		newList = append(newList, f)
	}
	if !found {
		return fmt.Errorf("funnel not found")
	}
	s.memSavedFunnels[projectID] = newList
	return nil
}
