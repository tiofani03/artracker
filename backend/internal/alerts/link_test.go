package alerts

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func linkSvc(t *testing.T) (*Service, *pgxpool.Pool, *fakeNotifier, int64) {
	t.Helper()
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	fn := &fakeNotifier{}
	return NewService(pool, fn, "https://app.example").WithBotUsername("ArtBot"), pool, fn, pid
}

func startUpdate(id int64, chat Chat, code string) Update {
	return Update{UpdateID: id, Message: &Message{Chat: chat, Text: "/start@ArtBot " + code}}
}

func countChannels(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM alert_channels`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var codeRe = regexp.MustCompile(`^[a-z0-9]{16}$`)

func TestGenerateLinkCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		c, err := generateLinkCode()
		if err != nil {
			t.Fatal(err)
		}
		if !codeRe.MatchString(c) {
			t.Fatalf("bad code %q", c)
		}
		if seen[c] {
			t.Fatalf("duplicate code %q", c)
		}
		seen[c] = true
	}
}

func TestCreateLinkCode(t *testing.T) {
	s, _, _, pid := linkSvc(t)
	lc, err := s.CreateLinkCode(context.Background(), pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !codeRe.MatchString(lc.Code) {
		t.Fatalf("bad code %q", lc.Code)
	}
	if lc.GroupLink != "https://t.me/ArtBot?startgroup="+lc.Code {
		t.Errorf("group link %q", lc.GroupLink)
	}
	if lc.PrivateLink != "https://t.me/ArtBot?start="+lc.Code {
		t.Errorf("private link %q", lc.PrivateLink)
	}
	if d := time.Until(lc.ExpiresAt) - 15*time.Minute; d > 5*time.Second || d < -5*time.Second {
		t.Errorf("expires_at off by %v", d)
	}
	off := NewService(s.db, nil, "https://app.example")
	if _, err := off.CreateLinkCode(context.Background(), pid, 0); !errors.Is(err, ErrTelegramDisabled) {
		t.Fatalf("want ErrTelegramDisabled, got %v", err)
	}
}

func TestHandleUpdateBindsGroup(t *testing.T) {
	s, pool, fn, pid := linkSvc(t)
	ctx := context.Background()
	lc, _ := s.CreateLinkCode(ctx, pid, 0)
	err := s.HandleUpdate(ctx, startUpdate(1, Chat{ID: -100, Type: "supergroup", Title: "Eng"}, lc.Code))
	if err != nil {
		t.Fatal(err)
	}
	var title, ctype string
	var enabled bool
	if err := pool.QueryRow(ctx, `SELECT title, chat_type, enabled FROM alert_channels WHERE project_id=$1 AND telegram_chat_id=-100`, pid).Scan(&title, &ctype, &enabled); err != nil {
		t.Fatal(err)
	}
	if title != "Eng" || ctype != "supergroup" || !enabled {
		t.Errorf("channel = %q %q %v", title, ctype, enabled)
	}
	var used bool
	pool.QueryRow(ctx, `SELECT used_at IS NOT NULL FROM alert_link_codes WHERE code=$1`, lc.Code).Scan(&used)
	if !used {
		t.Error("code not marked used")
	}
	sent := fn.Sent()
	if len(sent) != 1 || sent[0].ChatID != -100 || !strings.Contains(sent[0].HTML, "Checkout Web") || !strings.Contains(sent[0].HTML, "Connected") {
		t.Errorf("sent = %+v", sent)
	}
}

func TestHandleUpdateCodeSingleUse(t *testing.T) {
	s, pool, fn, pid := linkSvc(t)
	ctx := context.Background()
	lc, _ := s.CreateLinkCode(ctx, pid, 0)
	s.HandleUpdate(ctx, startUpdate(1, Chat{ID: -100, Type: "group", Title: "A"}, lc.Code))
	s.HandleUpdate(ctx, startUpdate(2, Chat{ID: -200, Type: "group", Title: "B"}, lc.Code))
	if n := countChannels(t, pool); n != 1 {
		t.Fatalf("channels = %d", n)
	}
	sent := fn.Sent()
	if len(sent) != 2 || sent[1].ChatID != -200 || !strings.Contains(sent[1].HTML, "expired") {
		t.Errorf("sent = %+v", sent)
	}
}

func TestHandleUpdateConcurrentSameCode(t *testing.T) {
	s, pool, fn, pid := linkSvc(t)
	ctx := context.Background()
	lc, _ := s.CreateLinkCode(ctx, pid, 0)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, id := range []int64{-100, -200} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s.HandleUpdate(ctx, startUpdate(1, Chat{ID: id, Type: "group", Title: "G"}, lc.Code))
		}()
	}
	close(start)
	wg.Wait()
	if n := countChannels(t, pool); n != 1 {
		t.Fatalf("channels = %d", n)
	}
	connected := 0
	for _, m := range fn.Sent() {
		if strings.Contains(m.HTML, "Connected") {
			connected++
		}
	}
	if connected != 1 || len(fn.Sent()) != 2 {
		t.Errorf("connected=%d sent=%+v", connected, fn.Sent())
	}
}

func TestHandleUpdateExpiredCode(t *testing.T) {
	s, pool, fn, pid := linkSvc(t)
	ctx := context.Background()
	lc, _ := s.CreateLinkCode(ctx, pid, 0)
	pool.Exec(ctx, `UPDATE alert_link_codes SET expires_at = now() - interval '1 minute' WHERE code=$1`, lc.Code)
	s.HandleUpdate(ctx, startUpdate(1, Chat{ID: -100, Type: "group", Title: "A"}, lc.Code))
	if n := countChannels(t, pool); n != 0 {
		t.Fatalf("channels = %d", n)
	}
	if sent := fn.Sent(); len(sent) != 1 || !strings.Contains(sent[0].HTML, "expired") {
		t.Errorf("sent = %+v", sent)
	}
}

func TestHandleUpdateSameChatTwoProjects(t *testing.T) {
	s, pool, _, p1 := linkSvc(t)
	ctx := context.Background()
	var p2 int64
	pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) SELECT org_id,'Other','other','web' FROM projects WHERE id=$1 RETURNING id`, p1).Scan(&p2)
	c1, _ := s.CreateLinkCode(ctx, p1, 0)
	c2, _ := s.CreateLinkCode(ctx, p2, 0)
	chat := Chat{ID: -100, Type: "group", Title: "Eng"}
	s.HandleUpdate(ctx, startUpdate(1, chat, c1.Code))
	s.HandleUpdate(ctx, startUpdate(2, chat, c2.Code))
	if n := countChannels(t, pool); n != 2 {
		t.Fatalf("channels = %d", n)
	}
}

func TestHandleUpdateRelinkReenables(t *testing.T) {
	s, pool, _, pid := linkSvc(t)
	ctx := context.Background()
	c1, _ := s.CreateLinkCode(ctx, pid, 0)
	s.HandleUpdate(ctx, startUpdate(1, Chat{ID: -100, Type: "group", Title: "Old"}, c1.Code))
	pool.Exec(ctx, `UPDATE alert_channels SET enabled=false, disabled_reason='x'`)
	c2, _ := s.CreateLinkCode(ctx, pid, 0)
	s.HandleUpdate(ctx, startUpdate(2, Chat{ID: -100, Type: "supergroup", Title: "New"}, c2.Code))
	var title, ctype, reason string
	var enabled bool
	pool.QueryRow(ctx, `SELECT title, chat_type, enabled, disabled_reason FROM alert_channels`).Scan(&title, &ctype, &enabled, &reason)
	if !enabled || reason != "" || title != "New" || ctype != "supergroup" || countChannels(t, pool) != 1 {
		t.Errorf("got %q %q %v %q", title, ctype, enabled, reason)
	}
}

func TestHandleUpdateTitleFallbacks(t *testing.T) {
	cases := []struct {
		chat Chat
		want string
	}{
		{Chat{ID: 1, Type: "private", Username: "bob", FirstName: "Bob"}, "@bob"},
		{Chat{ID: 2, Type: "private", FirstName: "Bob"}, "Bob"},
		{Chat{ID: 3, Type: "private"}, "Telegram chat"},
	}
	for _, c := range cases {
		s, pool, _, pid := linkSvc(t)
		ctx := context.Background()
		lc, _ := s.CreateLinkCode(ctx, pid, 0)
		s.HandleUpdate(ctx, startUpdate(1, c.chat, lc.Code))
		var title string
		pool.QueryRow(ctx, `SELECT title FROM alert_channels`).Scan(&title)
		if title != c.want {
			t.Errorf("title = %q, want %q", title, c.want)
		}
	}
}

func TestHandleUpdateEscapesProjectName(t *testing.T) {
	s, pool, fn, pid := linkSvc(t)
	ctx := context.Background()
	pool.Exec(ctx, `UPDATE projects SET name='A<b>&Co' WHERE id=$1`, pid)
	lc, _ := s.CreateLinkCode(ctx, pid, 0)
	s.HandleUpdate(ctx, startUpdate(1, Chat{ID: -100, Type: "group", Title: "G"}, lc.Code))
	sent := fn.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].HTML, "<b>A&lt;b&gt;&amp;Co</b>") {
		t.Errorf("sent = %+v", sent)
	}
}

func TestHandleUpdateIgnoresNonCodes(t *testing.T) {
	s, pool, fn, _ := linkSvc(t)
	ctx := context.Background()
	for i, txt := range []string{"/start", "hello", "/start short", "/start ABCDEFGHIJKLMNOP"} {
		err := s.HandleUpdate(ctx, Update{UpdateID: int64(i), Message: &Message{Chat: Chat{ID: -1, Type: "group"}, Text: txt}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.HandleUpdate(ctx, Update{UpdateID: 9}); err != nil {
		t.Fatal(err)
	}
	if len(fn.Sent()) != 0 || countChannels(t, pool) != 0 {
		t.Errorf("unexpected reply or channel: %+v", fn.Sent())
	}
}

func TestHandleUpdateNilNotifierDoesNotPanic(t *testing.T) {
	s, pool, _, pid := linkSvc(t)
	ctx := context.Background()
	lc, _ := s.CreateLinkCode(ctx, pid, 0)
	s.n = nil
	if err := s.HandleUpdate(ctx, startUpdate(1, Chat{ID: -100, Type: "group", Title: "G"}, lc.Code)); err != nil {
		t.Fatal(err)
	}
	if countChannels(t, pool) != 1 {
		t.Error("binding missing")
	}
}

func TestHandleUpdateKickedDisables(t *testing.T) {
	s, pool, _, pid := linkSvc(t)
	ctx := context.Background()
	lc, _ := s.CreateLinkCode(ctx, pid, 0)
	s.HandleUpdate(ctx, startUpdate(1, Chat{ID: -100, Type: "group", Title: "G"}, lc.Code))

	member := func(status string) Update {
		u := Update{UpdateID: 2, MyChatMember: &ChatMemberUpdated{Chat: Chat{ID: -100}}}
		u.MyChatMember.NewChatMember.Status = status
		return u
	}
	check := func(wantEnabled bool, wantReason string) {
		t.Helper()
		var enabled bool
		var reason string
		pool.QueryRow(ctx, `SELECT enabled, disabled_reason FROM alert_channels`).Scan(&enabled, &reason)
		if enabled != wantEnabled || reason != wantReason {
			t.Errorf("enabled=%v reason=%q", enabled, reason)
		}
	}
	s.HandleUpdate(ctx, member("member"))
	check(true, "")
	s.HandleUpdate(ctx, member("kicked"))
	check(false, "bot was removed from the chat")
	pool.Exec(ctx, `UPDATE alert_channels SET enabled=true, disabled_reason=''`)
	s.HandleUpdate(ctx, member("left"))
	check(false, "bot was removed from the chat")
}

type fakeSource struct {
	mu      sync.Mutex
	offsets []int64
	calls   int
	fn      func(call int, offset int64) ([]Update, error)
}

func (f *fakeSource) GetUpdates(ctx context.Context, offset int64, _ int) ([]Update, error) {
	f.mu.Lock()
	f.offsets = append(f.offsets, offset)
	call := f.calls
	f.calls++
	f.mu.Unlock()
	return f.fn(call, offset)
}

func (f *fakeSource) Offsets() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.offsets...)
}

func TestPollUpdatesAdvancesOffset(t *testing.T) {
	s, _, _, _ := linkSvc(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &fakeSource{}
	src.fn = func(call int, _ int64) ([]Update, error) {
		if call == 0 {
			// Updates without message or my_chat_member are ignored but still advance the offset.
			return []Update{{UpdateID: 5}, {UpdateID: 6}}, nil
		}
		cancel()
		return nil, nil
	}
	s.PollUpdates(ctx, src)
	offs := src.Offsets()
	if len(offs) != 2 || offs[0] != 0 || offs[1] != 7 {
		t.Errorf("offsets = %v", offs)
	}
}

func TestPollUpdatesAdvancesPastFailingUpdate(t *testing.T) {
	s, pool, _, _ := linkSvc(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A closed pool makes HandleUpdate fail on a valid-looking command.
	pool.Close()
	src := &fakeSource{}
	src.fn = func(call int, _ int64) ([]Update, error) {
		if call == 0 {
			return []Update{startUpdate(10, Chat{ID: -1, Type: "group"}, "abcdefghij012345")}, nil
		}
		cancel()
		return nil, nil
	}
	s.PollUpdates(ctx, src)
	if offs := src.Offsets(); len(offs) != 2 || offs[1] != 11 {
		t.Errorf("offsets = %v", offs)
	}
}

func TestPollUpdatesErrorSleeps(t *testing.T) {
	s, _, _, _ := linkSvc(t)
	oldConflict, oldErr := pollConflictSleep, pollErrorSleep
	pollConflictSleep, pollErrorSleep = 300*time.Millisecond, 10*time.Millisecond
	defer func() { pollConflictSleep, pollErrorSleep = oldConflict, oldErr }()

	run := func(err error) time.Duration {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var firstAt, secondAt time.Time
		src := &fakeSource{}
		src.fn = func(call int, _ int64) ([]Update, error) {
			if call == 0 {
				firstAt = time.Now()
				return nil, err
			}
			secondAt = time.Now()
			cancel()
			return nil, nil
		}
		s.PollUpdates(ctx, src)
		return secondAt.Sub(firstAt)
	}
	if d := run(errors.New("boom")); d >= 200*time.Millisecond {
		t.Errorf("generic error slept %v, want short", d)
	}
	if d := run(ErrConflict); d < 300*time.Millisecond {
		t.Errorf("conflict slept %v, want >= 300ms", d)
	}
}

func TestPollUpdatesCancelDuringSleep(t *testing.T) {
	s, _, _, _ := linkSvc(t)
	oldConflict := pollConflictSleep
	pollConflictSleep = time.Hour
	defer func() { pollConflictSleep = oldConflict }()
	ctx, cancel := context.WithCancel(context.Background())
	src := &fakeSource{fn: func(int, int64) ([]Update, error) {
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		return nil, ErrConflict
	}}
	done := make(chan struct{})
	go func() { s.PollUpdates(ctx, src); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("PollUpdates did not return after cancel")
	}
}
