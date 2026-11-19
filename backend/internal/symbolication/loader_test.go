package symbolication

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/db"
)

// testDB connects to TEST_DATABASE_URL, migrates, and truncates the shared tables.
// It skips the test when TEST_DATABASE_URL is unset.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE users, organizations, organization_members, projects, api_keys, refresh_tokens RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

func loaderProject(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	ctx := context.Background()
	var orgID, pid int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Acme','acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'P','p','android') RETURNING id`, orgID).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return pid
}

func insertArtifact(t *testing.T, pool *pgxpool.Pool, project int64, typ, version, build, uuid string, content []byte) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO project_release_artifacts(project_id, version, build_number, artifact_type, file_name,
			content, checksum_sha256, debug_uuid, size_bytes)
		VALUES ($1, $2, $3, $4, 'f', $5, 'x', $6, $7)
		RETURNING id`, project, version, build, typ, content, uuid, int64(len(content))).Scan(&id)
	if err != nil {
		t.Fatalf("insert artifact: %v", err)
	}
	return id
}

func mappingFor(class string) []byte {
	return []byte(class + " -> a.b.c:\n    10:25:void run() -> a\n")
}

func TestLoaderProguardPrefersBuild(t *testing.T) {
	pool := testDB(t)
	pid := loaderProject(t, pool)
	insertArtifact(t, pool, pid, "proguard_mapping", "3.2.0", "1", "", mappingFor("com.example.BuildOne"))
	insertArtifact(t, pool, pid, "proguard_mapping", "3.2.0", "2", "", mappingFor("com.example.BuildTwo"))
	l := NewLoader(pool, 8)
	ctx := context.Background()

	r, err := l.Proguard(ctx, pid, "3.2.0", "1")
	if err != nil || r == nil {
		t.Fatalf("Proguard build 1: %v, %v", r, err)
	}
	if got := r.RetraceClass("a.b.c"); got != "com.example.BuildOne" {
		t.Errorf("build 1 -> %q, want com.example.BuildOne", got)
	}

	// Unknown build falls back to the newest upload for the version.
	r, err = l.Proguard(ctx, pid, "3.2.0", "99")
	if err != nil || r == nil {
		t.Fatalf("Proguard build 99: %v, %v", r, err)
	}
	if got := r.RetraceClass("a.b.c"); got != "com.example.BuildTwo" {
		t.Errorf("build 99 -> %q, want newest com.example.BuildTwo", got)
	}

	// No build at all also means newest.
	r, _ = l.Proguard(ctx, pid, "3.2.0", "")
	if r == nil || r.RetraceClass("a.b.c") != "com.example.BuildTwo" {
		t.Errorf("empty build should pick newest")
	}

	// Nothing uploaded: (nil, nil).
	if r, err := l.Proguard(ctx, pid, "9.9.9", ""); r != nil || err != nil {
		t.Errorf("missing artifact: got %v, %v; want nil, nil", r, err)
	}
	if r, err := l.Proguard(ctx, pid, "", ""); r != nil || err != nil {
		t.Errorf("empty version: got %v, %v; want nil, nil", r, err)
	}
}

func TestLoaderCachesByArtifactID(t *testing.T) {
	pool := testDB(t)
	pid := loaderProject(t, pool)
	insertArtifact(t, pool, pid, "proguard_mapping", "1.0.0", "", "", mappingFor("com.example.Old"))
	l := NewLoader(pool, 8)
	var parses atomic.Int32
	l.parseHook = func(string) { parses.Add(1) }
	ctx := context.Background()

	first, err := l.Proguard(ctx, pid, "1.0.0", "")
	if err != nil || first == nil {
		t.Fatalf("first: %v, %v", first, err)
	}
	second, _ := l.Proguard(ctx, pid, "1.0.0", "")
	if second != first {
		t.Errorf("second call should return the cached retracer")
	}
	if n := parses.Load(); n != 1 {
		t.Fatalf("parses after two calls = %d, want 1", n)
	}

	insertArtifact(t, pool, pid, "proguard_mapping", "1.0.0", "", "", mappingFor("com.example.New"))
	third, _ := l.Proguard(ctx, pid, "1.0.0", "")
	if third == first || third.RetraceClass("a.b.c") != "com.example.New" {
		t.Errorf("new upload must not be served stale")
	}
	if n := parses.Load(); n != 2 {
		t.Errorf("parses after new upload = %d, want 2", n)
	}
}

func TestLoaderEvictsLRU(t *testing.T) {
	pool := testDB(t)
	pid := loaderProject(t, pool)
	insertArtifact(t, pool, pid, "proguard_mapping", "1.0.0", "", "", mappingFor("com.example.A"))
	insertArtifact(t, pool, pid, "proguard_mapping", "2.0.0", "", "", mappingFor("com.example.B"))
	l := NewLoader(pool, 1)
	var parses atomic.Int32
	l.parseHook = func(string) { parses.Add(1) }
	ctx := context.Background()

	if r, _ := l.Proguard(ctx, pid, "1.0.0", ""); r == nil {
		t.Fatal("nil A")
	}
	if r, _ := l.Proguard(ctx, pid, "2.0.0", ""); r == nil {
		t.Fatal("nil B")
	}
	if n := l.Len(); n != 1 {
		t.Fatalf("cache len = %d, want 1", n)
	}
	// A was evicted, so it parses again.
	if r, _ := l.Proguard(ctx, pid, "1.0.0", ""); r == nil {
		t.Fatal("nil A again")
	}
	if n := parses.Load(); n != 3 {
		t.Errorf("parses = %d, want 3 (A evicted by B)", n)
	}
}

func TestLoaderEvictionClosesIOSSymbolicator(t *testing.T) {
	fix := requireDarwinFixture(t)
	sym, err := NewIOSSymbolicator(fix.data)
	if err != nil {
		t.Fatal(err)
	}
	l := NewLoader(nil, 1)
	var closed []int64
	l.closeHook = func(id int64) { closed = append(closed, id) }

	l.put(1, sym)
	l.put(2, "not closable") // evicts the symbolicator
	if len(closed) != 1 || closed[0] != 1 {
		t.Fatalf("closed = %v, want [1]", closed)
	}
	l.put(3, "also not closable") // evicts a value that is not an *IOSSymbolicator
	if len(closed) != 1 {
		t.Errorf("non-iOS eviction must not be reported as a close, got %v", closed)
	}
}

func TestLoaderPutKeepsFirstResult(t *testing.T) {
	l := NewLoader(nil, 4)
	first := l.put(7, "first")
	second := l.put(7, "second")
	if first != "first" || second != "first" {
		t.Errorf("put returned %v then %v; want the first stored value both times", first, second)
	}
	if l.Len() != 1 {
		t.Errorf("len = %d, want 1", l.Len())
	}
}

func TestLoaderIOSByUUIDCaseInsensitive(t *testing.T) {
	sym := requireDarwinFixture(t)
	pool := testDB(t)
	pid := loaderProject(t, pool)
	insertArtifact(t, pool, pid, "dsym", "1.0.0", "", "abcdef12-3456-7890-abcd-ef1234567890", sym.data)
	l := NewLoader(pool, 8)
	ctx := context.Background()

	got, err := l.IOSByUUID(ctx, pid, "ABCDEF12-3456-7890-ABCD-EF1234567890")
	if err != nil || got == nil {
		t.Fatalf("IOSByUUID upper: %v, %v", got, err)
	}
	again, _ := l.IOSByUUID(ctx, pid, "abcdef12-3456-7890-abcd-ef1234567890")
	if again != got {
		t.Errorf("lower-case lookup should hit the same cached symbolicator")
	}
	if byVer, _ := l.IOSByVersion(ctx, pid, "1.0.0"); byVer != got {
		t.Errorf("IOSByVersion should resolve to the same cached artifact")
	}
	if none, err := l.IOSByUUID(ctx, pid, "00000000-0000-0000-0000-000000000000"); none != nil || err != nil {
		t.Errorf("unknown uuid: got %v, %v; want nil, nil", none, err)
	}
}

func TestLoaderParseFailureCachesNothing(t *testing.T) {
	pool := testDB(t)
	pid := loaderProject(t, pool)
	insertArtifact(t, pool, pid, "dsym", "1.0.0", "", "", []byte("not a macho"))
	l := NewLoader(pool, 8)

	r, err := l.IOSByVersion(context.Background(), pid, "1.0.0")
	if err == nil || r != nil {
		t.Fatalf("want parse error, got %v, %v", r, err)
	}
	if n := l.Len(); n != 0 {
		t.Errorf("cache len = %d after failed parse, want 0", n)
	}
}

func TestLoaderConcurrent(t *testing.T) {
	pool := testDB(t)
	pid := loaderProject(t, pool)
	for _, v := range []string{"1.0.0", "2.0.0", "3.0.0"} {
		insertArtifact(t, pool, pid, "proguard_mapping", v, "", "", mappingFor("com.example.V"+strings.ReplaceAll(v, ".", "_")))
	}
	l := NewLoader(pool, 2) // smaller than the working set to force eviction
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				v := []string{"1.0.0", "2.0.0", "3.0.0"}[(i+j)%3]
				r, err := l.Proguard(ctx, pid, v, "")
				if err != nil || r == nil {
					t.Errorf("Proguard(%s): %v, %v", v, r, err)
					return
				}
				want := "com.example.V" + strings.ReplaceAll(v, ".", "_")
				if got := r.RetraceClass("a.b.c"); got != want {
					t.Errorf("Proguard(%s) -> %q, want %q", v, got, want)
				}
			}
		}()
	}
	wg.Wait()
	if n := l.Len(); n > 2 {
		t.Errorf("cache len = %d, exceeds capacity 2", n)
	}
}
