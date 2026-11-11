package alerts

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedOrgWithBot inserts an organisation and an active bot row for token,
// sealed with s's sealer. It writes the row directly so no getMe request
// reaches the strict server.
func seedOrgWithBot(t *testing.T, pool *pgxpool.Pool, s *Service, slug, token string) int64 {
	t.Helper()
	orgID := seedOrg(t, pool, slug)
	insertBot(t, pool, s, orgID, token)
	return orgID
}

func insertBot(t *testing.T, pool *pgxpool.Pool, s *Service, orgID int64, token string) {
	t.Helper()
	blob, keyID, err := s.sealer.Seal(orgID, []byte(token))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO alert_bots(org_id, token_ciphertext, key_id, bot_id, bot_username, status, detail_level)
VALUES ($1, $2, $3, 1, 'TestBot', 'active', 'full')`, orgID, blob, keyID); err != nil {
		t.Fatalf("insert bot: %v", err)
	}
}

func seedOrgProject(t *testing.T, pool *pgxpool.Pool, orgID int64, slug string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO projects(org_id, name, slug, platform) VALUES ($1, $2, $2, 'web') RETURNING id`, orgID, slug).Scan(&id); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return id
}

func seedManualChannel(t *testing.T, pool *pgxpool.Pool, projectID int64, dest string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO alert_channels(project_id, origin, destination, title) VALUES ($1, 'manual', $2, 'Ops') RETURNING id`,
		projectID, dest).Scan(&id); err != nil {
		t.Fatalf("seed manual channel: %v", err)
	}
	return id
}

// seedTenantIncident is seedIncident with fired_at pinned relative to
// dispatchNow, so the 24 h expiry never depends on the wall clock.
func seedTenantIncident(t *testing.T, pool *pgxpool.Pool, projectID int64, age time.Duration) int64 {
	t.Helper()
	id := seedIncident(t, pool, projectID, KindNewIssue, "TypeError: boom")
	if _, err := pool.Exec(context.Background(),
		`UPDATE alert_incidents SET fired_at=$2 WHERE id=$1`, id, dispatchNow.Add(-age)); err != nil {
		t.Fatal(err)
	}
	return id
}

func channelRow(t *testing.T, pool *pgxpool.Pool, id int64) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(),
		`SELECT row_to_json(c)::text FROM alert_channels c WHERE id=$1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func botStatus(t *testing.T, pool *pgxpool.Pool, orgID int64) (status, reason string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`SELECT status, status_reason FROM alert_bots WHERE org_id=$1`, orgID).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	return status, reason
}

func callsForToken(srv *strictTelegram, token string) []strictRequest {
	var out []strictRequest
	for _, r := range srv.Requests() {
		if r.Token == token {
			out = append(out, r)
		}
	}
	return out
}

func wantState(t *testing.T, pool *pgxpool.Pool, id int64, notify, recovery string, attempts int, next time.Time) {
	t.Helper()
	st := incidentState(t, pool, id)
	if st.Notify != notify || st.Recovery != recovery || st.Attempts != attempts || !st.NextAttempt.Equal(next) {
		t.Fatalf("incident %d = %+v, want notify=%s recovery=%s attempts=%d next=%v", id, st, notify, recovery, attempts, next)
	}
}

// due is the next_attempt_at seedIncident writes.
var due = dispatchNow.Add(-time.Minute)

func TestTenantDispatchUsesOrgBot(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgA := seedOrgWithBot(t, pool, s, "org-a", testBotToken)
	orgB := seedOrgWithBot(t, pool, s, "org-b", testBotToken2)
	pa := seedOrgProject(t, pool, orgA, "proj-a")
	pb := seedOrgProject(t, pool, orgB, "proj-b")
	seedManualChannel(t, pool, pa, "-100111")
	seedManualChannel(t, pool, pb, "@b_channel")
	ia := seedTenantIncident(t, pool, pa, time.Minute)
	ib := seedTenantIncident(t, pool, pb, time.Minute)

	dispatch(t, s, dispatchNow)

	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %+v", reqs)
	}
	for _, r := range reqs {
		if r.Method != "sendMessage" {
			t.Fatalf("unexpected method %q", r.Method)
		}
	}
	a, b := callsForToken(srv, testBotToken), callsForToken(srv, testBotToken2)
	if len(a) != 1 || a[0].Body["chat_id"] != float64(-100111) {
		t.Fatalf("org A requests = %+v", a)
	}
	if len(b) != 1 || b[0].Body["chat_id"] != "@b_channel" {
		t.Fatalf("org B requests = %+v", b)
	}
	wantState(t, pool, ia, "sent", "none", 0, due)
	wantState(t, pool, ib, "sent", "none", 0, due)
}

func TestTenantNoBotKeepsPendingThenExpires(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgID := seedOrg(t, pool, "no-bot")
	pid := seedOrgProject(t, pool, orgID, "proj")
	seedManualChannel(t, pool, pid, "-100111")
	young := seedTenantIncident(t, pool, pid, time.Hour)
	old := seedTenantIncident(t, pool, pid, 25*time.Hour)
	// An old incident whose fire went out: only its pending recovery expires.
	oldSent := seedTenantIncident(t, pool, pid, 25*time.Hour)
	if _, err := pool.Exec(context.Background(),
		`UPDATE alert_incidents SET status='resolved', notify_state='sent', recovery_notify_state='pending' WHERE id=$1`, oldSent); err != nil {
		t.Fatal(err)
	}

	dispatch(t, s, dispatchNow)

	wantState(t, pool, young, "pending", "none", 0, dispatchNow.Add(time.Minute))
	wantState(t, pool, old, "skipped", "none", 0, due)
	wantState(t, pool, oldSent, "sent", "skipped", 0, due)
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests = %d, want none", n)
	}

	// The deferred incident is not due again before the recheck.
	dispatch(t, s, dispatchNow.Add(30*time.Second))
	wantState(t, pool, young, "pending", "none", 0, dispatchNow.Add(time.Minute))
}

func TestTenantBotAddedDeliversPending(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgID := seedOrg(t, pool, "late-bot")
	pid := seedOrgProject(t, pool, orgID, "proj")
	seedManualChannel(t, pool, pid, "-100111")
	var ids []int64
	for i := 0; i < 5; i++ {
		ids = append(ids, seedTenantIncident(t, pool, pid, time.Hour))
	}

	dispatch(t, s, dispatchNow)
	for _, id := range ids {
		wantState(t, pool, id, "pending", "none", 0, dispatchNow.Add(time.Minute))
	}

	insertBot(t, pool, s, orgID, testBotToken)
	dispatch(t, s, dispatchNow.Add(2*time.Minute))

	calls := srv.Calls("sendMessage")
	if len(calls) != 1 || len(srv.Requests()) != 1 {
		t.Fatalf("want exactly one digest, got %+v", srv.Requests())
	}
	if text, _ := calls[0].Body["text"].(string); !strings.HasPrefix(text, "🚨 5 new alerts") {
		t.Fatalf("not a digest: %q", text)
	}
	for _, id := range ids {
		wantState(t, pool, id, "sent", "none", 0, dispatchNow.Add(time.Minute))
	}
}

func TestTenantUnauthorizedMarksBotInvalid(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgA := seedOrgWithBot(t, pool, s, "org-a", testBotToken)
	pa1 := seedOrgProject(t, pool, orgA, "a-one")
	orgB := seedOrgWithBot(t, pool, s, "org-b", testBotToken2)
	pb := seedOrgProject(t, pool, orgB, "b-one")
	pa2 := seedOrgProject(t, pool, orgA, "a-two")
	chA1 := seedManualChannel(t, pool, pa1, "-100111")
	seedManualChannel(t, pool, pa2, "-100112")
	seedManualChannel(t, pool, pb, "-100222")
	ia1 := seedTenantIncident(t, pool, pa1, time.Minute)
	ia2 := seedTenantIncident(t, pool, pa2, time.Minute)
	ib := seedTenantIncident(t, pool, pb, time.Minute)
	srv.SetReplyForToken(testBotToken, "sendMessage", http.StatusUnauthorized,
		`{"ok":false,"error_code":401,"description":"Unauthorized"}`)
	chBefore := channelRow(t, pool, chA1)

	dispatch(t, s, dispatchNow)

	if st, reason := botStatus(t, pool, orgA); st != "invalid" || reason != "token rejected by Telegram" {
		t.Fatalf("org A bot = %q %q", st, reason)
	}
	if st, _ := botStatus(t, pool, orgB); st != "active" {
		t.Fatalf("org B bot = %q", st)
	}
	wantState(t, pool, ia1, "pending", "none", 0, dispatchNow.Add(time.Minute))
	wantState(t, pool, ia2, "pending", "none", 0, dispatchNow.Add(time.Minute))
	wantState(t, pool, ib, "sent", "none", 0, due)
	if n := len(callsForToken(srv, testBotToken)); n != 1 {
		t.Fatalf("org A token used %d times, want 1 (the org stops after the 401)", n)
	}
	if n := len(callsForToken(srv, testBotToken2)); n != 1 {
		t.Fatalf("org B token used %d times", n)
	}
	if got := channelRow(t, pool, chA1); got != chBefore {
		t.Fatalf("a bot failure touched the channel:\n%s\n%s", chBefore, got)
	}

	// The next tick sees no working bot: no request, still pending.
	before := len(srv.Requests())
	dispatch(t, s, dispatchNow.Add(2*time.Minute))
	if n := len(srv.Requests()); n != before {
		t.Fatalf("an invalid bot was used again")
	}
	wantState(t, pool, ia1, "pending", "none", 0, dispatchNow.Add(3*time.Minute))
}

// A 401 after one channel already accepted the message settles the incident
// as sent (no duplicate on the next tick) and still invalidates the bot.
func TestTenantUnauthorizedAfterPartialSendSettlesSent(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgID := seedOrgWithBot(t, pool, s, "org", testBotToken)
	pid := seedOrgProject(t, pool, orgID, "proj")
	seedManualChannel(t, pool, pid, "-100111")
	seedManualChannel(t, pool, pid, "-100112")
	inc := seedTenantIncident(t, pool, pid, time.Minute)
	other := seedTenantIncident(t, pool, pid, time.Minute)
	srv.QueueReply("sendMessage", http.StatusOK, `{"ok":true,"result":{"message_id":1}}`)
	srv.SetReplyForToken(testBotToken, "sendMessage", http.StatusUnauthorized,
		`{"ok":false,"error_code":401,"description":"Unauthorized"}`)

	dispatch(t, s, dispatchNow)

	if st, _ := botStatus(t, pool, orgID); st != "invalid" {
		t.Fatalf("bot = %q", st)
	}
	wantState(t, pool, inc, "sent", "none", 0, due)
	wantState(t, pool, other, "pending", "none", 0, dispatchNow.Add(time.Minute))
	if n := len(srv.Calls("sendMessage")); n != 2 {
		t.Fatalf("sendMessage calls = %d, want 2", n)
	}
}

func TestTenantRateLimitIsPerOrg(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgA := seedOrgWithBot(t, pool, s, "org-a", testBotToken)
	pa1 := seedOrgProject(t, pool, orgA, "a-one")
	orgB := seedOrgWithBot(t, pool, s, "org-b", testBotToken2)
	pb := seedOrgProject(t, pool, orgB, "b-one")
	pa2 := seedOrgProject(t, pool, orgA, "a-two")
	seedManualChannel(t, pool, pa1, "-100111")
	seedManualChannel(t, pool, pa2, "-100112")
	seedManualChannel(t, pool, pb, "-100222")
	ia1 := seedTenantIncident(t, pool, pa1, time.Minute)
	ia2 := seedTenantIncident(t, pool, pa2, time.Minute)
	notYetDue := seedTenantIncident(t, pool, pa2, time.Minute)
	longBackoff := seedTenantIncident(t, pool, pa2, time.Minute)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE alert_incidents SET next_attempt_at = CASE WHEN id=$1 THEN $3::timestamptz ELSE $4::timestamptz END WHERE id IN ($1,$2)`,
		notYetDue, longBackoff, dispatchNow.Add(5*time.Second), dispatchNow.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	ib := seedTenantIncident(t, pool, pb, time.Minute)
	srv.SetReplyForToken(testBotToken, "sendMessage", http.StatusTooManyRequests,
		`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":20}}`)

	dispatch(t, s, dispatchNow)

	until := dispatchNow.Add(20 * time.Second)
	wantState(t, pool, ia1, "pending", "none", 0, until)
	wantState(t, pool, ia2, "pending", "none", 0, until)
	wantState(t, pool, notYetDue, "pending", "none", 0, until)
	wantState(t, pool, longBackoff, "pending", "none", 0, dispatchNow.Add(2*time.Hour))
	wantState(t, pool, ib, "sent", "none", 0, due)
	if n := len(callsForToken(srv, testBotToken)); n != 1 {
		t.Fatalf("org A token used %d times, want 1", n)
	}
	if n := len(callsForToken(srv, testBotToken2)); n != 1 {
		t.Fatalf("org B token used %d times", n)
	}
}

func TestModeDerivedActivity(t *testing.T) {
	pool := testDB(t)
	srv := newStrictTelegramServer(t)
	fn := &fakeNotifier{}
	s := NewService(pool, fn, "http://app").WithSealer(testSealer(t, "k1")).WithAPIBase(srv.URL).WithMode(ModeTenant)
	orgID := seedOrgWithBot(t, pool, s, "org", testBotToken)
	pid := seedOrgProject(t, pool, orgID, "proj")
	linked := seedChannel(t, pool, pid, -1)
	manual := seedManualChannel(t, pool, pid, "-100555")
	// A numeric chat id on the manual row proves activity follows origin,
	// not the presence of telegram_chat_id.
	if _, err := pool.Exec(context.Background(), `UPDATE alert_channels SET telegram_chat_id=-777 WHERE id=$1`, manual); err != nil {
		t.Fatal(err)
	}

	// Tenant mode: only the manual channel, through the org's bot.
	linkedBefore := channelRow(t, pool, linked)
	i1 := seedTenantIncident(t, pool, pid, time.Minute)
	dispatch(t, s, dispatchNow)
	if n := len(fn.Sent()); n != 0 {
		t.Fatalf("shared notifier used in tenant mode: %+v", fn.Sent())
	}
	calls := srv.Calls("sendMessage")
	if len(calls) != 1 || calls[0].Token != testBotToken || calls[0].Body["chat_id"] != float64(-100555) {
		t.Fatalf("tenant calls = %+v", calls)
	}
	if got := channelRow(t, pool, linked); got != linkedBefore {
		t.Fatalf("linked row changed in tenant mode:\n%s\n%s", linkedBefore, got)
	}
	wantState(t, pool, i1, "sent", "none", 0, due)

	// Shared mode: only the linked channel, through the shared notifier.
	s.WithMode(ModeShared)
	manualBefore := channelRow(t, pool, manual)
	i2 := seedTenantIncident(t, pool, pid, time.Minute)
	dispatch(t, s, dispatchNow.Add(time.Minute))
	sent := fn.Sent()
	if len(sent) != 1 || sent[0].chat != "-1" {
		t.Fatalf("shared sends = %+v", sent)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Fatalf("org bot used in shared mode: %+v", srv.Requests())
	}
	if got := channelRow(t, pool, manual); got != manualBefore {
		t.Fatalf("manual row changed in shared mode:\n%s\n%s", manualBefore, got)
	}
	wantState(t, pool, i2, "sent", "none", 0, due)

	// Back to tenant mode: the manual channel resumes.
	s.WithMode(ModeTenant)
	i3 := seedTenantIncident(t, pool, pid, time.Minute)
	dispatch(t, s, dispatchNow.Add(2*time.Minute))
	if n := len(srv.Calls("sendMessage")); n != 2 {
		t.Fatalf("tenant mode did not resume: %d sends", n)
	}
	if n := len(fn.Sent()); n != 1 {
		t.Fatalf("shared notifier used again: %+v", fn.Sent())
	}
	wantState(t, pool, i3, "sent", "none", 0, due)

	for _, id := range []int64{linked, manual} {
		var enabled bool
		var reason string
		if err := pool.QueryRow(context.Background(), `SELECT enabled, disabled_reason FROM alert_channels WHERE id=$1`, id).Scan(&enabled, &reason); err != nil {
			t.Fatal(err)
		}
		if !enabled || reason != "" {
			t.Fatalf("channel %d enabled=%v reason=%q", id, enabled, reason)
		}
	}
}

const migrateReply = `{"ok":false,"error_code":400,"description":"Bad Request: group chat was upgraded to a supergroup chat","parameters":{"migrate_to_chat_id":-100999}}`

func TestManualChannelMigratedUpdatesDestination(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgID := seedOrgWithBot(t, pool, s, "org", testBotToken)
	pid := seedOrgProject(t, pool, orgID, "proj")
	ch := seedManualChannel(t, pool, pid, "-1")
	inc := seedTenantIncident(t, pool, pid, time.Minute)
	srv.QueueReply("sendMessage", http.StatusBadRequest, migrateReply)

	dispatch(t, s, dispatchNow)

	var dest string
	var chatID *int64
	if err := pool.QueryRow(context.Background(), `SELECT destination, telegram_chat_id FROM alert_channels WHERE id=$1`, ch).Scan(&dest, &chatID); err != nil {
		t.Fatal(err)
	}
	if dest != "-100999" || chatID != nil {
		t.Fatalf("destination=%q telegram_chat_id=%v", dest, chatID)
	}
	calls := srv.Calls("sendMessage")
	if len(calls) != 2 || calls[0].Body["chat_id"] != float64(-1) || calls[1].Body["chat_id"] != float64(-100999) {
		t.Fatalf("calls = %+v", calls)
	}
	wantState(t, pool, inc, "sent", "none", 0, due)
}

func TestManualChannelMigratedMergesIntoExisting(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgID := seedOrgWithBot(t, pool, s, "org", testBotToken)
	pid := seedOrgProject(t, pool, orgID, "proj")
	stale := seedManualChannel(t, pool, pid, "-1")
	seedManualChannel(t, pool, pid, "-100999")
	inc := seedTenantIncident(t, pool, pid, time.Minute)
	srv.QueueReply("sendMessage", http.StatusBadRequest, migrateReply)

	dispatch(t, s, dispatchNow)

	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM alert_channels WHERE id=$1`, stale).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the stale manual channel was not merged away")
	}
	calls := srv.Calls("sendMessage")
	if len(calls) != 2 || calls[1].Body["chat_id"] != float64(-100999) {
		t.Fatalf("calls = %+v", calls)
	}
	wantState(t, pool, inc, "sent", "none", 0, due)
}

func TestLinkedChannelMigratedUpdatesChatID(t *testing.T) {
	pool := testDB(t)
	srv := newStrictTelegramServer(t)
	s := NewService(pool, NewTelegram(testBotToken, srv.URL, nil), "http://app")
	_, pid := seedProject(t, pool)
	ch := seedChannel(t, pool, pid, -1)
	inc := seedTenantIncident(t, pool, pid, time.Minute)
	srv.QueueReply("sendMessage", http.StatusBadRequest, migrateReply)

	dispatch(t, s, dispatchNow)

	var chatID int64
	var dest string
	if err := pool.QueryRow(context.Background(), `SELECT telegram_chat_id, destination FROM alert_channels WHERE id=$1`, ch).Scan(&chatID, &dest); err != nil {
		t.Fatal(err)
	}
	if chatID != -100999 || dest != "" {
		t.Fatalf("telegram_chat_id=%d destination=%q", chatID, dest)
	}
	calls := srv.Calls("sendMessage")
	if len(calls) != 2 || calls[1].Body["chat_id"] != float64(-100999) {
		t.Fatalf("calls = %+v", calls)
	}
	wantState(t, pool, inc, "sent", "none", 0, due)
}

func TestTenantModeNeverPolls(t *testing.T) {
	fastTicks(t)
	g := pollGateInterval
	pollGateInterval = 5 * time.Millisecond
	t.Cleanup(func() { pollGateInterval = g })
	pool := testDB(t)
	src := &blockingSource{started: make(chan struct{})}
	// A notifier and a username would satisfy the shared-mode gate.
	s := NewService(pool, &fakeNotifier{}, "http://app").WithBotUsername("bot").
		WithMode(ModeTenant).WithSealer(testSealer(t, "k1"))
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(s, ctx, src)
	time.Sleep(150 * time.Millisecond)
	cancel()
	waitDone(t, done, time.Second)
	select {
	case <-src.started:
		t.Fatal("GetUpdates was called in tenant mode")
	default:
	}
}

func TestTenantServiceWithoutNotifierDoesNotSkip(t *testing.T) {
	pool := testDB(t)
	s, _ := tenantService(t, pool) // s.n == nil by design
	orgID := seedOrg(t, pool, "org")
	pid := seedOrgProject(t, pool, orgID, "proj")
	seedManualChannel(t, pool, pid, "-100111")
	inc := seedTenantIncident(t, pool, pid, time.Hour)
	// Another org's incident with a working bot must still be delivered.
	orgB := seedOrgWithBot(t, pool, s, "org-b", testBotToken2)
	pb := seedOrgProject(t, pool, orgB, "proj-b")
	seedManualChannel(t, pool, pb, "-100222")
	ib := seedTenantIncident(t, pool, pb, time.Hour)

	dispatch(t, s, dispatchNow)

	wantState(t, pool, inc, "pending", "none", 0, dispatchNow.Add(time.Minute))
	wantState(t, pool, ib, "sent", "none", 0, due)
}

func TestTenantNoManualChannelsSkips(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgID := seedOrgWithBot(t, pool, s, "org", testBotToken)
	pid := seedOrgProject(t, pool, orgID, "proj")
	seedChannel(t, pool, pid, -1) // linked: inactive in tenant mode
	inc := seedTenantIncident(t, pool, pid, time.Minute)

	dispatch(t, s, dispatchNow)

	wantState(t, pool, inc, "skipped", "none", 0, due)
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests = %+v", srv.Requests())
	}
}

func TestWorkingBotNilInSharedMode(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	srv := newStrictTelegramServer(t)
	s := NewService(pool, &fakeNotifier{}, "http://app").WithSealer(testSealer(t, "k1")).WithAPIBase(srv.URL)
	orgID := seedOrgWithBot(t, pool, s, "org", testBotToken)
	if bot, err := s.workingBot(ctx, orgID); err != nil || bot != nil {
		t.Fatalf("shared mode workingBot = %v, %v", bot, err)
	}
	s.WithMode(ModeTenant)
	if bot, err := s.workingBot(ctx, orgID); err != nil || bot == nil {
		t.Fatalf("tenant mode workingBot = %v, %v", bot, err)
	}
}

// failingSealer opens normally except for one organisation, for which Open
// fails with an error that is not ErrSealOpen (an unreachable KMS, say).
type failingSealer struct {
	*LocalSealer
	failOrg int64
	err     error
}

func (f *failingSealer) Open(orgID int64, keyID string, blob []byte) ([]byte, error) {
	if orgID == f.failOrg {
		return nil, f.err
	}
	return f.LocalSealer.Open(orgID, keyID, blob)
}

var errKMSDown = errors.New("kms unavailable")

func TestWorkingBotSurfacesNonSealOpenErrors(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	s, _ := tenantService(t, pool)
	orgID := seedOrgWithBot(t, pool, s, "org", testBotToken)
	s.WithSealer(&failingSealer{LocalSealer: testSealer(t, "k1"), failOrg: orgID, err: errKMSDown})
	bot, err := s.workingBot(ctx, orgID)
	if bot != nil || !errors.Is(err, errKMSDown) {
		t.Fatalf("workingBot = %v, %v; want the sealer error", bot, err)
	}
	if strings.Contains(err.Error(), testBotToken[:8]) {
		t.Fatalf("error leaks the token: %v", err)
	}
	// ErrSealOpen stays "no working bot".
	s.WithSealer(&failingSealer{LocalSealer: testSealer(t, "k1"), failOrg: orgID, err: ErrSealOpen})
	if bot, err := s.workingBot(ctx, orgID); bot != nil || err != nil {
		t.Fatalf("ErrSealOpen: workingBot = %v, %v", bot, err)
	}
}

func TestDispatchInfraErrorLeavesProjectUntouched(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgA := seedOrgWithBot(t, pool, s, "org-a", testBotToken)
	orgB := seedOrgWithBot(t, pool, s, "org-b", testBotToken2)
	pa := seedOrgProject(t, pool, orgA, "proj-a")
	pb := seedOrgProject(t, pool, orgB, "proj-b")
	seedManualChannel(t, pool, pa, "-100111")
	seedManualChannel(t, pool, pb, "-100222")
	ia := seedTenantIncident(t, pool, pa, 25*time.Hour) // old enough to expire if mishandled
	ib := seedTenantIncident(t, pool, pb, time.Minute)
	s.WithSealer(&failingSealer{LocalSealer: testSealer(t, "k1"), failOrg: orgA, err: errKMSDown})

	err := s.DispatchPending(context.Background(), dispatchNow)
	if err == nil || !errors.Is(err, errKMSDown) {
		t.Fatalf("DispatchPending error = %v, want the sealer failure joined", err)
	}
	wantState(t, pool, ia, "pending", "none", 0, due)
	wantState(t, pool, ib, "sent", "none", 0, due)
	if n := len(callsForToken(srv, testBotToken)); n != 0 {
		t.Fatalf("org A token used %d times", n)
	}
}

func TestSendMessageCanonicalNumericOnly(t *testing.T) {
	srv := newStrictTelegramServer(t)
	tg := NewTelegram(testBotToken, srv.URL, nil)
	ctx := context.Background()
	for _, chat := range []string{"007", "+123", "-1001234567890", "123456", "@name"} {
		if err := tg.SendMessage(ctx, chat, "hi"); err != nil {
			t.Fatal(err)
		}
	}
	want := []any{"007", "+123", float64(-1001234567890), float64(123456), "@name"}
	calls := srv.Calls("sendMessage")
	if len(calls) != len(want) {
		t.Fatalf("calls = %+v", calls)
	}
	for i, c := range calls {
		if c.Body["chat_id"] != want[i] {
			t.Fatalf("chat %d sent as %#v, want %#v", i, c.Body["chat_id"], want[i])
		}
	}
}
