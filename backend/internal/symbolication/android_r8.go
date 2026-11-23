package symbolication

import (
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ProguardMethodMapping maps an obfuscated method name and line number range to the original method.
type ProguardMethodMapping struct {
	OriginalName string
	StartLine    int
	EndLine      int
	TargetLine   int
	// OrigStart/OrigEnd are the original source line range (":10:12" after the signature).
	OrigStart int
	OrigEnd   int
	// OrigClass is set when the original name is qualified (an inlined method
	// from another class), e.g. "com.lib.Util" for "com.lib.Util.inlined".
	OrigClass string
}

// ProguardClassMapping represents the de-obfuscation map for a single Java/Kotlin class.
type ProguardClassMapping struct {
	OriginalClass string
	Methods       map[string][]ProguardMethodMapping // obfuscatedMethod -> []ProguardMethodMapping
	Fields        map[string]string                  // obfuscatedField -> originalField
}

// ProguardRetracer reads a ProGuard / R8 mapping.txt file and symbolicates Android stack traces.
type ProguardRetracer struct {
	classMap map[string]*ProguardClassMapping // obfuscatedClass -> mapping
}

// ParseProguardMapping decodes a ProGuard / R8 mapping.txt buffer.
func ParseProguardMapping(data []byte) (*ProguardRetracer, error) {
	if bytes.IndexByte(data, 0) != -1 {
		return nil, fmt.Errorf("invalid proguard mapping: binary content detected")
	}
	trimmedData := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmedData, []byte("--")) && bytes.Contains(trimmedData[:min(len(trimmedData), 512)], []byte("Content-Disposition:")) {
		return nil, fmt.Errorf("invalid proguard mapping: unparsed multipart form data")
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	retracer := &ProguardRetracer{
		classMap: make(map[string]*ProguardClassMapping),
	}

	var currentClass *ProguardClassMapping

	// Regex for class line: "com.example.original.ClassName -> a.b.c:"
	classLineRegex := regexp.MustCompile(`^([\w\.\$]+)\s+->\s+([\w\.\$]+):$`)
	// Regex for method line: "12:15:void doSomething() -> a", "void doSomething() -> a"
	// or with an original range "1:3:void foo():10:12 -> a".
	methodLineRegex := regexp.MustCompile(`^(?:(\d+):(\d+):)?[\w.$\[\]]+\s+([\w.$<>]+)\(([^)]*)\)(?::(\d+)(?::(\d+))?)?\s+->\s+([\w$<>]+)$`)
	// Regex for field line: "int count -> a"
	fieldLineRegex := regexp.MustCompile(`^(?:[\w\.\$\[\]]+)\s+([\w\$]+)\s+->\s+([\w\$]+)$`)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			if currentClass == nil {
				continue
			}
			// Parse member
			if m := methodLineRegex.FindStringSubmatch(trimmed); m != nil {
				startLine, _ := strconv.Atoi(m[1])
				endLine, _ := strconv.Atoi(m[2])
				origStart, _ := strconv.Atoi(m[5])
				origEnd, _ := strconv.Atoi(m[6])
				origName := m[3]
				obfMethod := m[7]

				origClass := ""
				if dot := strings.LastIndex(origName, "."); dot != -1 {
					origClass = origName[:dot]
					origName = origName[dot+1:]
				}

				currentClass.Methods[obfMethod] = append(currentClass.Methods[obfMethod], ProguardMethodMapping{
					OriginalName: origName,
					StartLine:    startLine,
					EndLine:      endLine,
					OrigStart:    origStart,
					OrigEnd:      origEnd,
					OrigClass:    origClass,
				})
			} else if f := fieldLineRegex.FindStringSubmatch(trimmed); len(f) >= 3 {
				origField := f[1]
				obfField := f[2]
				currentClass.Fields[obfField] = origField
			}
		} else {
			// Class line
			if m := classLineRegex.FindStringSubmatch(trimmed); len(m) >= 3 {
				origClass := m[1]
				obfClass := m[2]
				currentClass = &ProguardClassMapping{
					OriginalClass: origClass,
					Methods:       make(map[string][]ProguardMethodMapping),
					Fields:        make(map[string]string),
				}
				retracer.classMap[obfClass] = currentClass
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read mapping: %w", err)
	}

	return retracer, nil
}

// RetraceClass returns the original class name for an obfuscated class.
func (r *ProguardRetracer) RetraceClass(obfClass string) string {
	if mapping, ok := r.classMap[obfClass]; ok {
		return mapping.OriginalClass
	}
	return obfClass
}

// RetraceMethod returns the original method name and source file/line if available.
func (r *ProguardRetracer) RetraceMethod(obfClass, obfMethod string, lineNum int) (string, string, int) {
	classMapping, ok := r.classMap[obfClass]
	if !ok {
		return obfClass, obfMethod, lineNum
	}

	origClass := classMapping.OriginalClass

	methods, ok := classMapping.Methods[obfMethod]
	if !ok || len(methods) == 0 {
		return origClass, obfMethod, lineNum
	}

	// Find matching method range
	for _, m := range methods {
		if lineNum == 0 || (lineNum >= m.StartLine && (m.EndLine == 0 || lineNum <= m.EndLine)) {
			// Extract clean method name without arguments for standard frame
			cleanName := m.OriginalName
			if parenIdx := strings.Index(cleanName, "("); parenIdx != -1 {
				cleanName = cleanName[:parenIdx]
			}
			if m.OrigClass != "" {
				origClass = m.OrigClass
			}
			return origClass, cleanName, lineNum
		}
	}

	// Fallback to first mapping
	first := methods[0].OriginalName
	if parenIdx := strings.Index(first, "("); parenIdx != -1 {
		first = first[:parenIdx]
	}
	if methods[0].OrigClass != "" {
		origClass = methods[0].OrigClass
	}
	return origClass, first, lineNum
}

// RetracedFrame is one original (de-obfuscated) frame.
type RetracedFrame struct {
	Class, Method, File string
	Line                int
}

// RetraceFrame returns the original frames for one obfuscated frame, innermost
// (inlined callee) first. ok is false when obfClass is not in the mapping, in
// which case the caller keeps the frame as-is.
//
// When the class is known but no mapping entry matches the line, the class is
// still retraced and a single frame with the unchanged method and line is returned.
func (r *ProguardRetracer) RetraceFrame(obfClass, obfMethod, file string, line int) (frames []RetracedFrame, ok bool) {
	if r == nil {
		return nil, false
	}
	classMapping, ok := r.classMap[obfClass]
	if !ok {
		return nil, false
	}

	var kept []ProguardMethodMapping
	for _, m := range classMapping.Methods[obfMethod] {
		if line > 0 && m.StartLine <= line && line <= m.EndLine {
			kept = append(kept, m)
		}
	}
	matched := len(kept) > 0
	if !matched {
		kept = fallbackCandidates(classMapping, obfMethod, line)
	}

	for _, m := range kept {
		class := classMapping.OriginalClass
		if m.OrigClass != "" {
			class = m.OrigClass
		}
		origLine := line
		switch {
		case !matched:
			// The line did not select this entry: keep it unchanged.
		case m.OrigStart > 0 && m.OrigEnd > 0 && m.OrigStart != m.OrigEnd && m.StartLine <= line:
			origLine = m.OrigStart + (line - m.StartLine)
		case m.OrigStart > 0:
			origLine = m.OrigStart
		}
		resolvedFile := file
		if file == "" || file == "SourceFile" {
			resolvedFile = simpleClassName(class) + ".kt"
		}
		frames = append(frames, RetracedFrame{Class: class, Method: m.OriginalName, File: resolvedFile, Line: origLine})
	}
	return frames, true
}

// fallbackCandidates picks the single frame to emit when no ranged entry matched the line (line is left
// unchanged by the caller). Inline chains only come from ranged entries that match the line, so this never
// returns more than one entry: the first candidate, which is also what RetraceMethod returns and is the
// shared mapping whenever all candidates agree on (class, original name). Candidates are the unranged entries
// when there is a line to look at, else every entry; with no entry at all the obfuscated name is kept.
func fallbackCandidates(cm *ProguardClassMapping, obfMethod string, line int) []ProguardMethodMapping {
	all := cm.Methods[obfMethod]
	var cands []ProguardMethodMapping
	if line > 0 {
		for _, m := range all {
			if m.StartLine == 0 && m.EndLine == 0 {
				cands = append(cands, m)
			}
		}
	}
	if len(cands) == 0 {
		cands = all
	}
	if len(cands) == 0 {
		return []ProguardMethodMapping{{OriginalName: obfMethod}}
	}
	return cands[:1]
}

// simpleClassName returns the outermost simple class name (no package, no $inner suffix).
func simpleClassName(class string) string {
	simple := class
	if idx := strings.LastIndex(class, "."); idx != -1 {
		simple = class[idx+1:]
	}
	if dollarIdx := strings.Index(simple, "$"); dollarIdx != -1 {
		simple = simple[:dollarIdx]
	}
	return simple
}

// androidStackFrameRegex matches:
// "at com.example.a.b(SourceFile:42)"
// "at a.b.c(Native Method)"
// "at a.b.c(Unknown Source)"
var androidStackFrameRegex = regexp.MustCompile(`^\s*at\s+([\w\.\$]+)\.([\w\$<>]+)\(([^:\)]+)?(?::(\d+))?\)`)

// RetraceStackTrace de-obfuscates an entire multi-line Android/Kotlin stack trace.
func (r *ProguardRetracer) RetraceStackTrace(stackTrace string) string {
	if r == nil || stackTrace == "" {
		return stackTrace
	}

	lines := strings.Split(stackTrace, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if it's an exception message line: "a.b.c: An error occurred"
		if colonIdx := strings.Index(trimmed, ":"); colonIdx != -1 && !strings.HasPrefix(trimmed, "at ") {
			potentialClass := trimmed[:colonIdx]
			if orig := r.RetraceClass(potentialClass); orig != potentialClass {
				lines[i] = orig + trimmed[colonIdx:]
				continue
			}
		}

		// Check if it's a stack frame line: "at a.b.c(SourceFile:42)"
		matches := androidStackFrameRegex.FindStringSubmatch(trimmed)
		if len(matches) >= 3 {
			obfClass := matches[1]
			obfMethod := matches[2]
			sourceFile := matches[3]
			lineNum := 0
			if len(matches) >= 5 && matches[4] != "" {
				lineNum, _ = strconv.Atoi(matches[4])
			}

			// One output line per original frame, innermost (inlined callee) first.
			frames, ok := r.RetraceFrame(obfClass, obfMethod, sourceFile, lineNum)
			if !ok {
				frames = []RetracedFrame{{Class: obfClass, Method: obfMethod, Line: lineNum}}
			}

			out := make([]string, 0, len(frames))
			for _, f := range frames {
				origSimple := simpleClassName(f.Class)
				resolvedSource := origSimple + ".kt"
				if strings.HasSuffix(sourceFile, ".java") {
					resolvedSource = origSimple + ".java"
				}

				var locPart string
				if f.Line > 0 {
					locPart = fmt.Sprintf("(%s:%d)", filepath.Base(resolvedSource), f.Line)
				} else if sourceFile != "" {
					locPart = fmt.Sprintf("(%s)", sourceFile)
				} else {
					locPart = "()"
				}

				out = append(out, fmt.Sprintf("    at %s.%s%s", f.Class, f.Method, locPart))
			}
			lines[i] = strings.Join(out, "\n")
		}
	}

	return strings.Join(lines, "\n")
}
