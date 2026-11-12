package issues

import "strings"

// Go twin of frontend/src/issues/severity.ts (isFatalCrash); keep rules in sync.

// IsFatal reports whether an issue should be labelled a fatal crash.
func IsFatal(issueType, title string) bool {
	t := strings.ToLower(issueType)
	ti := strings.ToLower(title)
	return strings.Contains(t, "fatal") ||
		strings.Contains(t, "crash") ||
		strings.Contains(t, "panic") ||
		strings.Contains(ti, "uncaught") ||
		strings.Contains(ti, "unhandled") ||
		strings.Contains(ti, "fatal") ||
		strings.Contains(ti, "crash") ||
		strings.Contains(t, "typeerror") ||
		strings.Contains(t, "referenceerror")
}
