// Package issues implements the error-tracking domain: deterministic crash
// fingerprinting and Source Map v3 demangling.
package issues

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// frameLocRe matches a source location suffix such as "app.min.js:1:240" or
// "PaymentForm.tsx:42:18" optionally wrapped in parentheses.
var frameLocRe = regexp.MustCompile(`([\w./:@-]+(?:\.min)?\.(?:js|mjs|cjs|jsx|ts|tsx|css|map)):(\d+):(\d+)`)

// minifiedFileRe matches files produced by a bundler/minifier.
var minifiedFileRe = regexp.MustCompile(`\.min\.(?:js|mjs|cjs|jsx|ts|tsx|css)$`)

var wsRunRe = regexp.MustCompile(`\s+`)

// NormalizeFrame reduces a single stack frame line to a stable signature so
// identical code across different environments still groups together.
//   - function name is kept,
//   - URL scheme/host are stripped,
//   - column numbers are dropped,
//   - line numbers are dropped for minified bundles (where they are meaningless).
func NormalizeFrame(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	loc := frameLocRe.FindStringSubmatch(line)
	if loc == nil {
		// No parseable location: fall back to the whitespace-collapsed line.
		return wsRunRe.ReplaceAllString(line, " ")
	}

	file := stripURLHost(loc[1])
	lineNo := loc[2]

	// Function name is whatever precedes the location on the frame.
	fn := strings.TrimSpace(line[:strings.Index(line, loc[0])])
	fn = strings.Trim(fn, " \t()@")
	fn = wsRunRe.ReplaceAllString(fn, " ")

	// Minified bundles change their line numbers on every rebuild, so they
	// must not participate in the fingerprint.
	if minifiedFileRe.MatchString(file) {
		return fn + "@" + file
	}
	return fn + "@" + file + ":" + lineNo
}

// stripURLHost removes "https://host/" so the same app served from different
// CDN hosts / environments still fingerprints identically.
func stripURLHost(file string) string {
	scheme := strings.Index(file, "://")
	if scheme == -1 {
		return file
	}
	rest := file[scheme+3:]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		return rest[slash+1:]
	}
	return rest
}

// Fingerprint returns a stable, hex-encoded SHA-256 digest for a crash. Frames
// are normalized first, so minified bundles group by file+function while
// original sources group by file+function+line.
func Fingerprint(stackTrace, message, errType string) string {
	var frames []string
	for _, line := range strings.Split(stackTrace, "\n") {
		if f := NormalizeFrame(line); f != "" {
			frames = append(frames, f)
		}
	}

	var material string
	if len(frames) > 0 {
		material = strings.TrimSpace(errType) + "\n" + strings.Join(frames, "\n")
	} else {
		// No stack trace: group on the normalized message so identical errors
		// still share a fingerprint.
		material = strings.TrimSpace(errType) + "\n" + NormalizeMessage(message)
	}

	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

// NormalizeMessage collapses whitespace and caps the length, useful both for
// message-only fingerprints and for deriving a stable issue title.
func NormalizeMessage(message string) string {
	message = wsRunRe.ReplaceAllString(strings.TrimSpace(message), " ")
	if len(message) > 400 {
		message = message[:400]
	}
	return message
}

// ErrorType extracts the exception class (e.g. "TypeError", "NullPointerException", "SIGSEGV")
// from a message, or falls back to "error".
func ErrorType(message string) string {
	m := strings.TrimSpace(message)
	if i := strings.Index(m, ":"); i > 0 {
		head := strings.TrimSpace(m[:i])
		if lastDot := strings.LastIndex(head, "."); lastDot != -1 && lastDot < len(head)-1 {
			head = head[lastDot+1:]
		}
		if matched, _ := regexp.MatchString(`^[A-Za-z][A-Za-z0-9_]*(?:Error|Exception|Signal|Hang|ANR)$`, head); matched {
			return head
		}
		if strings.HasPrefix(head, "SIG") || strings.HasPrefix(head, "EXC_") {
			return head
		}
	}
	return "error"
}

// Title derives a human-readable issue title from the exception message.
func Title(message string) string {
	m := NormalizeMessage(message)
	if m == "" {
		return "Unknown error"
	}
	return m
}
