package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/tiofani03/artracker/backend/internal/ingest"
)

var testNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func p50(v []int) int {
	sort.Ints(v)
	return v[len(v)/2]
}

func TestGenAppStartsShape(t *testing.T) {
	for _, platform := range []string{"android", "ios"} {
		t.Run(platform, func(t *testing.T) {
			starts := genAppStarts(7, platform, rand.New(rand.NewSource(1)), testNow)
			if n := len(starts); n < 30*36 || n > 30*44 {
				t.Fatalf("%d starts, want about 40 a day", n)
			}
			counts := map[string]int{}
			ids := map[string]bool{}
			profiled := 0
			for _, s := range starts {
				counts[s.Type]++
				if ids[s.StartID] {
					t.Fatalf("duplicate start_id %s", s.StartID)
				}
				ids[s.StartID] = true

				sum := 0
				for _, ph := range s.Phases {
					sum += ph.Ms
				}
				if sum != s.DurationMs {
					t.Fatalf("%s %s: phases sum %d != duration %d", s.StartID, s.Type, sum, s.DurationMs)
				}
				var names []string
				for _, ph := range s.Phases {
					names = append(names, ph.Name)
				}
				want := phaseNames[platform][s.Type]
				if len(names) != len(want) {
					t.Fatalf("%s %s phases %v, want %v", s.StartID, s.Type, names, want)
				}
				for i := range want {
					if names[i] != want[i] {
						t.Fatalf("%s %s phases %v, want %v", s.StartID, s.Type, names, want)
					}
				}
				if s.RAMMB == nil || (*s.RAMMB != 3072 && *s.RAMMB != 6144 && *s.RAMMB != 12288) {
					t.Fatalf("ram_mb %v", s.RAMMB)
				}
				if s.Environment != "production" {
					t.Fatalf("environment %q", s.Environment)
				}
				if s.Profiled {
					profiled++
					if s.Type != "cold" {
						t.Fatalf("profiled start %s is %s", s.StartID, s.Type)
					}
				}
			}
			total := float64(len(starts))
			for typ, lo := range map[string][2]float64{"cold": {0.55, 0.65}, "warm": {0.20, 0.30}, "hot": {0.10, 0.20}} {
				if f := float64(counts[typ]) / total; f < lo[0] || f > lo[1] {
					t.Errorf("%s share %.2f outside %v", typ, f, lo)
				}
			}
			if profiled != appStartProfiles {
				t.Errorf("%d profiled starts, want %d", profiled, appStartProfiles)
			}
		})
	}
}

func TestColdRegression(t *testing.T) {
	for _, platform := range []string{"android", "ios"} {
		t.Run(platform, func(t *testing.T) {
			starts := genAppStarts(7, platform, rand.New(rand.NewSource(2)), testNow)
			cold := map[string][]int{}
			slow := map[string][]int{} // the init phase that carries the regression
			slowPhase := map[string]string{"android": "application_on_create", "ios": "did_finish_launching"}[platform]
			for _, s := range starts {
				if s.Type != "cold" {
					continue
				}
				cold[s.AppVersion] = append(cold[s.AppVersion], s.DurationMs)
				slow[s.AppVersion] = append(slow[s.AppVersion], phaseMs(s, slowPhase))
			}
			if len(cold) != 2 {
				t.Fatalf("versions %v, want exactly 3.1.0 and 3.2.0", cold)
			}
			old, regressed := p50(cold["3.1.0"]), p50(cold["3.2.0"])
			if old < 1300 || old > 1500 {
				t.Errorf("3.1.0 cold p50 = %d, want about 1400", old)
			}
			if regressed < 2200 || regressed > 2400 {
				t.Errorf("3.2.0 cold p50 = %d, want about 2300", regressed)
			}
			if d := regressed - old; d < 800 || d > 1000 {
				t.Errorf("regression = %d ms, want about 900", d)
			}
			if d := p50(slow["3.2.0"]) - p50(slow["3.1.0"]); d < 800 || d > 1000 {
				t.Errorf("%s grew by %d ms, want about 900 (the regression lives there)", slowPhase, d)
			}
		})
	}
}

func TestProfiles(t *testing.T) {
	topApp := map[string]string{
		"android": "com.example.shop.di.AppModule.provideDatabase",
		"ios":     "Shop.AppDelegate.configureDatabase()",
	}
	for _, platform := range []string{"android", "ios"} {
		t.Run(platform, func(t *testing.T) {
			rng := rand.New(rand.NewSource(3))
			starts := genAppStarts(7, platform, rng, testNow)
			profiles := genAppStartProfiles(platform, starts, rng)
			if len(profiles) != appStartProfiles {
				t.Fatalf("%d profiles, want %d", len(profiles), appStartProfiles)
			}

			attributed := map[string]int{}
			isApp := map[string]bool{}
			for _, p := range profiles {
				// The raw blob is exactly what the ingest endpoint stores: gzip of a valid upload.
				zr, err := gzip.NewReader(bytes.NewReader(p.raw))
				if err != nil {
					t.Fatal(err)
				}
				raw, err := io.ReadAll(zr)
				if err != nil {
					t.Fatal(err)
				}
				up, err := ingest.ParseAppStartProfile(raw)
				if err != nil {
					t.Fatalf("%s: %v", p.start.StartID, err)
				}
				if up.Profile.StartID != p.start.StartID {
					t.Fatalf("profile start_id %q != %q", up.Profile.StartID, p.start.StartID)
				}
				if platform == "ios" && len(p.imageUUIDs) == 0 {
					t.Fatal("ios profile has no image uuids")
				}
				if len(p.symbolicated) == 0 || len(p.stats) == 0 {
					t.Fatalf("%s: empty symbolicated/stats", p.start.StartID)
				}
				for _, st := range p.stats {
					attributed[st.Function] += st.AttributedMs
					isApp[st.Function] = st.IsApp
				}
			}

			var top string
			for fn, ms := range attributed {
				if ms > attributed[top] {
					top = fn
				}
			}
			if top != topApp[platform] {
				t.Errorf("top attributed function = %q, want %q (%v)", top, topApp[platform], attributed)
			}

			wantApp := map[string]map[string]bool{
				"android": {
					"com.example.shop.di.AppModule.provideDatabase": true,
					"com.example.shop.App.initAnalytics":            true,
					"androidx.room.RoomDatabase.open":               false,
					"com.google.firebase.FirebaseApp.initializeApp": false,
				},
				"ios": {
					"Shop.AppDelegate.configureDatabase()": true,
					"Shop.RemoteConfig.fetchSync()":        true,
					"libsqlite3.dylib":                     false,
				},
			}[platform]
			for fn, want := range wantApp {
				got, ok := isApp[fn]
				if !ok {
					t.Errorf("function %q missing from stats", fn)
				} else if got != want {
					t.Errorf("%q is_app = %v, want %v", fn, got, want)
				}
			}
		})
	}
}
