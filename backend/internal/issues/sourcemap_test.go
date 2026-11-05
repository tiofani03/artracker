package issues

import "testing"

// sampleMap is a minimal but valid Source Map v3 document. Its mappings encode a
// single mapping: generated (1,0) -> source 0, original line 0, column 0.
const sampleMap = `{
  "version": 3,
  "file": "app.min.js",
  "sources": ["../src/PaymentForm.tsx"],
  "names": ["handleSubmit"],
  "mappings": "AAAAA"
}`

func TestParseAndResolveSourceMap(t *testing.T) {
	sm, err := ParseSourceMap([]byte(sampleMap))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sm.File != "app.min.js" {
		t.Fatalf("file = %q", sm.File)
	}

	pos := sm.OriginalPosition(1, 1)
	if !pos.Found {
		t.Fatal("expected a resolved position")
	}
	if pos.Source != "../src/PaymentForm.tsx" || pos.Line != 1 || pos.Column != 1 {
		t.Fatalf("unexpected position: %+v", pos)
	}
	if pos.Name != "handleSubmit" {
		t.Fatalf("unexpected name: %q", pos.Name)
	}
}

func TestParseRejectsNonV3(t *testing.T) {
	if _, err := ParseSourceMap([]byte(`{"version":2,"file":"x","mappings":""}`)); err == nil {
		t.Fatal("expected an error for version 2")
	}
}

func TestDemangleStackTrace(t *testing.T) {
	sm, err := ParseSourceMap([]byte(sampleMap))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	stack := "TypeError: boom\n    at handleSubmit (app.min.js:1:1)"
	want := "TypeError: boom\n    at handleSubmit (../src/PaymentForm.tsx:1:1)"
	if got := sm.DemangleStackTrace(stack); got != want {
		t.Fatalf("DemangleStackTrace = %q, want %q", got, want)
	}
}

func TestOriginalPositionOutOfRange(t *testing.T) {
	sm, err := ParseSourceMap([]byte(sampleMap))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pos := sm.OriginalPosition(99, 1); pos.Found {
		t.Fatal("expected no mapping beyond the decoded lines")
	}
}
