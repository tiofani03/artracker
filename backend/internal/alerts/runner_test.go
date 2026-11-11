package alerts

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fastTicks shortens the runner intervals for one test.
func fastTicks(t *testing.T) {
	t.Helper()
	d, e := dispatchInterval, evaluateInterval
	dispatchInterval, evaluateInterval = 20*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { dispatchInterval, evaluateInterval = d, e })
}

func runAsync(s *Service, ctx context.Context, src UpdateSource) <-chan struct{} {
	done := make(chan struct{})
	go func() { s.Run(ctx, src); close(done) }()
	return done
}

func waitDone(t *testing.T, done <-chan struct{}, d time.Duration) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("Run did not return in time")
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	pool := testDB(t)
	s := NewService(pool, &fakeNotifier{}, "http://app").WithBotUsername("bot")
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(s, ctx, nil)
	time.Sleep(50 * time.Millisecond)
	cancel()
	waitDone(t, done, time.Second)
}

type blockingSource struct {
	started chan struct{}
	once    sync.Once
}

func (b *blockingSource) GetUpdates(ctx context.Context, _ int64, _ int) ([]Update, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRunPollsUpdatesAndWaitsForPoller(t *testing.T) {
	pool := testDB(t)
	src := &blockingSource{started: make(chan struct{})}
	s := NewService(pool, &fakeNotifier{}, "http://app").WithBotUsername("bot")
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(s, ctx, src)
	select {
	case <-src.started:
	case <-time.After(time.Second):
		t.Fatal("poller never started")
	}
	cancel()
	waitDone(t, done, time.Second)
}

func TestRunDoesNotPollWithoutTelegram(t *testing.T) {
	pool := testDB(t)
	src := &blockingSource{started: make(chan struct{})}
	s := NewService(pool, nil, "http://app") // no notifier, no username
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(s, ctx, src)
	time.Sleep(100 * time.Millisecond)
	cancel()
	waitDone(t, done, time.Second)
	select {
	case <-src.started:
		t.Fatal("poller started although Telegram is off")
	default:
	}
}

func TestRunDispatchesAndEvaluates(t *testing.T) {
	fastTicks(t)
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO alert_channels(project_id, telegram_chat_id, title, chat_type) VALUES ($1, 77, 'Ops', 'group')`, pid); err != nil {
		t.Fatal(err)
	}
	issueID := seedIssue(t, pool, pid, "open")
	if _, err := pool.Exec(ctx, `INSERT INTO alert_incidents(project_id, rule_kind, issue_id, title) VALUES ($1,'new_issue',$2,'TypeError: boom')`, pid, issueID); err != nil {
		t.Fatal(err)
	}
	// Evaluator proof: a firing incident whose issue is resolved is closed.
	resolved := seedIssue(t, pool, pid, "resolved")
	if _, err := pool.Exec(ctx, `INSERT INTO alert_incidents(project_id, rule_kind, issue_id, title, notify_state) VALUES ($1,'new_issue',$2,'old','sent')`, pid, resolved); err != nil {
		t.Fatal(err)
	}

	fn := &fakeNotifier{}
	s := NewService(pool, fn, "http://app").WithBotUsername("bot")
	rctx, cancel := context.WithCancel(ctx)
	done := runAsync(s, rctx, nil)
	defer func() { cancel(); waitDone(t, done, time.Second) }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		var state, status string
		_ = pool.QueryRow(ctx, `SELECT notify_state FROM alert_incidents WHERE issue_id=$1`, issueID).Scan(&state)
		_ = pool.QueryRow(ctx, `SELECT status FROM alert_incidents WHERE issue_id=$1`, resolved).Scan(&status)
		if state == "sent" && status != "firing" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not processed: notify_state=%q resolved incident status=%q", state, status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := fn.Sent(); len(got) == 0 || got[0].chat != "77" {
		t.Fatalf("sent = %+v", got)
	}
}

type panicNotifier struct{ calls chan struct{} }

func (p *panicNotifier) SendMessage(context.Context, string, string) error {
	select {
	case p.calls <- struct{}{}:
	default:
	}
	panic("boom")
}

func TestRunSurvivesPanicInTick(t *testing.T) {
	fastTicks(t)
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO alert_channels(project_id, telegram_chat_id, title, chat_type) VALUES ($1, 77, 'Ops', 'group')`, pid); err != nil {
		t.Fatal(err)
	}
	issueID := seedIssue(t, pool, pid, "open")
	if _, err := pool.Exec(ctx, `INSERT INTO alert_incidents(project_id, rule_kind, issue_id, title) VALUES ($1,'new_issue',$2,'x')`, pid, issueID); err != nil {
		t.Fatal(err)
	}
	pn := &panicNotifier{calls: make(chan struct{}, 10)}
	s := NewService(pool, pn, "http://app").WithBotUsername("bot")
	rctx, cancel := context.WithCancel(ctx)
	done := runAsync(s, rctx, nil)
	for i := 0; i < 2; i++ { // the dispatcher keeps ticking after a panic
		select {
		case <-pn.calls:
		case <-time.After(2 * time.Second):
			t.Fatalf("tick %d never ran", i)
		}
	}
	cancel()
	waitDone(t, done, time.Second)
}

func seedChannelEn(t *testing.T, s *Service, projectID, chatID int64, enabled bool) int64 {
	t.Helper()
	var id int64
	err := s.db.QueryRow(context.Background(),
		`INSERT INTO alert_channels(project_id, telegram_chat_id, title, chat_type, enabled) VALUES ($1,$2,'Ops','group',$3) RETURNING id`,
		projectID, chatID, enabled).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSendTest(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	fn := &fakeNotifier{}
	s := NewService(pool, fn, "http://app").WithBotUsername("bot")
	ch := seedChannelEn(t, s, pid, 555, true)
	if _, err := pool.Exec(context.Background(), `UPDATE alert_channels SET last_error='old' WHERE id=$1`, ch); err != nil {
		t.Fatal(err)
	}

	if err := s.SendTest(context.Background(), pid, ch); err != nil {
		t.Fatal(err)
	}
	got := fn.Sent()
	if len(got) != 1 || got[0].chat != "555" || !strings.Contains(got[0].HTML, "Test alert") {
		t.Fatalf("sent = %+v", got)
	}
	var lastErr string
	var sentAt *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT last_error, last_sent_at FROM alert_channels WHERE id=$1`, ch).Scan(&lastErr, &sentAt); err != nil {
		t.Fatal(err)
	}
	if lastErr != "" || sentAt == nil {
		t.Fatalf("last_error=%q last_sent_at=%v", lastErr, sentAt)
	}
}

func TestSendTestOtherProjectChannel(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	var other int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO projects(org_id, name, slug, platform) SELECT org_id, 'Other', 'other', 'web' FROM projects WHERE id=$1 RETURNING id`, pid).Scan(&other); err != nil {
		t.Fatal(err)
	}
	fn := &fakeNotifier{}
	s := NewService(pool, fn, "http://app").WithBotUsername("bot")
	ch := seedChannelEn(t, s, other, 9, true)
	if err := s.SendTest(context.Background(), pid, ch); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("err = %v, want ErrChannelNotFound", err)
	}
	if err := s.SendTest(context.Background(), pid, 987654); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("missing id err = %v", err)
	}
	if len(fn.Sent()) != 0 {
		t.Fatalf("sent = %+v, want none", fn.Sent())
	}
}

func TestSendTestDisabledChannelStillSends(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	fn := &fakeNotifier{}
	s := NewService(pool, fn, "http://app").WithBotUsername("bot")
	ch := seedChannelEn(t, s, pid, 5, false)
	if err := s.SendTest(context.Background(), pid, ch); err != nil {
		t.Fatal(err)
	}
	if len(fn.Sent()) != 1 {
		t.Fatalf("sent = %+v", fn.Sent())
	}
}

func TestSendTestTelegramOff(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	s := NewService(pool, nil, "http://app")
	if err := s.SendTest(context.Background(), pid, 1); !errors.Is(err, ErrTelegramDisabled) {
		t.Fatalf("err = %v", err)
	}
}

func TestSendTestFailureRecordsErrorAndForbiddenDisables(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	fn := &fakeNotifier{errFor: map[int64]error{}}
	s := NewService(pool, fn, "http://app").WithBotUsername("bot")
	bad := seedChannelEn(t, s, pid, 1, true)
	gone := seedChannelEn(t, s, pid, 2, true)
	fn.errFor[1] = &APIError{Code: 500, Description: "oops"}
	fn.errFor[2] = ErrChatForbidden

	if err := s.SendTest(context.Background(), pid, bad); err == nil {
		t.Fatal("want error")
	}
	var lastErr string
	pool.QueryRow(context.Background(), `SELECT last_error FROM alert_channels WHERE id=$1`, bad).Scan(&lastErr)
	if lastErr == "" {
		t.Fatal("last_error not recorded")
	}

	if err := s.SendTest(context.Background(), pid, gone); !errors.Is(err, ErrChatForbidden) {
		t.Fatalf("err = %v", err)
	}
	var enabled bool
	pool.QueryRow(context.Background(), `SELECT enabled FROM alert_channels WHERE id=$1`, gone).Scan(&enabled)
	if enabled {
		t.Fatal("forbidden channel should be disabled")
	}
}

func TestRunStartsPollingOnceBotUsernameIsSet(t *testing.T) {
	g := pollGateInterval
	pollGateInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollGateInterval = g })
	pool := testDB(t)
	src := &blockingSource{started: make(chan struct{})}
	s := NewService(pool, &fakeNotifier{}, "http://app") // notifier set, username unknown
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(s, ctx, src)
	defer func() { cancel(); waitDone(t, done, time.Second) }()

	select {
	case <-src.started:
		t.Fatal("poller started before the bot username was known")
	case <-time.After(10 * pollGateInterval):
	}

	s.SetBotUsername("ArtBot")
	select {
	case <-src.started:
	case <-time.After(2 * time.Second):
		t.Fatal("poller never started after SetBotUsername")
	}
}

func TestBotUsernameConcurrentAccess(t *testing.T) {
	g := pollGateInterval
	pollGateInterval = time.Millisecond
	t.Cleanup(func() { pollGateInterval = g })
	pool := testDB(t)
	s := NewService(pool, &fakeNotifier{}, "http://app")
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(s, ctx, &blockingSource{started: make(chan struct{})})

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s.SetBotUsername("ArtBot")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = s.TelegramConfigured()
				_ = s.BotUsername()
			}
		}()
	}
	wg.Wait()
	cancel()
	waitDone(t, done, time.Second)
	if !s.TelegramConfigured() || s.BotUsername() != "ArtBot" {
		t.Fatalf("configured=%v username=%q", s.TelegramConfigured(), s.BotUsername())
	}
}

func TestWithBotUsernameReturnsReceiver(t *testing.T) {
	s := NewService(nil, &fakeNotifier{}, "http://app")
	if got := s.WithBotUsername("ArtBot"); got != s || s.BotUsername() != "ArtBot" || !s.TelegramConfigured() {
		t.Fatalf("WithBotUsername: receiver=%v username=%q", got == s, s.BotUsername())
	}
}

func TestNextBackoff(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 5 * time.Second},
		{1, 10 * time.Second},
		{2, 20 * time.Second},
		{5, 160 * time.Second},
		{6, 5 * time.Minute},
		{62, 5 * time.Minute},
		{1000, 5 * time.Minute},
		{-1, 5 * time.Second},
	}
	for _, c := range cases {
		if got := nextBackoff(c.attempt); got != c.want {
			t.Errorf("nextBackoff(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestRetryBotUsernameRetriesUntilSuccess(t *testing.T) {
	b := getMeBackoff
	getMeBackoff = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { getMeBackoff = b })
	s := NewService(nil, &fakeNotifier{}, "http://app")
	calls := 0
	s.RetryBotUsername(context.Background(), func(context.Context) (string, error) {
		calls++
		if calls < 3 {
			return "", errors.New("dns hiccup")
		}
		return "ArtBot", nil
	})
	if calls != 3 || s.BotUsername() != "ArtBot" || !s.TelegramConfigured() {
		t.Fatalf("calls=%d username=%q configured=%v", calls, s.BotUsername(), s.TelegramConfigured())
	}
}

func TestRetryBotUsernameStopsOnCancel(t *testing.T) {
	b := getMeBackoff
	getMeBackoff = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { getMeBackoff = b })
	s := NewService(nil, &fakeNotifier{}, "http://app")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.RetryBotUsername(ctx, func(context.Context) (string, error) { return "", errors.New("down") })
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RetryBotUsername did not return after cancel")
	}
	if s.BotUsername() != "" {
		t.Fatalf("username = %q, want empty", s.BotUsername())
	}
}
