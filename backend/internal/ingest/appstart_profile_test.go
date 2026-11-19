package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const androidProfileJSON = `{
  "sdk": {"name": "artracker-android", "version": "0.4"},
  "context": {"app_version": "3.2.0", "build": "1203", "environment": "production"},
  "profile": {
    "v": 1, "start_id": "s-1", "platform": "android", "app_packages": ["com.example.shop"],
    "interval_ms": 10, "t0": "process_start",
    "frames": [{"c":"a.b.C","m":"d","l":42,"f":"C.kt"}, {"c":"a.b.E","m":"f"}],
    "stacks": [[0,1],[0]],
    "samples": [{"t":10,"s":0},{"t":20,"s":1},{"t":20,"s":0}]
  }
}`

const iosProfileJSON = `{
  "sdk": {"name": "artracker-ios", "version": "0.4"},
  "context": {"app_version": "1.4.0 (217)", "environment": "production"},
  "profile": {
    "v": 1, "start_id": "s-2", "platform": "ios", "interval_ms": 10, "t0": "process_start",
    "frames": [{"a":"0x1045f3a20","i":0}, {"a":"0x1a2b","i":1,"sym":"objc_msgSend"}],
    "images": [
      {"uuid":"e2a1b3c4d5e6f7089a0b1c2d3e4f5a6b","name":"MyApp","load":"0x104000000","app":true},
      {"uuid":"E2A1B3C4-D5E6-F708-9A0B-1C2D3E4F5A6B","name":"Dup","load":"0x1","app":false},
      {"uuid":"00112233-4455-6677-8899-aabbccddeeff","name":"UIKit","load":"0x180000000","app":false}
    ],
    "stacks": [[0,1]],
    "samples": [{"t":5,"s":0}]
  }
}`

func TestParseAppStartProfileAndroidValid(t *testing.T) {
	up, err := ParseAppStartProfile([]byte(androidProfileJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p := up.Profile
	if p.StartID != "s-1" || p.Platform != "android" || len(p.Frames) != 2 || len(p.Samples) != 3 {
		t.Fatalf("unexpected profile: %+v", p)
	}
	if p.Frames[0].L == nil || *p.Frames[0].L != 42 || p.Frames[1].L != nil {
		t.Fatalf("line pointers wrong: %+v", p.Frames)
	}
	if up.SDK.Name != "artracker-android" || up.Context["build"] != "1203" {
		t.Fatalf("envelope fields lost: %+v %+v", up.SDK, up.Context)
	}
	if got := p.ImageUUIDs(); len(got) != 0 {
		t.Fatalf("android ImageUUIDs = %v, want none", got)
	}
}

func TestParseAppStartProfileIOSValid(t *testing.T) {
	up, err := ParseAppStartProfile([]byte(iosProfileJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{"E2A1B3C4-D5E6-F708-9A0B-1C2D3E4F5A6B", "00112233-4455-6677-8899-AABBCCDDEEFF"}
	if got := up.Profile.ImageUUIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ImageUUIDs = %v, want %v", got, want)
	}
}

// mutate decodes the base payload, applies fn to the profile object and re-encodes it.
func mutateProfile(t *testing.T, base string, fn func(p map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(base), &doc); err != nil {
		t.Fatal(err)
	}
	fn(doc["profile"].(map[string]any))
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestParseAppStartProfileRejects(t *testing.T) {
	manySamples := func(n int) []any {
		s := make([]any, n)
		for i := range s {
			s[i] = map[string]any{"t": i % 100, "s": 0}
		}
		return s
	}
	manyStacks := func(n int) []any {
		s := make([]any, n)
		for i := range s {
			s[i] = []any{0}
		}
		return s
	}
	manyFrames := func(n int) []any {
		s := make([]any, n)
		for i := range s {
			s[i] = map[string]any{"c": "a", "m": fmt.Sprint("m", i)}
		}
		return s
	}
	manyImages := func(n int) []any {
		s := make([]any, n)
		for i := range s {
			s[i] = map[string]any{"uuid": fmt.Sprintf("%032x", i), "name": "x", "load": "0x1", "app": false}
		}
		return s
	}
	deepStack := make([]any, MaxProfileStackDepth+1)
	for i := range deepStack {
		deepStack[i] = 0
	}

	cases := []struct {
		name string
		base string
		fn   func(p map[string]any)
	}{
		{"version 0", androidProfileJSON, func(p map[string]any) { p["v"] = 0 }},
		{"version 2", androidProfileJSON, func(p map[string]any) { p["v"] = 2 }},
		{"empty start_id", androidProfileJSON, func(p map[string]any) { p["start_id"] = "" }},
		{"nul-only start_id", androidProfileJSON, func(p map[string]any) { p["start_id"] = "\x00\x00" }},
		{"long start_id", androidProfileJSON, func(p map[string]any) { p["start_id"] = strings.Repeat("x", 129) }},
		{"platform web", androidProfileJSON, func(p map[string]any) { p["platform"] = "web" }},
		{"interval 0", androidProfileJSON, func(p map[string]any) { p["interval_ms"] = 0 }},
		{"interval 101", androidProfileJSON, func(p map[string]any) { p["interval_ms"] = 101 }},
		{"too many samples", androidProfileJSON, func(p map[string]any) { p["samples"] = manySamples(MaxProfileSamples + 1) }},
		{"too many stacks", androidProfileJSON, func(p map[string]any) { p["stacks"] = manyStacks(MaxProfileStacks + 1) }},
		{"too many frames", androidProfileJSON, func(p map[string]any) { p["frames"] = manyFrames(MaxProfileFrames + 1) }},
		{"too many images", iosProfileJSON, func(p map[string]any) { p["images"] = manyImages(MaxProfileImages + 1) }},
		{"stack too deep", androidProfileJSON, func(p map[string]any) { p["stacks"] = []any{deepStack}; p["samples"] = []any{} }},
		{"stack frame index out of range", androidProfileJSON, func(p map[string]any) { p["stacks"] = []any{[]any{0, 2}} }},
		{"negative stack frame index", androidProfileJSON, func(p map[string]any) { p["stacks"] = []any{[]any{-1}} }},
		{"sample stack index out of range", androidProfileJSON, func(p map[string]any) { p["samples"] = []any{map[string]any{"t": 1, "s": 2}} }},
		{"negative sample stack index", androidProfileJSON, func(p map[string]any) { p["samples"] = []any{map[string]any{"t": 1, "s": -1}} }},
		{"negative t", androidProfileJSON, func(p map[string]any) { p["samples"] = []any{map[string]any{"t": -1, "s": 0}} }},
		{"t above 60000", androidProfileJSON, func(p map[string]any) { p["samples"] = []any{map[string]any{"t": 60001, "s": 0}} }},
		{"unsorted samples", androidProfileJSON, func(p map[string]any) {
			p["samples"] = []any{map[string]any{"t": 20, "s": 0}, map[string]any{"t": 10, "s": 0}}
		}},
		{"ios frame without image index", iosProfileJSON, func(p map[string]any) {
			p["frames"] = []any{map[string]any{"a": "0x10"}}
			p["stacks"] = []any{[]any{0}}
		}},
		{"ios frame image index out of range", iosProfileJSON, func(p map[string]any) {
			p["frames"] = []any{map[string]any{"a": "0x10", "i": 3}}
			p["stacks"] = []any{[]any{0}}
		}},
		{"ios frame address not hex", iosProfileJSON, func(p map[string]any) {
			p["frames"] = []any{map[string]any{"a": "0xZZ", "i": 0}}
			p["stacks"] = []any{[]any{0}}
		}},
		{"ios frame empty address", iosProfileJSON, func(p map[string]any) { p["frames"] = []any{map[string]any{"i": 0}}; p["stacks"] = []any{[]any{0}} }},
		{"android frame empty class", androidProfileJSON, func(p map[string]any) { p["frames"] = []any{map[string]any{"m": "x"}}; p["stacks"] = []any{[]any{0}} }},
		{"android frame empty method", androidProfileJSON, func(p map[string]any) { p["frames"] = []any{map[string]any{"c": "x"}}; p["stacks"] = []any{[]any{0}} }},
		{"too many app_packages", androidProfileJSON, func(p map[string]any) {
			pk := make([]any, 21)
			for i := range pk {
				pk[i] = "com.x"
			}
			p["app_packages"] = pk
		}},
		{"long app_package", androidProfileJSON, func(p map[string]any) { p["app_packages"] = []any{strings.Repeat("a", 129)} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAppStartProfile(mutateProfile(t, tc.base, tc.fn))
			if !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("err = %v, want ErrInvalidProfile", err)
			}
		})
	}

	t.Run("malformed json", func(t *testing.T) {
		if _, err := ParseAppStartProfile([]byte(`{"profile":`)); !errors.Is(err, ErrInvalidProfile) {
			t.Fatalf("err = %v, want ErrInvalidProfile", err)
		}
	})
}

func TestParseAppStartProfileAcceptsLimits(t *testing.T) {
	deep := make([]any, MaxProfileStackDepth)
	for i := range deep {
		deep[i] = 0
	}
	raw := mutateProfile(t, androidProfileJSON, func(p map[string]any) {
		p["stacks"] = []any{deep}
		p["samples"] = []any{map[string]any{"t": 0, "s": 0}, map[string]any{"t": 60000, "s": 0}}
		p["interval_ms"] = 100
	})
	if _, err := ParseAppStartProfile(raw); err != nil {
		t.Fatalf("boundary values rejected: %v", err)
	}
}

func TestGunzipLimit(t *testing.T) {
	d := NewDecompressor()
	big := gzipBytes(t, make([]byte, 5<<20))
	if _, err := d.GunzipLimit(big, 4<<20); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("err = %v, want ErrPayloadTooLarge", err)
	}
	out, err := d.GunzipLimit(big, 5<<20)
	if err != nil || len(out) != 5<<20 {
		t.Fatalf("at-limit gunzip: len=%d err=%v", len(out), err)
	}
	if !bytes.Equal(out[:4], []byte{0, 0, 0, 0}) {
		t.Fatal("unexpected content")
	}
}

func TestCleanStoredString(t *testing.T) {
	if got := CleanStoredString("a\x00b\xffc", 10); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := CleanStoredString("héllo wörld", 4); got != "héll" {
		t.Fatalf("rune-safe truncation: got %q", got)
	}
	if got := CleanStoredString("\x00", 4); got != "" {
		t.Fatalf("got %q", got)
	}
}
