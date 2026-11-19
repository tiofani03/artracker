package appstart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tiofani03/artracker/backend/internal/ingest"
	"github.com/tiofani03/artracker/backend/internal/symbolication"
)

type androidResolver struct {
	retracer    *symbolication.ProguardRetracer
	appPackages []string
}

// NewAndroidResolver resolves Android frames with the R8 mapping r (nil means no mapping was found).
// A frame is an app frame when its class is under one of appPackages.
func NewAndroidResolver(r *symbolication.ProguardRetracer, appPackages []string) Resolver {
	return &androidResolver{retracer: r, appPackages: appPackages}
}

func (a *androidResolver) MissingSymbols() bool { return a.retracer == nil }

func (a *androidResolver) isApp(class string) bool {
	for _, pkg := range a.appPackages {
		if strings.HasPrefix(class, pkg+".") {
			return true
		}
	}
	return false
}

// Resolve ignores caller: Android frames carry exact line numbers.
func (a *androidResolver) Resolve(f ingest.ProfileFrame, _ bool) []ResolvedFrame {
	line := 0
	if f.L != nil {
		line = *f.L
	}
	if retraced, ok := a.retracer.RetraceFrame(f.C, f.M, f.F, line); ok && len(retraced) > 0 {
		out := make([]ResolvedFrame, 0, len(retraced))
		for _, rf := range retraced {
			out = append(out, ResolvedFrame{
				Function: rf.Class + "." + rf.Method,
				File:     rf.File,
				Line:     rf.Line,
				IsApp:    a.isApp(rf.Class),
				Resolved: true,
			})
		}
		return out
	}
	return []ResolvedFrame{{
		Function: f.C + "." + f.M,
		File:     f.F,
		Line:     line,
		IsApp:    a.isApp(f.C),
	}}
}

type iosResolver struct {
	images  []ingest.ProfileImage
	keys    []string // dashed upper-case UUID per image; "" when it is not a valid UUID
	loads   []uint64
	loadOK  []bool
	syms    map[string]*symbolication.IOSSymbolicator
	missing bool
}

// NewIOSResolver resolves iOS frames. syms is keyed by dashed upper-case image UUID; a nil or absent
// value means the dSYM for that image is missing.
func NewIOSResolver(images []ingest.ProfileImage, syms map[string]*symbolication.IOSSymbolicator) Resolver {
	r := &iosResolver{
		images: images,
		keys:   make([]string, len(images)),
		loads:  make([]uint64, len(images)),
		loadOK: make([]bool, len(images)),
		syms:   syms,
	}
	for i, img := range images {
		if uuids := (ingest.AppStartProfile{Images: []ingest.ProfileImage{img}}).ImageUUIDs(); len(uuids) == 1 {
			r.keys[i] = uuids[0]
		}
		r.loads[i], r.loadOK[i] = parseHexAddr(img.Load)
		if img.App && r.syms[r.keys[i]] == nil {
			r.missing = true
		}
	}
	return r
}

func (r *iosResolver) MissingSymbols() bool { return r.missing }

func parseHexAddr(s string) (uint64, bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	v, err := strconv.ParseUint(s, 16, 64)
	return v, err == nil
}

// Resolve looks up pc-1 for caller frames: the SDK sends raw return addresses for non-leaf frames, and
// the return address belongs to the instruction after the call.
func (r *iosResolver) Resolve(f ingest.ProfileFrame, caller bool) []ResolvedFrame {
	pc, pcOK := parseHexAddr(f.A)
	if f.I == nil || *f.I < 0 || *f.I >= len(r.images) || !pcOK {
		return []ResolvedFrame{unplacedFrame(f)}
	}
	img := r.images[*f.I]

	if sym := r.syms[r.keys[*f.I]]; sym != nil && r.loadOK[*f.I] {
		lookup := pc
		if caller && lookup > 0 {
			lookup--
		}
		if loc := sym.SymbolicateImageAddress(lookup, r.loads[*f.I]); loc.Found && loc.Function != "" {
			return []ResolvedFrame{{Function: loc.Function, File: loc.File, Line: loc.Line, IsApp: img.App, Resolved: true}}
		}
	}
	if f.Sym != "" {
		return []ResolvedFrame{{Function: f.Sym, IsApp: img.App, Resolved: true}}
	}
	offset := pc
	if r.loadOK[*f.I] {
		offset = pc - r.loads[*f.I]
	}
	return []ResolvedFrame{{Function: fmt.Sprintf("%s+0x%x", img.Name, offset), IsApp: img.App}}
}

// unplacedFrame resolves a frame that names no usable image or address from its device symbol alone.
func unplacedFrame(f ingest.ProfileFrame) ResolvedFrame {
	if f.Sym != "" {
		return ResolvedFrame{Function: f.Sym, Resolved: true}
	}
	return ResolvedFrame{Function: "<unknown>"}
}
