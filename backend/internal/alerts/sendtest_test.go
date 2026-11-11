package alerts

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSendTestTenantUsesOrgBot(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgA := seedOrgWithBot(t, pool, s, "org-a", testBotToken)
	orgB := seedOrgWithBot(t, pool, s, "org-b", testBotToken2)
	pa := seedOrgProject(t, pool, orgA, "a-one")
	pb := seedOrgProject(t, pool, orgB, "b-one")
	ch := seedManualChannel(t, pool, pa, "@ops_room")
	seedManualChannel(t, pool, pb, "@other_room")

	if err := s.SendTest(context.Background(), pa, ch); err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %+v, want exactly one sendMessage", reqs)
	}
	r := reqs[0]
	if r.Method != "sendMessage" || r.Token != testBotToken || r.Body["chat_id"] != "@ops_room" {
		t.Fatalf("request = %+v, want sendMessage with org A's token to @ops_room", r)
	}
	if text, _ := r.Body["text"].(string); !strings.Contains(text, "Test alert") {
		t.Fatalf("text = %q", text)
	}
	var sentAt *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT last_sent_at FROM alert_channels WHERE id=$1`, ch).Scan(&sentAt); err != nil {
		t.Fatal(err)
	}
	if sentAt == nil {
		t.Fatal("last_sent_at not recorded")
	}

	// Another project's channel id is indistinguishable from a missing one.
	if err := s.SendTest(context.Background(), pb, ch); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("cross-project err = %v, want ErrChannelNotFound", err)
	}
}

func TestSendTestTenantWithoutBotIsErrBotNotFound(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgID := seedOrg(t, pool, "no-bot")
	pid := seedOrgProject(t, pool, orgID, "nb-app")
	ch := seedManualChannel(t, pool, pid, "-1001234567890")

	if err := s.SendTest(context.Background(), pid, ch); !errors.Is(err, ErrBotNotFound) {
		t.Fatalf("err = %v, want ErrBotNotFound", err)
	}
	insertBot(t, pool, s, orgID, testBotToken)
	if _, err := pool.Exec(context.Background(), `UPDATE alert_bots SET status='invalid' WHERE org_id=$1`, orgID); err != nil {
		t.Fatal(err)
	}
	if err := s.SendTest(context.Background(), pid, ch); !errors.Is(err, ErrBotNotFound) {
		t.Fatalf("invalid bot err = %v, want ErrBotNotFound", err)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests = %d, want none", n)
	}
}

func TestSendTestTenantLinkedChannelIsNotFound(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgID := seedOrgWithBot(t, pool, s, "linked-org", testBotToken)
	pid := seedOrgProject(t, pool, orgID, "linked-app")
	ch := seedChannelEn(t, s, pid, 4242, true) // origin 'linked'

	if err := s.SendTest(context.Background(), pid, ch); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("err = %v, want ErrChannelNotFound", err)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests = %d, want none", n)
	}
}

func TestSendTestSharedModeUnchanged(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	fn := &fakeNotifier{}
	s := NewService(pool, fn, "http://app").WithBotUsername("bot")
	linked := seedChannelEn(t, s, pid, 777, true)
	manual := seedManualChannel(t, pool, pid, "@manual_room")

	if err := s.SendTest(context.Background(), pid, linked); err != nil {
		t.Fatal(err)
	}
	if got := fn.Sent(); len(got) != 1 || got[0].chat != "777" {
		t.Fatalf("sent = %+v", got)
	}
	if err := s.SendTest(context.Background(), pid, manual); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("manual channel in shared mode err = %v, want ErrChannelNotFound", err)
	}
	if n := len(fn.Sent()); n != 1 {
		t.Fatalf("sent = %d, want 1", n)
	}
	off := NewService(pool, nil, "http://app")
	if err := off.SendTest(context.Background(), pid, linked); !errors.Is(err, ErrTelegramDisabled) {
		t.Fatalf("telegram off err = %v", err)
	}
}

func TestSendTestTenantUnauthorizedMarksBotInvalid(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgID := seedOrgWithBot(t, pool, s, "unauth-org", testBotToken)
	pid := seedOrgProject(t, pool, orgID, "unauth-app")
	ch := seedManualChannel(t, pool, pid, "@ops_room")
	before := channelRow(t, pool, ch)
	srv.SetReplyForToken(testBotToken, "sendMessage", http.StatusUnauthorized,
		`{"ok":false,"error_code":401,"description":"Unauthorized"}`)

	if err := s.SendTest(context.Background(), pid, ch); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if st, reason := botStatus(t, pool, orgID); st != "invalid" || reason != "token rejected by Telegram" {
		t.Fatalf("bot = %q %q", st, reason)
	}
	if got := channelRow(t, pool, ch); got != before {
		t.Fatalf("a token failure touched the channel:\n%s\n%s", before, got)
	}

	// A token saved in the meantime is not invalidated by a late 401 for the old one.
	if _, err := pool.Exec(context.Background(), `DELETE FROM alert_bots WHERE org_id=$1`, orgID); err != nil {
		t.Fatal(err)
	}
	insertBot(t, pool, s, orgID, testBotToken)
	srv.OnRequest(func(token, method string) {
		if method != "sendMessage" {
			return
		}
		blob, keyID, err := s.sealer.Seal(orgID, []byte(testBotToken2))
		if err != nil {
			t.Error(err)
			return
		}
		if _, err := pool.Exec(context.Background(),
			`UPDATE alert_bots SET token_ciphertext=$2, key_id=$3 WHERE org_id=$1`, orgID, blob, keyID); err != nil {
			t.Error(err)
		}
	})
	if err := s.SendTest(context.Background(), pid, ch); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if st, _ := botStatus(t, pool, orgID); st != "active" {
		t.Fatalf("a late 401 invalidated the replacement token: status %q", st)
	}
}

func TestSendTestTenantForbiddenDisablesChannel(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgID := seedOrgWithBot(t, pool, s, "forbid-org", testBotToken)
	pid := seedOrgProject(t, pool, orgID, "forbid-app")
	ch := seedManualChannel(t, pool, pid, "@ops_room")
	srv.SetReply("sendMessage", http.StatusForbidden, `{"ok":false,"error_code":403,"description":"Forbidden: bot was kicked"}`)

	if err := s.SendTest(context.Background(), pid, ch); !errors.Is(err, ErrChatForbidden) {
		t.Fatalf("err = %v, want ErrChatForbidden", err)
	}
	var enabled bool
	var reason, lastErr string
	if err := pool.QueryRow(context.Background(),
		`SELECT enabled, disabled_reason, last_error FROM alert_channels WHERE id=$1`, ch).Scan(&enabled, &reason, &lastErr); err != nil {
		t.Fatal(err)
	}
	if enabled || reason != forbiddenReason || lastErr == "" {
		t.Fatalf("enabled=%v reason=%q last_error=%q", enabled, reason, lastErr)
	}
}

func TestLevelFromDBFailsClosed(t *testing.T) {
	cases := map[string]DetailLevel{
		"full":    DetailFull,
		"minimal": DetailMinimal,
		"":        DetailMinimal,
		"FULL":    DetailMinimal,
		" full":   DetailMinimal,
		"verbose": DetailMinimal,
	}
	for in, want := range cases {
		if got := levelFromDB(in); got != want {
			t.Errorf("levelFromDB(%q) = %q, want %q", in, got, want)
		}
	}
	if (MessageContext{}).minimal() {
		t.Error("the MessageContext zero value must stay full")
	}
}
