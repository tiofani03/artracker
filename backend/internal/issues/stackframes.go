package issues

import (
	"regexp"
	"strings"
)

// Go twin of frontend/src/issues/stackTrace.ts; keep rules in sync.

// StackFrame is one line of a stack trace, split for display.
type StackFrame struct {
	Raw      string
	Symbol   string
	Location string
	// App is true for the app's own code; false for framework / runtime / library frames.
	App bool
}

// StackSection is the thrown exception or one of its "Caused by" causes, with its frames.
type StackSection struct {
	Title    string
	CausedBy bool
	Frames   []StackFrame
}

// FrameGroup is either a single app frame or a run of library frames (exactly one set).
type FrameGroup struct {
	App     *StackFrame
	Library []StackFrame
}

var libraryPrefixes = []string{
	"android.", "androidx.", "com.android.", "com.google.android.", "dalvik.",
	"java.", "javax.", "jdk.", "kotlin.", "kotlinx.", "sun.", "libcore.",
	"okhttp3.", "retrofit2.", "io.reactivex.",
}

var libraryPatterns = []*regexp.Regexp{
	regexp.MustCompile(`node_modules/`),
	regexp.MustCompile(`^\.\.\. \d+ more$`),
	regexp.MustCompile(`^<anonymous>|^native$`),
	regexp.MustCompile(`\b(UIKitCore|UIKit|CoreFoundation|Foundation|libdispatch\.dylib|libsystem_\w+\.dylib|GraphicsServices|libdyld\.dylib)\b`),
}

var (
	causedByRe  = regexp.MustCompile(`^Caused by:\s*`)
	atPrefixRe  = regexp.MustCompile(`^at\s+`)
	atLineRe    = regexp.MustCompile(`^at\s`)
	moreLineRe  = regexp.MustCompile(`^\.\.\. \d+ more$`)
	jvmJSFrameR = regexp.MustCompile(`^(.*?)\s*\(([^()]*)\)$`)
)

func isLibrary(symbol, line string) bool {
	for _, p := range libraryPrefixes {
		if strings.HasPrefix(symbol, p) {
			return true
		}
	}
	for _, re := range libraryPatterns {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func parseFrame(line string) StackFrame {
	text := atPrefixRe.ReplaceAllString(strings.TrimSpace(line), "")
	symbol, location := text, ""
	if m := jvmJSFrameR.FindStringSubmatch(text); m != nil {
		symbol = strings.TrimSpace(m[1])
		location = m[2]
	}
	display := symbol
	if display == "" {
		display = location
	}
	return StackFrame{Raw: line, Symbol: display, Location: location, App: !isLibrary(symbol, text)}
}

// ParseStackTrace splits a raw stack trace into the exception and its causes, each with frames.
func ParseStackTrace(trace string) []StackSection {
	sections := []StackSection{}
	for _, line := range strings.Split(trace, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		isFrame := atLineRe.MatchString(trimmed) || moreLineRe.MatchString(trimmed)
		caused := causedByRe.MatchString(trimmed)
		switch {
		case len(sections) == 0 || (!isFrame && caused):
			sections = append(sections, StackSection{
				Title:    causedByRe.ReplaceAllString(trimmed, ""),
				CausedBy: caused,
				Frames:   []StackFrame{},
			})
		case isFrame:
			cur := &sections[len(sections)-1]
			cur.Frames = append(cur.Frames, parseFrame(line))
		default:
			// Multi-line exception message: keep it with the title.
			cur := &sections[len(sections)-1]
			if len(cur.Frames) == 0 {
				cur.Title += "\n" + trimmed
			} else {
				cur.Frames = append(cur.Frames, parseFrame(line))
			}
		}
	}
	return sections
}

// GroupFrames keeps app frames individual and collapses each run of library frames.
func GroupFrames(frames []StackFrame) []FrameGroup {
	groups := []FrameGroup{}
	for i := range frames {
		f := frames[i]
		if f.App {
			groups = append(groups, FrameGroup{App: &f})
			continue
		}
		if n := len(groups); n > 0 && groups[n-1].App == nil {
			groups[n-1].Library = append(groups[n-1].Library, f)
		} else {
			groups = append(groups, FrameGroup{Library: []StackFrame{f}})
		}
	}
	return groups
}
