package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	testBotToken  = "987654321:AAFakeTokenForTestsOnly_abcdefghijk-XYZ"
	testBotToken2 = "555666777:BBAnotherFakeTokenForTests_lmnopqrs-T1"
)

func testSealer(t *testing.T, keyID string) *LocalSealer {
	t.Helper()
	key := bytes.Repeat([]byte{0x42}, 32)
	sl, err := NewLocalSealer(key, keyID)
	if err != nil {
		t.Fatal(err)
	}
	return sl
}

// tenantService is a tenant-mode service whose Bot API is the strict server.
func tenantService(t *testing.T, pool *pgxpool.Pool) (*Service, *strictTelegram) {
	t.Helper()
	srv := newStrictTelegramServer(t)
	s := NewService(pool, nil, "http://app").WithMode(ModeTenant).WithSealer(testSealer(t, "k1")).WithAPIBase(srv.URL)
	return s, srv
}

func seedUser(t *testing.T, pool *pgxpool.Pool, ssoID string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users(email, sso_provider, sso_id) VALUES ($1,'google',$2) RETURNING id`,
		ssoID+"@example.com", ssoID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedOrg(t *testing.T, pool *pgxpool.Pool, slug string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO organizations(name, slug) VALUES ($1,$1) RETURNING id`, slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

type botRow struct {
	cipher       []byte
	keyID        string
	botID        int64
	username     string
	status       string
	reason       string
	level        string
	createdBy    *int64
	hasValidated bool
}

func readBotRow(t *testing.T, pool *pgxpool.Pool, orgID int64) (botRow, bool) {
	t.Helper()
	var r botRow
	err := pool.QueryRow(context.Background(), `
SELECT token_ciphertext, key_id, bot_id, bot_username, status, status_reason, detail_level, created_by, last_validated_at IS NOT NULL
FROM alert_bots WHERE org_id = $1`, orgID).Scan(&r.cipher, &r.keyID, &r.botID, &r.username, &r.status, &r.reason, &r.level, &r.createdBy, &r.hasValidated)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return r, true
}

func TestMigration0012(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	orgID, pid := seedProject(t, pool)

	check := func(t *testing.T) {
		t.Helper()
		if _, err := pool.Exec(ctx, `DELETE FROM alert_channels`); err != nil {
			t.Fatal(err)
		}
		// Existing-style insert: linked, empty destination.
		var origin, dest string
		if err := pool.QueryRow(ctx,
			`INSERT INTO alert_channels(project_id, telegram_chat_id, title) VALUES ($1, 111, 'Ops') RETURNING origin, destination`,
			pid).Scan(&origin, &dest); err != nil {
			t.Fatal(err)
		}
		if origin != "linked" || dest != "" {
			t.Fatalf("origin=%q dest=%q", origin, dest)
		}
		// Manual channels with NULL chat ids coexist in one project.
		if _, err := pool.Exec(ctx,
			`INSERT INTO alert_channels(project_id, origin, destination, title) VALUES ($1,'manual','@news','News')`, pid); err != nil {
			t.Fatalf("manual insert: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO alert_channels(project_id, origin, destination, title) VALUES ($1,'manual','-100555','Group')`, pid); err != nil {
			t.Fatalf("second manual insert: %v", err)
		}
		// Duplicate manual destination in the same project fails.
		_, err := pool.Exec(ctx,
			`INSERT INTO alert_channels(project_id, origin, destination, title) VALUES ($1,'manual','@news','Dup')`, pid)
		if pgCode(err) != "23505" {
			t.Fatalf("duplicate manual: want 23505, got %v", err)
		}
		// The same destination text on linked rows is unconstrained.
		for _, chat := range []int64{222, 333} {
			if _, err := pool.Exec(ctx,
				`INSERT INTO alert_channels(project_id, telegram_chat_id, origin, destination) VALUES ($1,$2,'linked','@news')`, pid, chat); err != nil {
				t.Fatalf("linked with destination: %v", err)
			}
		}
		_, err = pool.Exec(ctx, `INSERT INTO alert_channels(project_id, telegram_chat_id, origin) VALUES ($1, 1, 'x')`, pid)
		if pgCode(err) != "23514" {
			t.Fatalf("origin x: want 23514, got %v", err)
		}
		// alert_bots checks.
		_, err = pool.Exec(ctx,
			`INSERT INTO alert_bots(org_id, token_ciphertext, key_id, bot_username, status) VALUES ($1,'\x00','k','b','x')`, orgID)
		if pgCode(err) != "23514" {
			t.Fatalf("status x: want 23514, got %v", err)
		}
		_, err = pool.Exec(ctx,
			`INSERT INTO alert_bots(org_id, token_ciphertext, key_id, bot_username, detail_level) VALUES ($1,'\x00','k','b','x')`, orgID)
		if pgCode(err) != "23514" {
			t.Fatalf("detail_level x: want 23514, got %v", err)
		}
	}
	check(t)

	// Running the migration SQL again is harmless.
	body, err := os.ReadFile("../db/migrations/0012_alert_bots.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, string(body), pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("re-run %d: %v", i, err)
		}
	}
	check(t)
	var nIdx int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_indexes WHERE tablename='alert_channels' AND indexdef LIKE '%destination%'`).Scan(&nIdx); err != nil {
		t.Fatal(err)
	}
	if nIdx != 1 {
		t.Fatalf("destination indexes = %d, want 1", nIdx)
	}

	// Deleting the organisation cascades the bot row.
	if _, err := pool.Exec(ctx,
		`INSERT INTO alert_bots(org_id, token_ciphertext, key_id, bot_username) VALUES ($1,'\x00','k','b')`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_bots`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("bot rows after org delete = %d", n)
	}
}

func TestNullChatChannelsDoNotBreakExistingReaders(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	_, pid := seedProject(t, pool)
	var linkedID, manualID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO alert_channels(project_id, telegram_chat_id, title) VALUES ($1, 111, 'Ops') RETURNING id`, pid).Scan(&linkedID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO alert_channels(project_id, origin, destination, title) VALUES ($1,'manual','@news','News') RETURNING id`, pid).Scan(&manualID); err != nil {
		t.Fatal(err)
	}
	n := &fakeNotifier{}
	s := NewService(pool, n, "http://app").WithBotUsername("TestBot")
	chs, err := s.enabledChannels(ctx, pid, originLinked)
	if err != nil {
		t.Fatalf("enabledChannels: %v", err)
	}
	if len(chs) != 1 || chs[0].id != linkedID || chs[0].dest != "111" {
		t.Fatalf("channels = %+v", chs)
	}
	if err := s.SendTest(ctx, pid, manualID); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("SendTest manual: %v", err)
	}
	if err := s.SendTest(ctx, pid, linkedID); err != nil {
		t.Fatalf("SendTest linked: %v", err)
	}
}

func TestSetBotValidatesWithGetMeOnly(t *testing.T) {
	pool := testDB(t)
	orgID, _ := seedProject(t, pool)
	s, srv := tenantService(t, pool)
	v, err := s.SetBot(context.Background(), orgID, 0, testBotToken, "")
	if err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].Method != "getMe" || reqs[0].Token != testBotToken {
		t.Fatalf("requests = %+v", reqs)
	}
	if !v.Configured || v.BotUsername != "TestBot" || v.Status != "active" || v.DetailLevel != DetailMinimal || v.UpdatedAt == nil {
		t.Fatalf("view = %+v", v)
	}
	r, ok := readBotRow(t, pool, orgID)
	if !ok || r.botID != 424242 || r.username != "TestBot" || r.createdBy != nil || !r.hasValidated {
		t.Fatalf("row = %+v ok=%v", r, ok)
	}
}

func TestSetBotStoresCiphertext(t *testing.T) {
	pool := testDB(t)
	orgID, _ := seedProject(t, pool)
	s, _ := tenantService(t, pool)
	if _, err := s.SetBot(context.Background(), orgID, 0, testBotToken, DetailFull); err != nil {
		t.Fatal(err)
	}
	r, ok := readBotRow(t, pool, orgID)
	if !ok {
		t.Fatal("no row")
	}
	if bytes.Contains(r.cipher, []byte(testBotToken)) || bytes.Contains(r.cipher, []byte(testBotToken[:8])) {
		t.Fatal("ciphertext contains the token")
	}
	if r.keyID != "k1" || r.level != "full" {
		t.Fatalf("key_id=%q level=%q", r.keyID, r.level)
	}
	// Every text column too.
	var all string
	if err := pool.QueryRow(context.Background(), `SELECT row_to_json(b)::text FROM alert_bots b WHERE org_id=$1`, orgID).Scan(&all); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(all, testBotToken[:8]) {
		t.Fatal("row contains the token prefix")
	}
	pt, err := testSealer(t, "k1").Open(orgID, r.keyID, r.cipher)
	if err != nil || string(pt) != testBotToken {
		t.Fatalf("open: %v", err)
	}
}

func TestSetBotRejectsBadFormatWithoutNetwork(t *testing.T) {
	pool := testDB(t)
	orgID, _ := seedProject(t, pool)
	s, srv := tenantService(t, pool)
	ctx := context.Background()
	for _, tok := range []string{
		"", "abc", "123:short", "12345678:has space inside the token value xx",
		"1234:AAFakeTokenForTestsOnly_abcdefghijk-XYZ",                   // id too short
		"987654321:AAFakeTokenForTestsOnly_abcdefghijk/XYZ",              // bad char
		"987654321AAFakeTokenForTestsOnly_abcdefghijk-XYZ",               // no colon
		"https://evil.example/987654321:AAFakeTokenForTestsOnly_abcdefg", // url
	} {
		if _, err := s.SetBot(ctx, orgID, 0, tok, ""); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("token %q: err = %v", tok, err)
		}
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests = %d", n)
	}
	if _, ok := readBotRow(t, pool, orgID); ok {
		t.Fatal("row stored")
	}
	// Surrounding whitespace is trimmed; the trimmed value is used.
	if _, err := s.SetBot(ctx, orgID, 0, "  \t"+testBotToken+"\n ", ""); err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].Token != testBotToken {
		t.Fatalf("requests = %+v", reqs)
	}
	r, _ := readBotRow(t, pool, orgID)
	pt, err := testSealer(t, "k1").Open(orgID, r.keyID, r.cipher)
	if err != nil || string(pt) != testBotToken {
		t.Fatalf("sealed value is not the trimmed token: %v", err)
	}
}

func TestSetBotUnauthorizedIsInvalidToken(t *testing.T) {
	pool := testDB(t)
	orgID, _ := seedProject(t, pool)
	s, srv := tenantService(t, pool)
	srv.SetReply("getMe", http.StatusUnauthorized, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
	if _, err := s.SetBot(context.Background(), orgID, 0, testBotToken, ""); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := readBotRow(t, pool, orgID); ok {
		t.Fatal("row stored")
	}
}

func TestSetBotUnreachable(t *testing.T) {
	pool := testDB(t)
	orgID, _ := seedProject(t, pool)
	ctx := context.Background()

	s, srv := tenantService(t, pool)
	srv.SetReply("getMe", http.StatusInternalServerError, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`)
	_, err := s.SetBot(ctx, orgID, 0, testBotToken, "")
	if !errors.Is(err, ErrTelegramUnreachable) {
		t.Fatalf("500: err = %v", err)
	}
	if strings.Contains(err.Error(), testBotToken[:8]) || strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("error leaks: %q", err)
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	s2 := NewService(pool, nil, "http://app").WithMode(ModeTenant).WithSealer(testSealer(t, "k1")).WithAPIBase(closedURL)
	_, err = s2.SetBot(ctx, orgID, 0, testBotToken, "")
	if !errors.Is(err, ErrTelegramUnreachable) {
		t.Fatalf("closed: err = %v", err)
	}
	if strings.Contains(err.Error(), testBotToken[:8]) || strings.Contains(err.Error(), closedURL) {
		t.Fatalf("error leaks: %q", err)
	}
	if _, ok := readBotRow(t, pool, orgID); ok {
		t.Fatal("row stored")
	}
}

func TestSetBotWithoutSealer(t *testing.T) {
	pool := testDB(t)
	orgID, _ := seedProject(t, pool)
	srv := newStrictTelegramServer(t)
	s := NewService(pool, nil, "http://app").WithMode(ModeTenant).WithAPIBase(srv.URL)
	if _, err := s.SetBot(context.Background(), orgID, 0, testBotToken, ""); !errors.Is(err, ErrNoSealer) {
		t.Fatalf("err = %v", err)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests = %d", n)
	}
	if _, ok := readBotRow(t, pool, orgID); ok {
		t.Fatal("row stored")
	}
}

func TestSharedModeRejectsBotCalls(t *testing.T) {
	ctx := context.Background()
	srv := newStrictTelegramServer(t)
	// A nil pool proves no query is made.
	s := NewService(nil, nil, "http://app").WithSealer(testSealer(t, "k1")).WithAPIBase(srv.URL)
	if s.Mode() != ModeShared {
		t.Fatalf("default mode = %q", s.Mode())
	}
	if _, err := s.SetBot(ctx, 1, 0, testBotToken, ""); !errors.Is(err, ErrWrongMode) {
		t.Fatalf("SetBot: %v", err)
	}
	if _, err := s.SetDetailLevel(ctx, 1, DetailFull); !errors.Is(err, ErrWrongMode) {
		t.Fatalf("SetDetailLevel: %v", err)
	}
	if err := s.DeleteBot(ctx, 1); !errors.Is(err, ErrWrongMode) {
		t.Fatalf("DeleteBot: %v", err)
	}
	if _, err := s.LookupChat(ctx, 1, "@news"); !errors.Is(err, ErrWrongMode) {
		t.Fatalf("LookupChat: %v", err)
	}
	v, err := s.BotStatus(ctx, 1)
	if err != nil || v != (BotView{}) {
		t.Fatalf("BotStatus = %+v, %v", v, err)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests = %d", n)
	}
	if s.WithMode(ModeTenant).Mode() != ModeTenant {
		t.Fatal("WithMode")
	}
}

func TestSetBotReplaceOverwritesAndReactivates(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	orgID, _ := seedProject(t, pool)
	u1, u2 := seedUser(t, pool, "u1"), seedUser(t, pool, "u2")
	s, srv := tenantService(t, pool)
	if _, err := s.SetBot(ctx, orgID, u1, testBotToken, DetailFull); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE alert_bots SET status='invalid', status_reason='revoked', last_validated_at=NULL WHERE org_id=$1`, orgID); err != nil {
		t.Fatal(err)
	}
	srv.SetReplyForToken(testBotToken2, "getMe", http.StatusOK, `{"ok":true,"result":{"id":777,"username":"OtherBot"}}`)
	v, err := s.SetBot(ctx, orgID, u2, testBotToken2, "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "active" || v.StatusReason != "" || v.BotUsername != "OtherBot" || v.DetailLevel != DetailFull {
		t.Fatalf("view = %+v", v)
	}
	r, _ := readBotRow(t, pool, orgID)
	if r.createdBy == nil || *r.createdBy != u1 || r.botID != 777 || !r.hasValidated || r.status != "active" || r.reason != "" {
		t.Fatalf("row = %+v", r)
	}
	pt, err := testSealer(t, "k1").Open(orgID, r.keyID, r.cipher)
	if err != nil || string(pt) != testBotToken2 {
		t.Fatal("token not replaced")
	}
	// An explicit level replaces the existing one.
	v, err = s.SetBot(ctx, orgID, u2, testBotToken2, DetailMinimal)
	if err != nil || v.DetailLevel != DetailMinimal {
		t.Fatalf("level: %+v %v", v, err)
	}
}

func TestSetBotInvalidLevelBeforeNetwork(t *testing.T) {
	pool := testDB(t)
	orgID, _ := seedProject(t, pool)
	srv := newStrictTelegramServer(t)
	// No sealer and a bad token: the level check still comes first.
	s := NewService(pool, nil, "http://app").WithMode(ModeTenant).WithAPIBase(srv.URL)
	if _, err := s.SetBot(context.Background(), orgID, 0, "bad", "loud"); !errors.Is(err, ErrInvalidLevel) {
		t.Fatalf("err = %v", err)
	}
	s.WithSealer(testSealer(t, "k1"))
	if _, err := s.SetBot(context.Background(), orgID, 0, testBotToken, "loud"); !errors.Is(err, ErrInvalidLevel) {
		t.Fatalf("err = %v", err)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests = %d", n)
	}
}

func TestSetDetailLevel(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	orgID, _ := seedProject(t, pool)
	s, _ := tenantService(t, pool)
	if _, err := s.SetDetailLevel(ctx, orgID, DetailFull); !errors.Is(err, ErrBotNotFound) {
		t.Fatalf("no bot: %v", err)
	}
	if _, err := s.SetBot(ctx, orgID, 0, testBotToken, ""); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []DetailLevel{"", "loud"} {
		if _, err := s.SetDetailLevel(ctx, orgID, bad); !errors.Is(err, ErrInvalidLevel) {
			t.Fatalf("level %q: %v", bad, err)
		}
	}
	v, err := s.SetDetailLevel(ctx, orgID, DetailFull)
	if err != nil || v.DetailLevel != DetailFull || !v.Configured || v.BotUsername != "TestBot" {
		t.Fatalf("view = %+v, %v", v, err)
	}
	got, err := s.BotStatus(ctx, orgID)
	if err != nil || got.DetailLevel != DetailFull {
		t.Fatalf("status = %+v, %v", got, err)
	}
}

func TestDeleteBotIsIdempotent(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	orgID, _ := seedProject(t, pool)
	s, _ := tenantService(t, pool)
	if err := s.DeleteBot(ctx, orgID); err != nil {
		t.Fatalf("delete none: %v", err)
	}
	if _, err := s.SetBot(ctx, orgID, 0, testBotToken, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBot(ctx, orgID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBot(ctx, orgID); err != nil {
		t.Fatal(err)
	}
	if _, ok := readBotRow(t, pool, orgID); ok {
		t.Fatal("row remains")
	}
	v, err := s.BotStatus(ctx, orgID)
	if err != nil || v.Configured || v.DetailLevel != DetailMinimal || v.Status != "" || v.UpdatedAt != nil {
		t.Fatalf("status = %+v, %v", v, err)
	}
}

func TestBotViewNeverContainsToken(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	orgID, _ := seedProject(t, pool)
	s, _ := tenantService(t, pool)
	set, err := s.SetBot(ctx, orgID, 0, testBotToken, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.BotStatus(ctx, orgID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []BotView{set, got} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		for k := range m {
			if strings.Contains(strings.ToLower(k), "token") || strings.Contains(strings.ToLower(k), "cipher") {
				t.Fatalf("key %q in %s", k, b)
			}
		}
		want := []string{"configured", "bot_username", "status", "status_reason", "detail_level", "updated_at"}
		if len(m) != len(want) {
			t.Fatalf("keys = %s", b)
		}
		for _, k := range want {
			if _, ok := m[k]; !ok {
				t.Fatalf("missing %q in %s", k, b)
			}
		}
		if bytes.Contains(b, []byte(testBotToken[:8])) {
			t.Fatalf("token in %s", b)
		}
	}
}

// captureLogs routes the default slog logger to a buffer for the test.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	var mu sync.Mutex
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, w: &buf}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestSetBotNeverLogsOrReturnsToken(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	orgID, _ := seedProject(t, pool)
	logs := captureLogs(t)
	s, srv := tenantService(t, pool)
	var errs []string
	collect := func(err error) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	_, err := s.SetBot(ctx, orgID, 0, testBotToken, "")
	collect(err)
	if err != nil {
		t.Fatalf("success case: %v", err)
	}
	srv.SetReply("getMe", http.StatusUnauthorized, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
	_, err = s.SetBot(ctx, orgID, 0, testBotToken, "")
	collect(err)
	srv.SetReply("getMe", http.StatusInternalServerError, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`)
	_, err = s.SetBot(ctx, orgID, 0, testBotToken, "")
	collect(err)
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	s.WithAPIBase(closed.URL)
	_, err = s.SetBot(ctx, orgID, 0, testBotToken, "")
	collect(err)
	if len(errs) != 3 {
		t.Fatalf("errors = %q", errs)
	}
	out := logs()
	for _, hay := range append(errs, out) {
		for _, needle := range []string{testBotToken, testBotToken[:8], srv.URL, closed.URL, "/bot"} {
			if strings.Contains(hay, needle) {
				t.Fatalf("%q leaks %q", hay, needle)
			}
		}
	}
}

func TestCopiedCiphertextFailsForOtherOrg(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	orgA, _ := seedProject(t, pool)
	orgB := seedOrg(t, pool, "other")
	logs := captureLogs(t)
	s, srv := tenantService(t, pool)
	if _, err := s.SetBot(ctx, orgA, 0, testBotToken, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO alert_bots(org_id, token_ciphertext, key_id, bot_id, bot_username)
SELECT $2, token_ciphertext, key_id, bot_id, bot_username FROM alert_bots WHERE org_id = $1`, orgA, orgB); err != nil {
		t.Fatal(err)
	}
	before := len(srv.Requests())
	bot, err := s.workingBot(ctx, orgB)
	if err != nil || bot != nil {
		t.Fatalf("workingBot(B) = %v, %v", bot, err)
	}
	if _, err := s.LookupChat(ctx, orgB, "@news"); !errors.Is(err, ErrBotNotFound) {
		t.Fatalf("LookupChat(B): %v", err)
	}
	if n := len(srv.Requests()); n != before {
		t.Fatalf("requests after copy = %d, before = %d", n, before)
	}
	r, _ := readBotRow(t, pool, orgB)
	if r.status != "active" {
		t.Fatalf("decrypt failure marked the bot %q", r.status)
	}
	out := logs()
	if !strings.Contains(out, "cannot decrypt org bot token") || strings.Contains(out, testBotToken[:8]) {
		t.Fatalf("logs = %s", out)
	}
}

func TestWorkingBotReturnsClientForActiveBot(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	orgID, _ := seedProject(t, pool)
	s, srv := tenantService(t, pool)
	if _, err := s.SetBot(ctx, orgID, 0, testBotToken, DetailFull); err != nil {
		t.Fatal(err)
	}
	bot, err := s.workingBot(ctx, orgID)
	if err != nil || bot == nil {
		t.Fatalf("workingBot = %v, %v", bot, err)
	}
	if bot.level != DetailFull || bot.username != "TestBot" {
		t.Fatalf("bot = %+v", bot)
	}
	var _ Notifier = bot.tg
	if err := bot.tg.SendMessage(ctx, "-100123", "hi"); err != nil {
		t.Fatal(err)
	}
	calls := srv.Calls("sendMessage")
	if len(calls) != 1 || calls[0].Token != testBotToken {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestWorkingBotNilWhenInvalidOrNoSealer(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	orgID, _ := seedProject(t, pool)
	s, srv := tenantService(t, pool)
	if bot, err := s.workingBot(ctx, orgID); err != nil || bot != nil {
		t.Fatalf("no row: %v, %v", bot, err)
	}
	if _, err := s.SetBot(ctx, orgID, 0, testBotToken, ""); err != nil {
		t.Fatal(err)
	}
	noSealer := NewService(pool, nil, "http://app").WithMode(ModeTenant).WithAPIBase(srv.URL)
	if bot, err := noSealer.workingBot(ctx, orgID); err != nil || bot != nil {
		t.Fatalf("no sealer: %v, %v", bot, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE alert_bots SET status='invalid' WHERE org_id=$1`, orgID); err != nil {
		t.Fatal(err)
	}
	if bot, err := s.workingBot(ctx, orgID); err != nil || bot != nil {
		t.Fatalf("invalid: %v, %v", bot, err)
	}
}

func TestLookupChat(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	orgID, _ := seedProject(t, pool)
	s, srv := tenantService(t, pool)
	if _, err := s.LookupChat(ctx, orgID, "@news"); !errors.Is(err, ErrBotNotFound) {
		t.Fatalf("no bot: %v", err)
	}
	if _, err := s.SetBot(ctx, orgID, 0, testBotToken, ""); err != nil {
		t.Fatal(err)
	}
	info, err := s.LookupChat(ctx, orgID, "@news")
	if err != nil || info != (ChatInfo{Title: "Ops", Type: "supergroup"}) {
		t.Fatalf("info = %+v, %v", info, err)
	}
	calls := srv.Calls("getChat")
	if len(calls) != 1 || calls[0].Token != testBotToken || calls[0].Body["chat_id"] != "@news" {
		t.Fatalf("calls = %+v", calls)
	}
}
