package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// MaxProfileCompressedBytes and MaxProfileUncompressedBytes bound one profile upload on the wire and after gunzip.
	MaxProfileCompressedBytes   = 1 << 20
	MaxProfileUncompressedBytes = 4 << 20
	MaxProfileSamples           = 1000
	MaxProfileStackDepth        = 128
	MaxProfileStacks            = 1000
	MaxProfileFrames            = 20000
	MaxProfileImages            = 512

	profileWireVersion   = 1
	profileMaxIntervalMs = 100
	profileMaxSampleT    = 60_000
	profileMaxPackages   = 20
	profileMaxShortStr   = 128
)

// ErrInvalidProfile is returned for any app start profile that does not satisfy the wire contract.
var ErrInvalidProfile = errors.New("invalid app start profile")

var profileHexAddr = regexp.MustCompile(`^(0[xX])?[0-9a-fA-F]{1,16}$`)

// ProfileFrame is one unique frame. Android uses C/M/L/F; iOS uses A/I/Sym.
type ProfileFrame struct {
	C string `json:"c,omitempty"`
	M string `json:"m,omitempty"`
	L *int   `json:"l,omitempty"`
	F string `json:"f,omitempty"` // android

	A   string `json:"a,omitempty"`
	I   *int   `json:"i,omitempty"`
	Sym string `json:"sym,omitempty"` // ios
}

// ProfileImage is one loaded binary image referenced by iOS frames.
type ProfileImage struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Load string `json:"load"`
	App  bool   `json:"app"`
}

// ProfileSample is one stack sample: T ms since the start anchor, S the stack index.
type ProfileSample struct {
	T int64 `json:"t"`
	S int   `json:"s"`
}

// AppStartProfile is the sampled startup profile (spec §5.2).
type AppStartProfile struct {
	V           int             `json:"v"`
	StartID     string          `json:"start_id"`
	Platform    string          `json:"platform"`
	AppPackages []string        `json:"app_packages,omitempty"`
	IntervalMs  int             `json:"interval_ms"`
	T0          string          `json:"t0"`
	Frames      []ProfileFrame  `json:"frames"`
	Images      []ProfileImage  `json:"images,omitempty"`
	Stacks      [][]int         `json:"stacks"`
	Samples     []ProfileSample `json:"samples"`
}

// AppStartProfileUpload is the body of POST /app-start/profiles.
type AppStartProfileUpload struct {
	SDK     SDKMeta         `json:"sdk"`
	Context map[string]any  `json:"context"`
	Profile AppStartProfile `json:"profile"`
}

// CleanStoredString makes s safe for a Postgres text column: invalid UTF-8 and NUL bytes are removed and
// the result is cut to at most maxRunes characters without splitting one.
func CleanStoredString(s string, maxRunes int) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\x00", "")
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes])
}

func invalidProfile(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidProfile, fmt.Sprintf(format, args...))
}

// ParseAppStartProfile decodes and validates an uncompressed profile upload.
func ParseAppStartProfile(raw []byte) (AppStartProfileUpload, error) {
	var up AppStartProfileUpload
	if err := json.Unmarshal(raw, &up); err != nil {
		return AppStartProfileUpload{}, invalidProfile("json: %v", err)
	}
	if err := up.Profile.validate(); err != nil {
		return AppStartProfileUpload{}, err
	}
	return up, nil
}

func (p AppStartProfile) validate() error {
	if p.V != profileWireVersion {
		return invalidProfile("unsupported version %d", p.V)
	}
	if id := CleanStoredString(p.StartID, MaxStringIDLen+1); id == "" || utf8.RuneCountInString(id) > MaxStringIDLen {
		return invalidProfile("start_id length")
	}
	if p.Platform != "android" && p.Platform != "ios" {
		return invalidProfile("platform %q", p.Platform)
	}
	if p.IntervalMs < 1 || p.IntervalMs > profileMaxIntervalMs {
		return invalidProfile("interval_ms %d out of range", p.IntervalMs)
	}
	if len(p.Samples) > MaxProfileSamples {
		return invalidProfile("%d samples", len(p.Samples))
	}
	if len(p.Stacks) > MaxProfileStacks {
		return invalidProfile("%d stacks", len(p.Stacks))
	}
	if len(p.Frames) > MaxProfileFrames {
		return invalidProfile("%d frames", len(p.Frames))
	}
	if len(p.Images) > MaxProfileImages {
		return invalidProfile("%d images", len(p.Images))
	}
	if len(p.AppPackages) > profileMaxPackages {
		return invalidProfile("%d app_packages", len(p.AppPackages))
	}
	for _, pkg := range p.AppPackages {
		if utf8.RuneCountInString(pkg) > profileMaxShortStr {
			return invalidProfile("app_packages entry too long")
		}
	}

	for i, f := range p.Frames {
		if p.Platform == "ios" {
			if f.I == nil || *f.I < 0 || *f.I >= len(p.Images) {
				return invalidProfile("frame %d image index", i)
			}
			if !profileHexAddr.MatchString(f.A) {
				return invalidProfile("frame %d address", i)
			}
		} else if f.C == "" || f.M == "" {
			return invalidProfile("frame %d class or method empty", i)
		}
	}
	for i, st := range p.Stacks {
		if len(st) > MaxProfileStackDepth {
			return invalidProfile("stack %d depth %d", i, len(st))
		}
		for _, fi := range st {
			if fi < 0 || fi >= len(p.Frames) {
				return invalidProfile("stack %d frame index %d", i, fi)
			}
		}
	}
	var prev int64
	for i, s := range p.Samples {
		if s.T < 0 || s.T > profileMaxSampleT {
			return invalidProfile("sample %d t %d out of range", i, s.T)
		}
		if s.T < prev {
			return invalidProfile("sample %d out of order", i)
		}
		prev = s.T
		if s.S < 0 || s.S >= len(p.Stacks) {
			return invalidProfile("sample %d stack index %d", i, s.S)
		}
	}
	return nil
}

// ImageUUIDs returns the distinct image UUIDs in upper-case dashed form (8-4-4-4-12). Entries that are not
// 32 hex digits (dashes ignored) cannot match a debug file and are skipped.
func (p AppStartProfile) ImageUUIDs() []string {
	out := make([]string, 0, len(p.Images))
	seen := make(map[string]struct{}, len(p.Images))
	for _, img := range p.Images {
		u, ok := normalizeImageUUID(img.UUID)
		if !ok {
			continue
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

func normalizeImageUUID(s string) (string, bool) {
	h := strings.ToUpper(strings.ReplaceAll(s, "-", ""))
	if len(h) != 32 {
		return "", false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if (c < '0' || c > '9') && (c < 'A' || c > 'F') {
			return "", false
		}
	}
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], true
}
