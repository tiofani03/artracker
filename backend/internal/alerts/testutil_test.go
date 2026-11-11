package alerts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/db"
)

// testDB connects to TEST_DATABASE_URL, migrates, and truncates the tables so
// every test starts empty. Skips when the variable is unset. Packages share one
// database, so run with -p 1.
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

// seedProject inserts an org and a project with the default alert rules.
func seedProject(t *testing.T, pool *pgxpool.Pool) (orgID, projectID int64) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ('Acme','acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO projects(org_id, name, slug, platform) VALUES ($1,'Checkout Web','checkout-web','web') RETURNING id`, orgID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := pool.Exec(ctx, SeedRulesSQL, projectID); err != nil {
		t.Fatalf("seed rules: %v", err)
	}
	return orgID, projectID
}

var issueSeq int

// seedIssue inserts an issue with the given status and returns its id.
func seedIssue(t *testing.T, pool *pgxpool.Pool, projectID int64, status string) int64 {
	t.Helper()
	issueSeq++
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO issues(project_id, fingerprint, title, status) VALUES ($1, $2, 'TypeError: boom', $3) RETURNING id`,
		projectID, "fp-"+strconv.Itoa(issueSeq), status).Scan(&id)
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return id
}

type sentMsg struct {
	chat string
	HTML string
}

// fakeNotifier records messages; errFor returns a canned error per chat id.
type fakeNotifier struct {
	mu     sync.Mutex
	sent   []sentMsg
	errFor map[int64]error
}

func (f *fakeNotifier) SendMessage(_ context.Context, chat string, html string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, perr := strconv.ParseInt(chat, 10, 64); perr == nil {
		if err := f.errFor[id]; err != nil {
			return err
		}
	}
	f.sent = append(f.sent, sentMsg{chat: chat, HTML: html})
	return nil
}

// Sent returns a copy of the messages recorded so far.
func (f *fakeNotifier) Sent() []sentMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentMsg(nil), f.sent...)
}

// strictRequest is one call the strict Bot API server received. Body is the
// decoded JSON body of a POST; for a GET it is the query string as a map.
type strictRequest struct {
	Token  string
	Method string
	Body   map[string]any
}

type strictReply struct {
	status int
	body   string
}

// strictTelegram poses as the Telegram Bot API at /bot<token>/<method>. It
// serves only sendMessage, getMe and getChat; any other method fails the test,
// which is how send-only delivery is enforced.
type strictTelegram struct {
	URL string

	t      errorfer
	mu     sync.Mutex
	reqs   []strictRequest
	byMeth map[string]strictReply
	byTok  map[string]strictReply // key: token + "/" + method
}

// errorfer is the slice of *testing.T the server reports through; the helper's
// own test swaps in a recorder to prove an unexpected method is an error.
type errorfer interface {
	Errorf(format string, args ...any)
}

func newStrictTelegramServer(t *testing.T) *strictTelegram {
	t.Helper()
	return newStrictTelegramServerReporting(t, t)
}

func newStrictTelegramServerReporting(t *testing.T, rep errorfer) *strictTelegram {
	t.Helper()
	st := &strictTelegram{t: rep, byMeth: map[string]strictReply{}, byTok: map[string]strictReply{}}
	srv := httptest.NewServer(http.HandlerFunc(st.handle))
	t.Cleanup(srv.Close)
	st.URL = srv.URL
	return st
}

// SetReply overrides the reply for a method, for every token.
func (s *strictTelegram) SetReply(method string, status int, jsonBody string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byMeth[method] = strictReply{status, jsonBody}
}

// SetReplyForToken overrides the reply for a method for one token only; it
// wins over SetReply.
func (s *strictTelegram) SetReplyForToken(token, method string, status int, jsonBody string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byTok[token+"/"+method] = strictReply{status, jsonBody}
}

// Requests returns a copy of every request recorded so far, in arrival order.
func (s *strictTelegram) Requests() []strictRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]strictRequest(nil), s.reqs...)
}

// Calls returns the recorded requests for one method.
func (s *strictTelegram) Calls(method string) []strictRequest {
	var out []strictRequest
	for _, r := range s.Requests() {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

func (s *strictTelegram) handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "bot") {
		s.t.Errorf("strict telegram: unexpected path %q", r.URL.Path)
		http.NotFound(w, r)
		return
	}
	token, method := strings.TrimPrefix(parts[0], "bot"), parts[1]
	body := map[string]any{}
	if r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&body)
	} else {
		for k, v := range r.URL.Query() {
			body[k] = v[0]
		}
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, strictRequest{Token: token, Method: method, Body: body})
	rep, ok := s.byTok[token+"/"+method]
	if !ok {
		rep, ok = s.byMeth[method]
	}
	s.mu.Unlock()

	if !ok {
		switch method {
		case "sendMessage":
			rep = strictReply{http.StatusOK, `{"ok":true,"result":{"message_id":1}}`}
		case "getMe":
			rep = strictReply{http.StatusOK, `{"ok":true,"result":{"id":424242,"username":"TestBot"}}`}
		case "getChat":
			rep = strictReply{http.StatusOK, `{"ok":true,"result":{"id":-100123,"title":"Ops","type":"supergroup"}}`}
		default:
			s.t.Errorf("strict telegram: the platform is send-only, unexpected method %q", method)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":404,"description":"Not Found"}`))
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rep.status)
	_, _ = w.Write([]byte(rep.body))
}
