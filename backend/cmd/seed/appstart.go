package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/appstart"
	"github.com/tiofani03/artracker/backend/internal/ingest"
)

const (
	appStartDays        = 30
	appStartPerDayMin   = 36 // 36..44 starts a day, about 40
	appStartPerDaySpan  = 9
	appStartProfiles    = 25
	appStartRegressedAt = 15 // days ago: older starts run 3.1.0, newer ones 3.2.0
	profileIntervalMs   = 10
	seedEnvironment     = "production"
	androidAppPackage   = "com.example.shop"
)

// seedAppStarts replaces the project's app start data with 30 days of demo starts (a startup regression
// in 3.2.0) and 25 symbolicated cold-start profiles. Only android and ios projects get any.
func seedAppStarts(ctx context.Context, pool *pgxpool.Pool, projectID int64, platform string, rng *rand.Rand, now time.Time) error {
	if platform != "android" && platform != "ios" {
		return nil
	}
	starts := genAppStarts(projectID, platform, rng, now)
	profiles := genAppStartProfiles(platform, starts, rng)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, table := range []string{"app_start_fn_stats", "app_start_profiles", "app_starts"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE project_id = $1`, projectID); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}

	batch := &pgx.Batch{}
	for i := range starts {
		r := &starts[i]
		phases, err := json.Marshal(r.Phases)
		if err != nil {
			return err
		}
		batch.Queue(`
			INSERT INTO app_starts
				(project_id, start_id, session_id, platform, environment, type, duration_ms, phases, first_screen,
				 app_version, build, os_version, device_model, ram_mb, trampoline, profiled, started_at, received_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
			r.ProjectID, r.StartID, r.SessionID, r.Platform, r.Environment, r.Type, r.DurationMs, phases,
			r.FirstScreen, r.AppVersion, r.Build, r.OSVersion, r.DeviceModel, r.RAMMB, r.Trampoline, r.Profiled,
			r.StartedAt, r.ReceivedAt)
	}
	for i := range profiles {
		p := &profiles[i]
		batch.Queue(`
			INSERT INTO app_start_profiles
				(project_id, start_id, platform, environment, app_version, build, image_uuids, raw, status,
				 attempts, next_attempt_at, symbolicated, received_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'done', 0, NULL, $9, $10)`,
			projectID, p.start.StartID, platform, p.start.Environment, p.start.AppVersion, p.start.Build,
			p.imageUUIDs, p.raw, p.symbolicated, p.start.ReceivedAt)
		for _, st := range p.stats {
			batch.Queue(`
				INSERT INTO app_start_fn_stats
					(project_id, start_id, platform, environment, app_version, function, is_app, self_ms,
					 total_ms, attributed_ms, started_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				projectID, p.start.StartID, platform, p.start.Environment, p.start.AppVersion, st.Function,
				st.IsApp, st.SelfMs, st.TotalMs, st.AttributedMs, p.start.StartedAt)
		}
	}
	br := tx.SendBatch(ctx, batch)
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("insert app start data: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("insert app start data: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	log.Printf("  -> Seeded %d app starts and %d profiles", len(starts), len(profiles))
	return nil
}

// --- starts ---

// phaseNames lists the platform's phase names per start type (spec section 4).
var phaseNames = map[string]map[string][]string{
	"android": {
		"cold": {"process_to_init", "application_on_create", "activity_create", "first_draw"},
		"warm": {"activity_create", "first_draw"},
		"hot":  {"activity_start", "first_draw"},
	},
	"ios": {
		"cold": {"pre_main", "pre_init", "did_finish_launching", "scene_activation", "first_draw"},
		"warm": {"did_finish_launching", "scene_activation", "first_draw"},
		"hot":  {"foreground_transition", "first_draw"},
	},
}

// phaseBaseMs is the typical phase length. Cold starts on 3.1.0 add up to about 1.4 s; on 3.2.0 the
// application_on_create / did_finish_launching phase grows by about 0.9 s, which makes them about 2.3 s.
func phaseBaseMs(platform, typ, phase, version string) float64 {
	regressed := version == "3.2.0"
	switch platform + "/" + typ + "/" + phase {
	case "android/cold/process_to_init":
		return 300
	case "android/cold/application_on_create":
		if regressed {
			return 1350
		}
		return 450
	case "android/cold/activity_create":
		return 420
	case "android/cold/first_draw":
		return 230
	case "android/warm/activity_create":
		return 350
	case "android/warm/first_draw":
		return 180
	case "android/hot/activity_start":
		return 110
	case "android/hot/first_draw":
		return 90

	case "ios/cold/pre_main":
		return 350
	case "ios/cold/pre_init":
		return 120
	case "ios/cold/did_finish_launching":
		if regressed {
			return 1280
		}
		return 380
	case "ios/cold/scene_activation":
		return 330
	case "ios/cold/first_draw":
		return 220
	case "ios/warm/did_finish_launching":
		return 200
	case "ios/warm/scene_activation":
		return 220
	case "ios/warm/first_draw":
		return 150
	case "ios/hot/foreground_transition":
		return 120
	case "ios/hot/first_draw":
		return 80
	}
	return 100
}

type demoDevice struct{ model, os string }

var demoDevices = map[string][]demoDevice{
	"android": {{"Pixel 8", "14"}, {"Galaxy S23", "14"}, {"Redmi Note 12", "13"}, {"Pixel 6a", "13"}, {"Galaxy A34", "12"}},
	"ios":     {{"iPhone15,2", "17.5"}, {"iPhone14,5", "17.4"}, {"iPhone13,2", "16.7"}, {"iPhone12,1", "16.6"}, {"iPhone16,1", "17.5"}},
}

var demoFirstScreens = map[string][]string{
	"android": {"HomeActivity", "ProductListActivity", "LoginActivity", "CartActivity"},
	"ios":     {"HomeViewController", "ProductListViewController", "LoginViewController", "CartViewController"},
}

var demoRAMMB = []int{3072, 6144, 12288}

func pickStartType(rng *rand.Rand) string {
	switch p := rng.Float64(); {
	case p < 0.60:
		return "cold"
	case p < 0.85:
		return "warm"
	default:
		return "hot"
	}
}

// startPhases draws each phase around its typical length (about 12% spread, median preserved) and
// returns phases whose sum is the start's duration.
func startPhases(platform, typ, version string, rng *rand.Rand) ([]ingest.AppStartPhase, int) {
	names := phaseNames[platform][typ]
	phases := make([]ingest.AppStartPhase, 0, len(names))
	total := 0
	for _, n := range names {
		ms := int(math.Round(phaseBaseMs(platform, typ, n, version) * (1 + rng.NormFloat64()*0.12)))
		if ms < 1 {
			ms = 1
		}
		phases = append(phases, ingest.AppStartPhase{Name: n, Ms: ms})
		total += ms
	}
	return phases, total
}

// genAppStarts returns about 40 starts a day for 30 days, oldest day first. 25 cold starts are marked
// profiled (10 on 3.1.0, 15 on 3.2.0), spread evenly through each version's cold starts.
func genAppStarts(projectID int64, platform string, rng *rand.Rand, now time.Time) []ingest.AppStartRow {
	var out []ingest.AppStartRow
	for day := appStartDays - 1; day >= 0; day-- {
		version, build := "3.2.0", "320"
		if day >= appStartRegressedAt {
			version, build = "3.1.0", "310"
		}
		n := appStartPerDayMin + rng.Intn(appStartPerDaySpan)
		for i := 0; i < n; i++ {
			startedAt := now.Add(-time.Duration(day)*24*time.Hour - time.Duration(rng.Int63n(int64(24*time.Hour))))
			typ := pickStartType(rng)
			phases, total := startPhases(platform, typ, version, rng)
			dev := demoDevices[platform][rng.Intn(len(demoDevices[platform]))]
			ram := demoRAMMB[rng.Intn(len(demoRAMMB))]
			idx := len(out)
			out = append(out, ingest.AppStartRow{
				ProjectID:   projectID,
				StartID:     fmt.Sprintf("seed-%s-%05d", platform, idx),
				SessionID:   fmt.Sprintf("ses_seed_%05d", idx),
				Platform:    platform,
				Environment: seedEnvironment,
				Type:        typ,
				DurationMs:  total,
				Phases:      phases,
				FirstScreen: demoFirstScreens[platform][rng.Intn(len(demoFirstScreens[platform]))],
				AppVersion:  version,
				Build:       build,
				OSVersion:   dev.os,
				DeviceModel: dev.model,
				RAMMB:       &ram,
				Trampoline:  platform == "android" && typ == "cold" && rng.Float64() < 0.2,
				StartedAt:   startedAt,
				ReceivedAt:  startedAt.Add(time.Duration(2+rng.Intn(20)) * time.Second),
			})
		}
	}

	byVersion := map[string][]int{}
	for i := range out {
		if out[i].Type == "cold" {
			byVersion[out[i].AppVersion] = append(byVersion[out[i].AppVersion], i)
		}
	}
	for version, want := range map[string]int{"3.1.0": appStartProfiles * 2 / 5, "3.2.0": appStartProfiles * 3 / 5} {
		cold := byVersion[version]
		for k := 0; k < want && k < len(cold); k++ {
			out[cold[k*len(cold)/want]].Profiled = true
		}
	}
	return out
}

// --- profiles ---

type seededProfile struct {
	start        ingest.AppStartRow
	imageUUIDs   []string
	raw          []byte // gzip of the upload JSON, as the ingest endpoint stores it
	symbolicated []byte
	stats        []appstart.FnStat
}

func genAppStartProfiles(platform string, starts []ingest.AppStartRow, rng *rand.Rand) []seededProfile {
	var out []seededProfile
	for _, s := range starts {
		if !s.Profiled {
			continue
		}
		p := genProfile(platform, s, rng)
		sym := appstart.Symbolicate(p, demoResolver{platform: platform, images: p.Images})
		symJSON, err := json.Marshal(sym)
		if err != nil {
			panic(err) // plain structs: cannot fail
		}
		upload, err := json.Marshal(ingest.AppStartProfileUpload{
			SDK:     ingest.SDKMeta{Name: "artracker-" + platform, Version: "1.0.0"},
			Context: map[string]any{"environment": s.Environment, "app_version": s.AppVersion, "build": s.Build},
			Profile: p,
		})
		if err != nil {
			panic(err)
		}
		var gz bytes.Buffer
		zw := gzip.NewWriter(&gz)
		_, _ = zw.Write(upload)
		_ = zw.Close()
		out = append(out, seededProfile{
			start:        s,
			imageUUIDs:   p.ImageUUIDs(),
			raw:          gz.Bytes(),
			symbolicated: symJSON,
			stats:        appstart.ComputeFnStats(sym),
		})
	}
	return out
}

// demoResolver stands in for the real Android/iOS resolvers: the demo frames already carry their names.
type demoResolver struct {
	platform string
	images   []ingest.ProfileImage
}

func (demoResolver) MissingSymbols() bool { return false }

func (r demoResolver) Resolve(f ingest.ProfileFrame, _ bool) []appstart.ResolvedFrame {
	if r.platform == "ios" {
		return []appstart.ResolvedFrame{{Function: f.Sym, IsApp: r.images[*f.I].App, Resolved: true}}
	}
	line := 0
	if f.L != nil {
		line = *f.L
	}
	return []appstart.ResolvedFrame{{
		Function: f.C + "." + f.M,
		File:     f.F,
		Line:     line,
		IsApp:    strings.HasPrefix(f.C, androidAppPackage+"."),
		Resolved: true,
	}}
}

// segment is one stretch of the start during which one stack was sampled.
type segment struct {
	stack []int // root-first frame indexes
	ms    int
}

// profileBuilder collects the unique frames of a profile and the segments that sample them.
type profileBuilder struct {
	platform string
	frames   []ingest.ProfileFrame
	byName   map[string]int
	images   []ingest.ProfileImage
	imageIdx map[string]int
	segments []segment
}

func newProfileBuilder(platform string) *profileBuilder {
	return &profileBuilder{platform: platform, byName: map[string]int{}, imageIdx: map[string]int{}}
}

// frame returns the index of a frame, creating it on first use. name is "pkg.Class.method" on Android and
// "Image|Symbol" on iOS.
func (b *profileBuilder) frame(name string) int {
	if i, ok := b.byName[name]; ok {
		return i
	}
	i := len(b.frames)
	n := uint64(i + 1)
	if b.platform == "android" {
		dot := strings.LastIndex(name, ".")
		cls, method := name[:dot], name[dot+1:]
		line := 10 + i*7
		file := cls[strings.LastIndex(cls, ".")+1:] + ".kt"
		b.frames = append(b.frames, ingest.ProfileFrame{C: cls, M: method, F: file, L: &line})
	} else {
		image, sym, _ := strings.Cut(name, "|")
		img := b.image(image)
		addr := 0x100000000 + uint64(img)*0x1000000 + n*0x40
		b.frames = append(b.frames, ingest.ProfileFrame{A: fmt.Sprintf("0x%x", addr), I: &img, Sym: sym})
	}
	b.byName[name] = i
	return i
}

func (b *profileBuilder) image(name string) int {
	if i, ok := b.imageIdx[name]; ok {
		return i
	}
	i := len(b.images)
	b.images = append(b.images, ingest.ProfileImage{
		UUID: fmt.Sprintf("%08X-1111-4222-8333-%012X", 0xA0000000+uint32(i), 0xC0FFEE000000+uint64(i)),
		Name: name,
		Load: fmt.Sprintf("0x%x", 0x100000000+uint64(i)*0x1000000),
		App:  name == "Shop",
	})
	b.imageIdx[name] = i
	return i
}

// add queues a segment of ms milliseconds sampled with the given root-first stack (frame names).
func (b *profileBuilder) add(ms int, stack ...string) {
	if ms <= 0 {
		return
	}
	idx := make([]int, len(stack))
	for i, n := range stack {
		idx[i] = b.frame(n)
	}
	b.segments = append(b.segments, segment{stack: idx, ms: ms})
}

// build samples the segments back to back every profileIntervalMs.
func (b *profileBuilder) build(s ingest.AppStartRow) ingest.AppStartProfile {
	p := ingest.AppStartProfile{
		V:          1,
		StartID:    s.StartID,
		Platform:   b.platform,
		IntervalMs: profileIntervalMs,
		T0:         "process_start",
		Frames:     b.frames,
		Images:     b.images,
		Stacks:     make([][]int, len(b.segments)),
		Samples:    []ingest.ProfileSample{},
	}
	if b.platform == "android" {
		p.AppPackages = []string{androidAppPackage}
	}
	end, t := 0, int64(0)
	for i, seg := range b.segments {
		p.Stacks[i] = seg.stack
		end += seg.ms
		for ; t < int64(end); t += profileIntervalMs {
			p.Samples = append(p.Samples, ingest.ProfileSample{T: t, S: i})
		}
	}
	return p
}

// join returns base followed by more as a new slice.
func join(base []string, more ...string) []string {
	return append(append(make([]string, 0, len(base)+len(more)), base...), more...)
}

// share returns frac of ms, rounded.
func share(ms int, frac float64) int { return int(math.Round(float64(ms) * frac)) }

// phaseMs returns the named phase's length in the start.
func phaseMs(s ingest.AppStartRow, name string) int {
	for _, p := range s.Phases {
		if p.Name == name {
			return p.Ms
		}
	}
	return 0
}

// genProfile builds a startup profile for one cold start. On 3.2.0 the slow init phase is dominated by
// database initialization; on 3.1.0 it is split between several initializers.
func genProfile(platform string, s ingest.AppStartRow, rng *rand.Rand) ingest.AppStartProfile {
	// Fractions of the init phase spent in the database, analytics and the remote SDK; the rest is the
	// initializer's own work.
	db, analytics, remote := 0.27, 0.22, 0.30
	if s.AppVersion == "3.2.0" {
		db, analytics, remote = 0.74, 0.09, 0.12
	}
	db += rng.Float64()*0.04 - 0.02

	b := newProfileBuilder(platform)
	if platform == "android" {
		genAndroidProfile(b, s, db, analytics, remote)
	} else {
		genIOSProfile(b, s, db, analytics, remote)
	}
	return b.build(s)
}

func genAndroidProfile(b *profileBuilder, s ingest.AppStartRow, db, analytics, remote float64) {
	const app = androidAppPackage + "."
	root := []string{"com.android.internal.os.ZygoteInit.main", "android.app.ActivityThread.main"}
	bind := join(root, "android.app.ActivityThread.handleBindApplication")
	onCreate := join(bind, "android.app.Instrumentation.callApplicationOnCreate", app+"App.onCreate")

	b.add(phaseMs(s, "process_to_init"), join(bind, "android.app.LoadedApk.makeApplication")...)

	initMs := phaseMs(s, "application_on_create")
	dbMs, anMs, fbMs := share(initMs, db), share(initMs, analytics), share(initMs, remote)
	provideDB := join(onCreate, app+"di.AppModule.provideDatabase")
	b.add(fbMs, join(onCreate, "com.google.firebase.FirebaseApp.initializeApp")...)
	b.add(dbMs*2/5, provideDB...)
	b.add(dbMs-dbMs*2/5, join(provideDB, "androidx.room.RoomDatabase.open", "android.database.sqlite.SQLiteConnection.nativeOpen")...)
	b.add(anMs, join(onCreate, app+"App.initAnalytics")...)
	b.add(initMs-fbMs-dbMs-anMs, onCreate...)

	create := phaseMs(s, "activity_create")
	launch := join(root, "android.app.ActivityThread.performLaunchActivity", "android.app.Instrumentation.callActivityOnCreate", app+"ui.MainActivity.onCreate")
	b.add(share(create, 0.55), launch...)
	b.add(create-share(create, 0.55), join(launch, "android.view.LayoutInflater.inflate")...)

	b.add(phaseMs(s, "first_draw"), join(root, "android.view.ViewRootImpl.performTraversals", "android.view.View.draw")...)
}

func genIOSProfile(b *profileBuilder, s ingest.AppStartRow, db, analytics, remote float64) {
	const (
		shop  = "Shop|"
		uikit = "UIKitCore|"
	)
	start := []string{"dyld|dyld4::start", shop + "Shop.main"}
	uiMain := join(start, uikit+"UIApplicationMain")
	didLaunch := join(uiMain,
		uikit+"-[UIApplication _handleDelegateCallbacksWithOptions:isSuspended:restoreState:]",
		shop+"Shop.AppDelegate.application(_:didFinishLaunchingWithOptions:)")

	b.add(phaseMs(s, "pre_main"), "dyld|dyld4::start")
	b.add(phaseMs(s, "pre_init"), start...)

	initMs := phaseMs(s, "did_finish_launching")
	dbMs, anMs, rcMs := share(initMs, db), share(initMs, analytics), share(initMs, remote)
	configureDB := join(didLaunch, shop+"Shop.AppDelegate.configureDatabase()")
	b.add(dbMs*2/5, configureDB...)
	b.add(dbMs-dbMs*2/5, join(configureDB, "libsqlite3.dylib|libsqlite3.dylib")...)
	b.add(rcMs, join(didLaunch, shop+"Shop.RemoteConfig.fetchSync()", "libsystem_kernel.dylib|semaphore_wait_trap")...)
	b.add(anMs, join(didLaunch, shop+"Shop.AppDelegate.configureAnalytics()")...)
	b.add(initMs-dbMs-anMs-rcMs, didLaunch...)

	activate := phaseMs(s, "scene_activation")
	activation := join(uiMain, uikit+"-[UIScene _activate]")
	b.add(share(activate, 0.6), join(activation, shop+"Shop.SceneDelegate.scene(_:willConnectTo:options:)", shop+"Shop.HomeViewController.viewDidLoad()")...)
	b.add(activate-share(activate, 0.6), activation...)

	b.add(phaseMs(s, "first_draw"), join(uiMain, "QuartzCore|CA::Transaction::commit()")...)
}
