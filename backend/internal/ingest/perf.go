package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	// PerfItemType is the item type SDKs use for profiling sample batches (see session-profiling-design §3).
	PerfItemType = "perf"
	// MaxPerfSamples is the most samples one perf item may carry.
	MaxPerfSamples = 60

	perfWireVersion   = 1
	perfMinIntervalMs = 500
	perfMaxIntervalMs = 10_000
	perfMaxMemMB      = 1_048_576
	perfMaxFPS        = 240
	perfMaxStorageMB  = 100_000_000
)

// ErrInvalidPerf is returned for any perf item that does not satisfy the wire contract.
var ErrInvalidPerf = errors.New("invalid perf item")

// PerfSample is one profiling reading. Every metric except T is optional; an absent metric means "not measured".
type PerfSample struct {
	T       int64    `json:"t"`
	CPU     *float64 `json:"cpu,omitempty"`
	MemMB   *float64 `json:"mem_mb,omitempty"`
	FPS     *float64 `json:"fps,omitempty"`
	Jank    *int     `json:"jank,omitempty"`
	Frozen  *int     `json:"frozen,omitempty"`
	Threads *int     `json:"threads,omitempty"`
	Battery *float64 `json:"battery,omitempty"`
	Thermal string   `json:"thermal,omitempty"`
	// RAMFreeMB is the device's free RAM and StorageFreeMB its free disk space, both in MB.
	RAMFreeMB     *float64 `json:"ram_free_mb,omitempty"`
	StorageFreeMB *float64 `json:"storage_free_mb,omitempty"`
	// Net is the active connection: wifi, cellular, ethernet or none.
	Net string `json:"net,omitempty"`
}

// PerfDevice describes the device the samples came from.
type PerfDevice struct {
	RAMMB     int     `json:"ram_mb,omitempty"`
	Cores     int     `json:"cores,omitempty"`
	RefreshHz float64 `json:"refresh_hz,omitempty"`
	// OSVersion is the OS release string (e.g. "17.4", "14"); StorageMB the total disk size in MB.
	OSVersion string `json:"os_version,omitempty"`
	StorageMB int    `json:"storage_mb,omitempty"`
}

// PerfBatchRow is a validated perf item ready to be stored in session_perf_batches.
type PerfBatchRow struct {
	ProjectID  int64
	SessionID  string
	Platform   string
	StartTS    time.Time
	EndTS      time.Time
	IntervalMs int
	Device     PerfDevice
	Samples    []PerfSample
	ReceivedAt time.Time
}

type perfAttributes struct {
	V          int          `json:"v"`
	Platform   string       `json:"platform"`
	IntervalMs int          `json:"interval_ms"`
	Device     PerfDevice   `json:"device"`
	Samples    []PerfSample `json:"samples"`
}

var validThermal = map[string]bool{"nominal": true, "fair": true, "serious": true, "critical": true}

var validNet = map[string]bool{"wifi": true, "cellular": true, "ethernet": true, "none": true}

const perfMaxOSVersionLen = 32

// ParsePerfItem validates a perf item and normalizes it: samples sorted by T, invalid samples dropped,
// values clamped, unknown keys discarded (the typed structs only re-marshal known fields).
func ParsePerfItem(projectID int64, receivedAt time.Time, item RawItem) (PerfBatchRow, error) {
	sessionID := truncateStr(strings.TrimSpace(item.SessionID), MaxStringIDLen)
	if sessionID == "" {
		return PerfBatchRow{}, fmt.Errorf("%w: missing session_id", ErrInvalidPerf)
	}

	var attrs perfAttributes
	if err := json.Unmarshal(item.Attributes, &attrs); err != nil {
		return PerfBatchRow{}, fmt.Errorf("%w: attributes: %v", ErrInvalidPerf, err)
	}
	if attrs.V != perfWireVersion {
		return PerfBatchRow{}, fmt.Errorf("%w: unsupported version %d", ErrInvalidPerf, attrs.V)
	}
	if attrs.IntervalMs < perfMinIntervalMs || attrs.IntervalMs > perfMaxIntervalMs {
		return PerfBatchRow{}, fmt.Errorf("%w: interval_ms %d out of range", ErrInvalidPerf, attrs.IntervalMs)
	}
	if len(attrs.Samples) == 0 || len(attrs.Samples) > MaxPerfSamples {
		return PerfBatchRow{}, fmt.Errorf("%w: sample count %d", ErrInvalidPerf, len(attrs.Samples))
	}

	samples := make([]PerfSample, 0, len(attrs.Samples))
	for _, s := range attrs.Samples {
		if s.T <= 0 {
			continue
		}
		s.CPU = clampFloat(s.CPU, 0, 100)
		s.MemMB = clampFloat(s.MemMB, 0, perfMaxMemMB)
		s.FPS = clampFloat(s.FPS, 0, perfMaxFPS)
		s.Battery = clampFloat(s.Battery, 0, 1)
		s.RAMFreeMB = clampFloat(s.RAMFreeMB, 0, perfMaxMemMB)
		s.StorageFreeMB = clampFloat(s.StorageFreeMB, 0, perfMaxStorageMB)
		if !validNet[s.Net] {
			s.Net = ""
		}
		s.Jank = clampInt(s.Jank)
		s.Frozen = clampInt(s.Frozen)
		s.Threads = clampInt(s.Threads)
		if !validThermal[s.Thermal] {
			s.Thermal = ""
		}
		samples = append(samples, s)
	}
	if len(samples) == 0 {
		return PerfBatchRow{}, fmt.Errorf("%w: no sample has a positive t", ErrInvalidPerf)
	}
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].T < samples[j].T })

	attrs.Device.OSVersion = truncateStr(strings.TrimSpace(attrs.Device.OSVersion), perfMaxOSVersionLen)
	if attrs.Device.RAMMB < 0 {
		attrs.Device.RAMMB = 0
	}
	if attrs.Device.StorageMB < 0 {
		attrs.Device.StorageMB = 0
	}

	platform := attrs.Platform
	if platform != "android" && platform != "ios" {
		platform = ""
	}

	return PerfBatchRow{
		ProjectID:  projectID,
		SessionID:  sessionID,
		Platform:   platform,
		StartTS:    time.UnixMilli(samples[0].T).UTC(),
		EndTS:      time.UnixMilli(samples[len(samples)-1].T).UTC(),
		IntervalMs: attrs.IntervalMs,
		Device:     attrs.Device,
		Samples:    samples,
		ReceivedAt: receivedAt,
	}, nil
}

func clampFloat(v *float64, lo, hi float64) *float64 {
	if v == nil {
		return nil
	}
	c := *v
	if c != c { // NaN
		return nil
	}
	if c < lo {
		c = lo
	}
	if c > hi {
		c = hi
	}
	return &c
}

func clampInt(v *int) *int {
	if v == nil {
		return nil
	}
	c := *v
	if c < 0 {
		c = 0
	}
	return &c
}
