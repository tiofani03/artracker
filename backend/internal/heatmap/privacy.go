package heatmap

import (
	"strings"
	"unicode"
)

// isUnrecognized reports screen names that carry no usable identity: empty, the SDK fallbacks "App" and
// "Screen", and iOS system windows (UI...WindowController).
func isUnrecognized(screen string) bool {
	switch screen {
	case "", "App", "Screen":
		return true
	}
	return strings.HasPrefix(screen, "UI") && strings.HasSuffix(screen, "WindowController")
}

// SensitiveRule is the human-readable rule shown in Settings.
const SensitiveRule = "Words: pin, otp, cvv; words starting with password, passcode, login, signin, payment, secur; sign in; card number"

// splitWords lowercases a screen name split at CamelCase, letter/digit and any non-alphanumeric
// boundaries. Acronym runs stay together until the last capital that starts a new word: "OTPView" -> otp,
// view. A lowercase plural stays with its acronym: "EnterPINsScreen" -> enter, pins, screen
// ("pins" matches the exact word "pin" through IsSensitive's singularization).
func splitWords(screen string) []string {
	r := []rune(screen)
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	for i, c := range r {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			flush()
			continue
		}
		if len(cur) > 0 {
			prev := r[i-1]
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			// an acronym plural ("OTPs", "PINsScreen"): a lone lowercase s does not start a new word
			if nextLower && r[i+1] == 's' && (i+2 >= len(r) || !unicode.IsLower(r[i+2])) {
				nextLower = false
			}
			switch {
			case unicode.IsDigit(c) != unicode.IsDigit(prev):
				flush()
			case unicode.IsUpper(c) && (unicode.IsLower(prev) || (unicode.IsUpper(prev) && nextLower)):
				flush()
			}
		}
		cur = append(cur, c)
	}
	flush()
	return words
}

// IsSensitive applies the word-based sensitive screen rule (not a substring match).
func IsSensitive(screen string) bool {
	words := splitWords(screen)
	for i, w := range words {
		switch strings.TrimSuffix(w, "s") {
		case "pin", "otp", "cvv":
			return true
		}
		for _, p := range []string{"password", "passcode", "login", "signin", "payment", "secur"} {
			if strings.HasPrefix(w, p) {
				return true
			}
		}
		if i+1 < len(words) {
			n := words[i+1]
			if (w == "sign" && n == "in") || (w == "card" && n == "number") {
				return true
			}
		}
	}
	return false
}

// SkipFunc returns Extract's skip predicate: IsSensitive OR exact (case-sensitive) match in excluded.
func SkipFunc(excluded []string) func(screen string) bool {
	set := make(map[string]struct{}, len(excluded))
	for _, s := range excluded {
		set[s] = struct{}{}
	}
	return func(screen string) bool {
		if _, ok := set[screen]; ok {
			return true
		}
		return IsSensitive(screen)
	}
}
