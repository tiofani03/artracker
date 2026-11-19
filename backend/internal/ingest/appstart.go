package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// AppStartItemName is the name of the perf item that carries one measured app start.
	AppStartItemName = "app_start"

	appStartWireVersion   = 1
	appStartMaxDurationMs = 60_000
	appStartMaxPhases     = 8
	appStartPhaseSumSlack = 5
	appStartMaxFirstScrn  = 255
	appStartMaxShortStr   = 64
)

// ErrInvalidAppStart is returned for any app_start item that does not satisfy the wire contract.
var ErrInvalidAppStart = errors.New("invalid app_start item")

var appStartPhaseName = regexp.MustCompile(`^[a-z_]{1,40}$`)

// AppStartPhase is one named slice of the start duration.
type AppStartPhase struct {
	Name string `json:"name"`
	Ms   int    `json:"ms"`
}

// AppStartRow is a validated app_start item ready to be stored in app_starts.
type AppStartRow struct {
	ProjectID   int64
	StartID     string
	SessionID   string
	Platform    string
	Environment string
	Type        string
	DurationMs  int
	Phases      []AppStartPhase
	FirstScreen string
	AppVersion  string
	Build       string
	OSVersion   string
	DeviceModel string
	RAMMB       *int
	Trampoline  bool
	Profiled    bool
	StartedAt   time.Time
	ReceivedAt  time.Time
}

type appStartDevice struct {
	Model     string `json:"model"`
	OSVersion string `json:"os_version"`
	RAMMB     int    `json:"ram_mb"`
}

type appStartAttributes struct {
	V           int             `json:"v"`
	Platform    string          `json:"platform"`
	StartID     string          `json:"start_id"`
	Type        string          `json:"type"`
	DurationMs  int             `json:"duration_ms"`
	Phases      []AppStartPhase `json:"phases"`
	FirstScreen string          `json:"first_screen"`
	Trampoline  bool            `json:"trampoline"`
	Profiled    bool            `json:"profiled"`
	Build       string          `json:"build"`
	Device      appStartDevice  `json:"device"`
}

// EnvelopeEnvAndVersion returns context.environment (default "production", at most 64 chars) and
// context.app_version, falling back to release_version (at most 64 chars).
func EnvelopeEnvAndVersion(ctx map[string]any) (env, version string) {
	env = "production"
	if ctx == nil {
		return env, ""
	}
	if v, ok := ctx["environment"].(string); ok && v != "" {
		env = truncateStr(v, 64)
	}
	if v, ok := ctx["app_version"].(string); ok && v != "" {
		version = truncateStr(v, 64)
	} else if v, ok := ctx["release_version"].(string); ok && v != "" {
		version = truncateStr(v, 64)
	}
	return env, version
}

// SplitVersionBuild turns the iOS SDK's "1.4.0 (217)" into ("1.4.0", "217"), splitting on the last " (" only.
// Any other string is returned unchanged with an empty build.
func SplitVersionBuild(v string) (version, build string) {
	if !strings.HasSuffix(v, ")") {
		return v, ""
	}
	i := strings.LastIndex(v, " (")
	if i <= 0 {
		return v, ""
	}
	build = v[i+2 : len(v)-1]
	if build == "" {
		return v, ""
	}
	return v[:i], build
}

// ParseAppStartItem validates an app_start perf item and normalizes it. env and appVersion come from the
// envelope context (see EnvelopeEnvAndVersion).
func ParseAppStartItem(projectID int64, receivedAt time.Time, env, appVersion string, item RawItem) (AppStartRow, error) {
	var a appStartAttributes
	if err := json.Unmarshal(item.Attributes, &a); err != nil {
		return AppStartRow{}, fmt.Errorf("%w: attributes: %v", ErrInvalidAppStart, err)
	}
	if a.V != appStartWireVersion {
		return AppStartRow{}, fmt.Errorf("%w: unsupported version %d", ErrInvalidAppStart, a.V)
	}
	if a.Platform != "android" && a.Platform != "ios" {
		return AppStartRow{}, fmt.Errorf("%w: platform %q", ErrInvalidAppStart, a.Platform)
	}
	if a.Type != "cold" && a.Type != "warm" && a.Type != "hot" {
		return AppStartRow{}, fmt.Errorf("%w: type %q", ErrInvalidAppStart, a.Type)
	}
	if a.StartID == "" || len(a.StartID) > MaxStringIDLen {
		return AppStartRow{}, fmt.Errorf("%w: start_id length %d", ErrInvalidAppStart, len(a.StartID))
	}
	if a.DurationMs < 1 || a.DurationMs > appStartMaxDurationMs {
		return AppStartRow{}, fmt.Errorf("%w: duration_ms %d out of range", ErrInvalidAppStart, a.DurationMs)
	}
	if len(a.Phases) > appStartMaxPhases {
		return AppStartRow{}, fmt.Errorf("%w: %d phases", ErrInvalidAppStart, len(a.Phases))
	}
	sum := 0
	for _, p := range a.Phases {
		if !appStartPhaseName.MatchString(p.Name) {
			return AppStartRow{}, fmt.Errorf("%w: phase name %q", ErrInvalidAppStart, p.Name)
		}
		if p.Ms < 0 {
			return AppStartRow{}, fmt.Errorf("%w: negative phase ms", ErrInvalidAppStart)
		}
		sum += p.Ms
	}
	if len(a.Phases) > 0 {
		diff := sum - a.DurationMs
		if diff < 0 {
			diff = -diff
		}
		if diff > appStartPhaseSumSlack {
			return AppStartRow{}, fmt.Errorf("%w: phases sum %d vs duration_ms %d", ErrInvalidAppStart, sum, a.DurationMs)
		}
	}

	version, splitBuild := SplitVersionBuild(appVersion)
	build := strings.TrimSpace(a.Build)
	if build == "" {
		build = splitBuild
	}

	phases := a.Phases
	if phases == nil {
		phases = []AppStartPhase{}
	}
	var ramMB *int
	if a.Device.RAMMB > 0 {
		v := a.Device.RAMMB
		ramMB = &v
	}
	startedAt := receivedAt
	if item.Timestamp != nil {
		startedAt = *item.Timestamp
	}

	return AppStartRow{
		ProjectID:   projectID,
		StartID:     a.StartID,
		SessionID:   truncateStr(strings.TrimSpace(item.SessionID), MaxStringIDLen),
		Platform:    a.Platform,
		Environment: env,
		Type:        a.Type,
		DurationMs:  a.DurationMs,
		Phases:      phases,
		FirstScreen: truncateStr(a.FirstScreen, appStartMaxFirstScrn),
		AppVersion:  version,
		Build:       truncateStr(build, appStartMaxShortStr),
		OSVersion:   truncateStr(a.Device.OSVersion, appStartMaxShortStr),
		DeviceModel: truncateStr(a.Device.Model, appStartMaxShortStr),
		RAMMB:       ramMB,
		Trampoline:  a.Trampoline,
		Profiled:    a.Profiled,
		StartedAt:   startedAt,
		ReceivedAt:  receivedAt,
	}, nil
}
