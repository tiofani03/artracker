// Package people builds per-user profiles and per-session context from ingested telemetry.
package people

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"
)

const (
	MaxTraitKeys     = 50
	MaxTraitKeyLen   = 64
	MaxTraitValueLen = 256
	maxContextKeys   = 64
)

// sensitiveTokens are matched against the words of a key (split on separators and camelCase), so `pin` drops
// `pin` / `user_pin` / `pinCode` but keeps `shipping`, and `card` keeps `discard_reason`.
var sensitiveTokens = map[string]bool{
	"pass": true, "password": true, "passwd": true, "pwd": true, "secret": true, "token": true,
	"apikey": true, "authorization": true, "auth": true, "cookie": true, "card": true, "cardnumber": true,
	"cvv": true, "cvc": true, "pin": true, "nik": true, "ssn": true,
}

// sensitiveSubstrings catch run-together spellings such as `userpassword` or `x_api_key`.
var sensitiveSubstrings = []string{"password", "passwd", "secret", "apikey", "api_key", "api-key", "authorization"}

func keyWords(key string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	runes := []rune(key)
	for i, r := range runes {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r) && i > 0 && unicode.IsLower(runes[i-1]):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}

// IsSensitiveKey reports whether a trait key looks like it holds a credential or identity number.
func IsSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveSubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	words := keyWords(key)
	for _, w := range words {
		if sensitiveTokens[w] {
			return true
		}
	}
	// "api" followed by "key" as separate words (API_KEY, apiKey).
	for i := 0; i+1 < len(words); i++ {
		if words[i] == "api" && words[i+1] == "key" {
			return true
		}
	}
	return false
}

// SanitizeTraits turns a raw `$identify` attribute payload into a bounded flat trait map. A nil value means
// "delete this key". It never fails: garbage input yields an empty map.
func SanitizeTraits(raw []byte) map[string]any {
	out := map[string]any{}
	var in map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &in) != nil {
		return out
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if len(out) >= MaxTraitKeys {
			break
		}
		if k == "" || len([]rune(k)) > MaxTraitKeyLen || IsSensitiveKey(k) {
			continue
		}
		switch v := in[k].(type) {
		case nil, bool, float64:
			out[k] = v
		case string:
			if r := []rune(v); len(r) > MaxTraitValueLen {
				v = string(r[:MaxTraitValueLen])
			}
			out[k] = v
		}
	}
	return out
}

// sanitizeContext keeps a session-start attribute map as-is, capped at maxContextKeys (sorted order).
func sanitizeContext(raw []byte) map[string]any {
	var in map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &in) != nil || in == nil {
		return map[string]any{}
	}
	if len(in) <= maxContextKeys {
		return in
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]any, maxContextKeys)
	for _, k := range keys[:maxContextKeys] {
		out[k] = in[k]
	}
	return out
}
