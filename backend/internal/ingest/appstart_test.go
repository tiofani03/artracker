package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const appStartExampleAttrs = `{
	"v": 1,
	"platform": "android",
	"start_id": "7d1c1f0e-0000-4000-8000-000000000001",
	"type": "cold",
	"duration_ms": 2412,
	"phases": [
		{ "name": "process_to_init", "ms": 310 },
		{ "name": "application_on_create", "ms": 1502 },
		{ "name": "activity_create", "ms": 380 },
		{ "name": "first_draw", "ms": 220 }
	],
	"first_screen": "MainActivity",
	"trampoline": false,
	"profiled": true,
	"build": "1203",
	"device": { "model": "SM-A145F", "os_version": "14", "ram_mb": 3712, "cores": 8 }
}`

func appStartItem(attrs string) RawItem {
	ts := time.UnixMilli(5000).UTC()
	return RawItem{
		Type:       PerfItemType,
		Name:       AppStartItemName,
		Timestamp:  &ts,
		SessionID:  "ses_a",
		Attributes: json.RawMessage(attrs),
	}
}

func TestParseAppStartItemValid(t *testing.T) {
	received := time.UnixMilli(9000).UTC()
	row, err := ParseAppStartItem(7, received, "staging", "3.2.0", appStartItem(appStartExampleAttrs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row.Type != "cold" || row.DurationMs != 2412 || len(row.Phases) != 4 {
		t.Errorf("bad core fields: %+v", row)
	}
	if row.FirstScreen != "MainActivity" || row.RAMMB == nil || *row.RAMMB != 3712 {
		t.Errorf("bad first_screen/ram: %+v", row)
	}
	if row.Environment != "staging" || row.AppVersion != "3.2.0" || row.Build != "1203" {
		t.Errorf("bad env/version/build: %+v", row)
	}
	if row.ProjectID != 7 || row.SessionID != "ses_a" || row.Platform != "android" || row.StartID == "" {
		t.Errorf("bad ids: %+v", row)
	}
	if row.OSVersion != "14" || row.DeviceModel != "SM-A145F" || row.Trampoline || !row.Profiled {
		t.Errorf("bad device/flags: %+v", row)
	}
	if !row.StartedAt.Equal(time.UnixMilli(5000)) || !row.ReceivedAt.Equal(received) {
		t.Errorf("bad times: %v %v", row.StartedAt, row.ReceivedAt)
	}
}

func TestParseAppStartItemMissingTimestampUsesReceivedAt(t *testing.T) {
	item := appStartItem(appStartExampleAttrs)
	item.Timestamp = nil
	received := time.UnixMilli(9000).UTC()
	row, err := ParseAppStartItem(7, received, "production", "", item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !row.StartedAt.Equal(received) {
		t.Errorf("StartedAt=%v want %v", row.StartedAt, received)
	}
}

func TestSplitVersionBuild(t *testing.T) {
	cases := []struct{ in, version, build string }{
		{"1.4.0 (217)", "1.4.0", "217"},
		{"3.2.0", "3.2.0", ""},
		{"1.0 (beta) (5)", "1.0 (beta)", "5"},
		{"", "", ""},
	}
	for _, c := range cases {
		v, b := SplitVersionBuild(c.in)
		if v != c.version || b != c.build {
			t.Errorf("SplitVersionBuild(%q) = (%q, %q), want (%q, %q)", c.in, v, b, c.version, c.build)
		}
	}
}

func TestParseAppStartItemIOSVersion(t *testing.T) {
	const noBuild = `{"v":1,"platform":"ios","start_id":"s1","type":"cold","duration_ms":1000}`
	row, err := ParseAppStartItem(1, time.Now(), "production", "1.4.0 (217)", appStartItem(noBuild))
	if err != nil {
		t.Fatal(err)
	}
	if row.AppVersion != "1.4.0" || row.Build != "217" {
		t.Errorf("got version=%q build=%q", row.AppVersion, row.Build)
	}

	const withBuild = `{"v":1,"platform":"ios","start_id":"s1","type":"cold","duration_ms":1000,"build":"300"}`
	row, err = ParseAppStartItem(1, time.Now(), "production", "1.4.0 (217)", appStartItem(withBuild))
	if err != nil {
		t.Fatal(err)
	}
	if row.AppVersion != "1.4.0" || row.Build != "300" {
		t.Errorf("got version=%q build=%q", row.AppVersion, row.Build)
	}
}

func TestParseAppStartItemRejects(t *testing.T) {
	phases := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `{"name":"p","ms":1}`
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	attrs := func(platform, typ, startID string, duration int, phasesJSON string) string {
		return fmt.Sprintf(`{"v":1,"platform":%q,"start_id":%q,"type":%q,"duration_ms":%d,"phases":%s}`,
			platform, startID, typ, duration, phasesJSON)
	}
	cases := []struct{ name, attrs string }{
		{"bad version", `{"v":2,"platform":"android","start_id":"s","type":"cold","duration_ms":100}`},
		{"bad platform", attrs("web", "cold", "s", 100, "[]")},
		{"bad type", attrs("android", "lukewarm", "s", 100, "[]")},
		{"empty start_id", attrs("android", "cold", "", 100, "[]")},
		{"long start_id", attrs("android", "cold", strings.Repeat("x", 129), 100, "[]")},
		{"zero duration", attrs("android", "cold", "s", 0, "[]")},
		{"duration too long", attrs("android", "cold", "s", 60001, "[]")},
		{"too many phases", attrs("android", "cold", "s", 9, phases(9))},
		{"bad phase name", attrs("android", "cold", "s", 10, `[{"name":"Bad-Name","ms":10}]`)},
		{"phase name too long", attrs("android", "cold", "s", 10, `[{"name":"`+strings.Repeat("a", 41)+`","ms":10}]`)},
		{"negative phase ms", attrs("android", "cold", "s", 10, `[{"name":"a","ms":-1},{"name":"b","ms":11}]`)},
		{"phase sum mismatch", attrs("android", "cold", "s", 2412, `[{"name":"a","ms":1200},{"name":"b","ms":1200}]`)},
		{"invalid json", `{`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseAppStartItem(1, time.Now(), "production", "", appStartItem(c.attrs))
			if !errors.Is(err, ErrInvalidAppStart) {
				t.Fatalf("err = %v, want ErrInvalidAppStart", err)
			}
		})
	}

	// Phases within the 5 ms tolerance are accepted.
	ok := attrs("android", "cold", "s", 2412, `[{"name":"a","ms":1200},{"name":"b","ms":1208}]`)
	if _, err := ParseAppStartItem(1, time.Now(), "production", "", appStartItem(ok)); err != nil {
		t.Errorf("sum 2408 vs 2412 should be accepted: %v", err)
	}
}

func TestParseAppStartItemNormalises(t *testing.T) {
	attrs := fmt.Sprintf(`{"v":1,"platform":"ios","start_id":"s","type":"warm","duration_ms":500,
		"first_screen":%q,"build":%q,"device":{"model":%q,"os_version":%q,"ram_mb":0}}`,
		strings.Repeat("a", 300), strings.Repeat("b", 100), strings.Repeat("c", 100), strings.Repeat("d", 100))
	row, err := ParseAppStartItem(1, time.Now(), "production", "", appStartItem(attrs))
	if err != nil {
		t.Fatal(err)
	}
	if len(row.FirstScreen) != 255 || len(row.Build) != 64 || len(row.DeviceModel) != 64 || len(row.OSVersion) != 64 {
		t.Errorf("bad truncation: %d %d %d %d", len(row.FirstScreen), len(row.Build), len(row.DeviceModel), len(row.OSVersion))
	}
	if row.RAMMB != nil {
		t.Errorf("ram_mb 0 should be nil, got %d", *row.RAMMB)
	}
	if row.Phases == nil || len(row.Phases) != 0 {
		t.Errorf("phases should be empty non-nil slice, got %#v", row.Phases)
	}
}

func TestEnvelopeEnvAndVersion(t *testing.T) {
	env, ver := EnvelopeEnvAndVersion(nil)
	if env != "production" || ver != "" {
		t.Errorf("nil ctx: %q %q", env, ver)
	}
	env, ver = EnvelopeEnvAndVersion(map[string]any{"environment": "staging", "release_version": "9"})
	if env != "staging" || ver != "9" {
		t.Errorf("release fallback: %q %q", env, ver)
	}
	_, ver = EnvelopeEnvAndVersion(map[string]any{"app_version": "1.2", "release_version": "9"})
	if ver != "1.2" {
		t.Errorf("app_version should win: %q", ver)
	}
}

func TestParseAppStartItemCleansStrings(t *testing.T) {
	// 63 ASCII bytes then a 3-byte rune: byte-slicing at 64 would split the rune.
	boundary := strings.Repeat("a", 63) + "€€"
	attrs := fmt.Sprintf(`{"v":1,"platform":"android","start_id":"s\u0000id","type":"cold","duration_ms":500,
		"first_screen":"Main\u0000Activity","build":%q,"device":{"model":%q,"os_version":"1\u00004","ram_mb":2000000000}}`,
		boundary, boundary)
	item := appStartItem(attrs)
	item.SessionID = "ses\x00_a"
	row, err := ParseAppStartItem(1, time.Now(), "prod\x00", boundary+"x", item)
	if err != nil {
		t.Fatal(err)
	}
	if row.StartID != "sid" || row.SessionID != "ses_a" || row.FirstScreen != "MainActivity" || row.OSVersion != "14" || row.Environment != "prod" {
		t.Errorf("NUL not stripped: %+v", row)
	}
	for name, v := range map[string]string{"build": row.Build, "model": row.DeviceModel, "version": row.AppVersion} {
		if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			t.Errorf("%s not clean: %q", name, v)
		}
		if utf8.RuneCountInString(v) != 64 {
			t.Errorf("%s runes = %d, want 64", name, utf8.RuneCountInString(v))
		}
	}
	if row.RAMMB != nil {
		t.Errorf("huge ram_mb should be nil, got %d", *row.RAMMB)
	}

	ok := fmt.Sprintf(`{"v":1,"platform":"android","start_id":"s","type":"cold","duration_ms":500,"device":{"ram_mb":%d}}`, 1048576)
	row, err = ParseAppStartItem(1, time.Now(), "production", "", appStartItem(ok))
	if err != nil || row.RAMMB == nil || *row.RAMMB != 1048576 {
		t.Errorf("ram_mb at the limit should be kept: %v %v", row.RAMMB, err)
	}
}

func TestParseAppStartItemRejectsStartIDEmptyAfterCleaning(t *testing.T) {
	attrs := `{"v":1,"platform":"android","start_id":"\u0000\u0000","type":"cold","duration_ms":500}`
	_, err := ParseAppStartItem(1, time.Now(), "production", "", appStartItem(attrs))
	if !errors.Is(err, ErrInvalidAppStart) {
		t.Fatalf("err = %v, want ErrInvalidAppStart", err)
	}
}

func TestParseAppStartItemEmptyEnvAfterCleaningIsProduction(t *testing.T) {
	row, err := ParseAppStartItem(1, time.Now(), "\x00\x00", "", appStartItem(appStartExampleAttrs))
	if err != nil {
		t.Fatal(err)
	}
	if row.Environment != "production" {
		t.Errorf("environment = %q, want production", row.Environment)
	}
	env, _ := EnvelopeEnvAndVersion(map[string]any{"environment": "\x00"})
	if env != "production" {
		t.Errorf("envelope env = %q, want production", env)
	}
	env, _ = EnvelopeEnvAndVersion(map[string]any{"environment": ""})
	if env != "production" {
		t.Errorf("empty envelope env = %q, want production", env)
	}
}
