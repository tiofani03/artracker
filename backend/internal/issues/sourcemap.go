package issues

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// SourceMap is a parsed Source Map v3 document. It maps generated (minified)
// positions back to their original source locations.
type SourceMap struct {
	Version        int      `json:"version"`
	File           string   `json:"file"`
	SourceRoot     string   `json:"sourceRoot"`
	Sources        []string `json:"sources"`
	Names          []string `json:"names"`
	Mappings       string   `json:"mappings"`
	SourcesContent []string `json:"sourcesContent"`

	lines []mappingLine
}

type segment struct {
	generatedColumn int
	sourceIndex     int // -1 when the segment carries no source mapping
	sourceLine      int // 0-based
	sourceColumn    int // 0-based
	nameIndex       int // -1 when absent
}

type mappingLine struct {
	segments []segment
}

// Position is a resolved location in an original source file.
type Position struct {
	Source string
	Line   int // 1-based
	Column int // 1-based
	Name   string
	Found  bool
}

// ParseSourceMap decodes a Source Map v3 document and pre-computes its mappings.
func ParseSourceMap(data []byte) (*SourceMap, error) {
	var sm SourceMap
	if err := json.Unmarshal(data, &sm); err != nil {
		return nil, fmt.Errorf("parse source map: %w", err)
	}
	if sm.Version != 3 {
		return nil, fmt.Errorf("unsupported source map version %d", sm.Version)
	}
	if err := sm.decodeMappings(); err != nil {
		return nil, err
	}
	return &sm, nil
}

// decodeMappings expands the VLQ-encoded "mappings" string into per-line
// segments. All fields are stored as deltas from the previous segment (source,
// source-line and source-column persist across lines; generated-column resets).
func (sm *SourceMap) decodeMappings() error {
	var prevSource, prevSourceLine, prevSourceColumn, prevName int

	for _, line := range strings.Split(sm.Mappings, ";") {
		var segs []segment
		prevGenColumn := 0
		for _, raw := range strings.Split(line, ",") {
			if raw == "" {
				continue
			}
			pos := 0

			genCol, next, ok := decodeVLQ(raw, pos)
			if !ok {
				continue
			}
			pos = next
			seg := segment{generatedColumn: prevGenColumn + genCol, sourceIndex: -1, nameIndex: -1}
			prevGenColumn = seg.generatedColumn

			if pos < len(raw) {
				srcIdx, n, ok := decodeVLQ(raw, pos)
				if !ok {
					continue
				}
				pos = n
				seg.sourceIndex = prevSource + srcIdx
				prevSource = seg.sourceIndex

				srcLine, n, ok := decodeVLQ(raw, pos)
				if !ok {
					continue
				}
				pos = n
				seg.sourceLine = prevSourceLine + srcLine
				prevSourceLine = seg.sourceLine

				srcCol, n, ok := decodeVLQ(raw, pos)
				if !ok {
					continue
				}
				pos = n
				seg.sourceColumn = prevSourceColumn + srcCol
				prevSourceColumn = seg.sourceColumn

				if pos < len(raw) {
					nameIdx, _, ok := decodeVLQ(raw, pos)
					if ok {
						seg.nameIndex = prevName + nameIdx
						prevName = seg.nameIndex
					}
				}
			}
			segs = append(segs, seg)
		}
		sm.lines = append(sm.lines, mappingLine{segments: segs})
	}
	return nil
}

// decodeVLQ decodes a single base64 VLQ value starting at pos and returns the
// value plus the index of the next character.
func decodeVLQ(s string, pos int) (value int, next int, ok bool) {
	shift := 0
	for {
		if pos >= len(s) {
			return 0, pos, false
		}
		digit := strings.IndexByte(base64Chars, s[pos])
		if digit < 0 {
			return 0, pos, false
		}
		pos++
		continuation := digit & 32
		digit &= 31
		value += digit << shift
		if continuation == 0 {
			break
		}
		shift += 5
	}

	// The lowest bit is the sign bit; the value itself is shifted right by one.
	if value&1 == 1 {
		value = -(value >> 1)
	} else {
		value >>= 1
	}
	return value, pos, true
}

// OriginalPosition resolves a generated (minified) 1-based line/column to the
// corresponding original source location.
func (sm *SourceMap) OriginalPosition(genLine, genColumn int) Position {
	li := genLine - 1
	if li < 0 || li >= len(sm.lines) {
		return Position{}
	}

	segs := sm.lines[li].segments
	if len(segs) == 0 {
		return Position{}
	}

	target := genColumn - 1

	// Binary search for the greatest segment with generatedColumn <= target.
	lo, hi := 0, len(segs)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if segs[mid].generatedColumn <= target {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	seg := segs[lo]
	if seg.generatedColumn > target || seg.sourceIndex < 0 || seg.sourceIndex >= len(sm.Sources) {
		return Position{}
	}

	pos := Position{
		Source: sm.Sources[seg.sourceIndex],
		Line:   seg.sourceLine + 1,
		Column: seg.sourceColumn + 1,
		Found:  true,
	}
	if seg.nameIndex >= 0 && seg.nameIndex < len(sm.Names) {
		pos.Name = sm.Names[seg.nameIndex]
	}
	return pos
}

// DemangleFrame rewrites a single stack frame, replacing occurrences of
// "<sm.File>:<line>:<col>" with the original "<source>:<line>:<col>".
func (sm *SourceMap) DemangleFrame(frame string) string {
	if sm.File == "" {
		return frame
	}
	re := regexp.MustCompile(regexp.QuoteMeta(sm.File) + `:(\d+):(\d+)`)
	return re.ReplaceAllStringFunc(frame, func(match string) string {
		sub := re.FindStringSubmatch(match)
		if sub == nil {
			return match
		}
		line, _ := strconv.Atoi(sub[1])
		col, _ := strconv.Atoi(sub[2])
		pos := sm.OriginalPosition(line, col)
		if !pos.Found {
			return match
		}
		return fmt.Sprintf("%s:%d:%d", pos.Source, pos.Line, pos.Column)
	})
}

// DemangleStackTrace demangles every frame in a multi-line stack trace.
func (sm *SourceMap) DemangleStackTrace(stackTrace string) string {
	if sm.File == "" || stackTrace == "" {
		return stackTrace
	}
	lines := strings.Split(stackTrace, "\n")
	for i, line := range lines {
		lines[i] = sm.DemangleFrame(line)
	}
	return strings.Join(lines, "\n")
}
