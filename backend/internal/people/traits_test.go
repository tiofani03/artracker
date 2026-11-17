package people

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSanitizeTraitsLimits(t *testing.T) {
	in := map[string]any{}
	for i := 0; i < 60; i++ {
		in[fmt.Sprintf("k%02d", i)] = i
	}
	got := SanitizeTraits(mustJSON(t, in))
	if len(got) != MaxTraitKeys {
		t.Fatalf("want %d keys, got %d", MaxTraitKeys, len(got))
	}
	if _, ok := got["k49"]; !ok {
		t.Fatal("k49 should be kept (sorted-order cut)")
	}
	if _, ok := got["k50"]; ok {
		t.Fatal("k50 should be cut")
	}

	got = SanitizeTraits(mustJSON(t, map[string]any{
		strings.Repeat("a", 65): "x",
		"long":                  strings.Repeat("é", 300),
		"obj":                   map[string]any{"b": 1},
		"arr":                   []int{1},
		"ok":                    true,
	}))
	if _, ok := got[strings.Repeat("a", 65)]; ok {
		t.Fatal("65-char key must be dropped")
	}
	if s, _ := got["long"].(string); utf8.RuneCountInString(s) != MaxTraitValueLen {
		t.Fatalf("long value should be %d runes, got %d", MaxTraitValueLen, utf8.RuneCountInString(s))
	}
	if _, ok := got["obj"]; ok {
		t.Fatal("nested object must be dropped")
	}
	if _, ok := got["arr"]; ok {
		t.Fatal("array must be dropped")
	}
	if got["ok"] != true {
		t.Fatal("bool must be kept")
	}
}

func TestSanitizeTraitsSensitive(t *testing.T) {
	got := SanitizeTraits(mustJSON(t, map[string]any{
		"password": "x", "API_KEY": "x", "cardNumber": "x", "nik": "x", "plan": "pro",
	}))
	if len(got) != 1 || got["plan"] != "pro" {
		t.Fatalf("only plan should survive, got %v", got)
	}
}

func TestSanitizeTraitsNullDeletes(t *testing.T) {
	got := SanitizeTraits([]byte(`{"plan":null}`))
	v, ok := got["plan"]
	if !ok || v != nil {
		t.Fatalf("want plan:nil, got %v (present=%v)", v, ok)
	}
}

func TestSanitizeTraitsGarbage(t *testing.T) {
	for _, raw := range [][]byte{[]byte("[1,2]"), []byte("nope"), nil, []byte("")} {
		got := SanitizeTraits(raw)
		if got == nil || len(got) != 0 {
			t.Fatalf("%q: want empty non-nil map, got %v", raw, got)
		}
	}
}

func TestSensitiveKeyDoesNotOverMatch(t *testing.T) {
	for _, k := range []string{"shipping", "discard_reason", "spinner_theme", "plan", "country", "passport_country"} {
		if IsSensitiveKey(k) {
			t.Errorf("%q must not be treated as sensitive", k)
		}
	}
	for _, k := range []string{"pin", "user_pin", "pinCode", "apiKey", "x-api-key", "authToken", "creditCard", "userPassword", "SSN"} {
		if !IsSensitiveKey(k) {
			t.Errorf("%q must be treated as sensitive", k)
		}
	}
}
