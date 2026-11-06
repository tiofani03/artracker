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
	scanner := bufio.NewScanner(bytes.NewReader(data))
	retracer := &ProguardRetracer{
		classMap: make(map[string]*ProguardClassMapping),
	}

	var currentClass *ProguardClassMapping

	// Regex for class line: "com.example.original.ClassName -> a.b.c:"
	classLineRegex := regexp.MustCompile(`^([\w\.\$]+)\s+->\s+([\w\.\$]+):$`)
	// Regex for method line: "12:15:void doSomething() -> a" or "void doSomething() -> a"
	methodLineRegex := regexp.MustCompile(`^(?:(\d+):(\d+):)?(?:[\w\.\$\[\]]+)\s+([\w\.\$<>]+\([^\)]*\))\s+->\s+([\w\$]+)$`)
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
			if m := methodLineRegex.FindStringSubmatch(trimmed); len(m) >= 5 {
				startLine := 0
				endLine := 0
				if m[1] != "" {
					startLine, _ = strconv.Atoi(m[1])
				}
				if m[2] != "" {
					endLine, _ = strconv.Atoi(m[2])
				}
				origMethod := m[3]
				obfMethod := m[4]

				currentClass.Methods[obfMethod] = append(currentClass.Methods[obfMethod], ProguardMethodMapping{
					OriginalName: origMethod,
					StartLine:    startLine,
					EndLine:      endLine,
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
			return origClass, cleanName, lineNum
		}
	}

	// Fallback to first mapping
	first := methods[0].OriginalName
	if parenIdx := strings.Index(first, "("); parenIdx != -1 {
		first = first[:parenIdx]
	}
	return origClass, first, lineNum
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

			origClass, origMethod, resolvedLine := r.RetraceMethod(obfClass, obfMethod, lineNum)

			origSimple := origClass
			if idx := strings.LastIndex(origClass, "."); idx != -1 {
				origSimple = origClass[idx+1:]
			}
			if dollarIdx := strings.Index(origSimple, "$"); dollarIdx != -1 {
				origSimple = origSimple[:dollarIdx]
			}
			resolvedSource := origSimple + ".kt"
			if strings.HasSuffix(sourceFile, ".java") {
				resolvedSource = origSimple + ".java"
			}

			var locPart string
			if resolvedLine > 0 {
				locPart = fmt.Sprintf("(%s:%d)", filepath.Base(resolvedSource), resolvedLine)
			} else if sourceFile != "" {
				locPart = fmt.Sprintf("(%s)", sourceFile)
			} else {
				locPart = "()"
			}

			lines[i] = fmt.Sprintf("    at %s.%s%s", origClass, origMethod, locPart)
		}
	}

	return strings.Join(lines, "\n")
}
