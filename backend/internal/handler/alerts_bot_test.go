package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tiofani03/artracker/backend/internal/alerts"
)

// hBotToken matches the Bot API token shape; it never reaches real Telegram.
const hBotToken = "123456789:AAHandlerFakeTokenForTests_abcdefXYZ"

type botAPICall struct {
	token, method string
	body          map[string]any
}

type botAPIReply struct {
	status int
	body   string
}

// fakeBotAPI is a strict Bot API at /bot<token>/<method>: it serves
// sendMessage, getMe and getChat and fails the test for any other method.
// Production code calls only sendMessage and getMe; see methods().
type fakeBotAPI struct {
	t       *testing.T
	URL     string
	mu      sync.Mutex
	calls   []botAPICall
	replies map[string]botAPIReply
}

func newFakeBotAPI(t *testing.T) *fakeBotAPI {
	t.Helper()
	f := &fakeBotAPI{t: t, replies: map[string]botAPIReply{}}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	return f
}

func (f *fakeBotAPI) set(method string, status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[method] = botAPIReply{status, body}
}

func (f *fakeBotAPI) reset(method string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.replies, method)
}

func (f *fakeBotAPI) callsOf(method string) []botAPICall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []botAPICall
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// methods lists the Bot API methods called so far, in order.
func (f *fakeBotAPI) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.method)
	}
	return out
}

func (f *fakeBotAPI) handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "bot") {
		f.t.Errorf("fake Bot API: unexpected path %q", r.URL.Path)
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
	f.mu.Lock()
	f.calls = append(f.calls, botAPICall{token: token, method: method, body: body})
	rep, ok := f.replies[method]
	f.mu.Unlock()
	if !ok {
		switch method {
		case "sendMessage":
			rep = botAPIReply{http.StatusOK, `{"ok":true,"result":{"message_id":1}}`}
		case "getMe":
			rep = botAPIReply{http.StatusOK, `{"ok":true,"result":{"id":424242,"username":"TestBot"}}`}
		case "getChat":
			rep = botAPIReply{http.StatusOK, `{"ok":true,"result":{"id":-100123,"title":"Ops","type":"supergroup"}}`}
		default:
			f.t.Errorf("fake Bot API: the platform is send-only, unexpected method %q", method)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":404,"description":"Not Found"}`))
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rep.status)
	_, _ = w.Write([]byte(rep.body))
}

func testLocalSealer(t *testing.T) *alerts.LocalSealer {
	t.Helper()
	sl, err := alerts.NewLocalSealer(bytes.Repeat([]byte{0x24}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	return sl
}

// botEnv is org A (owner, admin, member, viewer; projects pid and other), org
// B (its own admin and project), an outsider, and a tenant-mode service whose
// Bot API is the strict fake.
type botEnv struct {
	*testEnv
	api                                    *fakeBotAPI
	svc                                    *alerts.Service
	org, orgB                              int64
	pid, other, pidB                       int64
	adminID                                int64
	owner, admin, member, viewer, outsider map[string]string
	adminB                                 map[string]string
}

func newBotEnv(t *testing.T, name string) *botEnv {
	t.Helper()
	e := newTestEnv(t)
	b := &botEnv{testEnv: e, api: newFakeBotAPI(t)}
	b.org = e.addOrg(t, name)
	b.orgB = e.addOrg(t, name+"-b")
	add := func(role string, org int64) (int64, map[string]string) {
		id, hdr := e.addUser(t, fmt.Sprintf("%s-%s-%d", name, role, org))
		e.addMember(t, org, id, role)
		return id, hdr
	}
	_, b.owner = add("owner", b.org)
	b.adminID, b.admin = add("admin", b.org)
	_, b.member = add("member", b.org)
	_, b.viewer = add("viewer", b.org)
	_, b.adminB = add("admin", b.orgB)
	_, b.outsider = e.addUser(t, name+"-outsider")
	b.pid = e.addProject(t, b.org, name+"-app")
	b.other = e.addProject(t, b.org, name+"-other")
	b.pidB = e.addProject(t, b.orgB, name+"-b-app")
	b.svc = alerts.NewService(e.pool, nil, "http://app").WithMode(alerts.ModeTenant).
		WithSealer(testLocalSealer(t)).WithAPIBase(b.api.URL)
	e.h.WithAlerts(b.svc)
	return b
}

func (b *botEnv) botPath(org int64) string {
	return fmt.Sprintf("/api/v1/orgs/%d/alerts/telegram-bot", org)
}
func (b *botEnv) chanPath(pid int64) string {
	return fmt.Sprintf("/api/v1/projects/%d/alerts/channels", pid)
}

func (b *botEnv) call(t *testing.T, method, path, body string, hdr map[string]string) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	resp := b.do(t, method, path, r, hdr)
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (b *botEnv) putBot(t *testing.T) {
	t.Helper()
	if code, body := b.call(t, "PUT", b.botPath(b.org), `{"token":"`+hBotToken+`"}`, b.admin); code != 200 {
		t.Fatalf("PUT bot = %d %s", code, body)
	}
}

func (b *botEnv) insertManual(t *testing.T, pid int64, dest string) int64 {
	t.Helper()
	var id int64
	if err := b.pool.QueryRow(context.Background(),
		`INSERT INTO alert_channels(project_id, origin, destination, title) VALUES ($1,'manual',$2,'Manual') RETURNING id`,
		pid, dest).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (b *botEnv) insertLinked(t *testing.T, pid, chatID int64) int64 {
	t.Helper()
	var id int64
	if err := b.pool.QueryRow(context.Background(),
		`INSERT INTO alert_channels(project_id, telegram_chat_id, title, chat_type) VALUES ($1,$2,'Linked','group') RETURNING id`,
		pid, chatID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func wantError(t *testing.T, what string, code int, body []byte, wantCode int, wantMsg string) {
	t.Helper()
	if code != wantCode {
		t.Errorf("%s = %d %s, want %d", what, code, body, wantCode)
		return
	}
	if wantMsg == "" {
		return
	}
	var e APIError
	if err := json.Unmarshal(body, &e); err != nil || e.Error != wantMsg {
		t.Errorf("%s body = %s, want error %q", what, body, wantMsg)
	}
}

// captureSlog routes the default slog logger to a buffer for the test.
func captureSlog(t *testing.T) func() string {
	t.Helper()
	var mu sync.Mutex
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&syncWriter{mu: &mu, w: &buf}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
}

type syncWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

type botResp struct {
	Configured   bool    `json:"configured"`
	BotUsername  string  `json:"bot_username"`
	Status       string  `json:"status"`
	StatusReason string  `json:"status_reason"`
	DetailLevel  string  `json:"detail_level"`
	UpdatedAt    *string `json:"updated_at"`
	Mode         string  `json:"mode"`
}

func decodeBot(t *testing.T, body []byte) botResp {
	t.Helper()
	var v botResp
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode bot %s: %v", body, err)
	}
	return v
}

func TestBotEndpointsRoleMatrix(t *testing.T) {
	b := newBotEnv(t, "bot-roles")
	p := b.botPath(b.org)

	for name, hdr := range map[string]map[string]string{"owner": b.owner, "admin": b.admin, "member": b.member, "viewer": b.viewer} {
		code, body := b.call(t, "GET", p, "", hdr)
		if code != 200 {
			t.Errorf("%s GET = %d %s", name, code, body)
			continue
		}
		if v := decodeBot(t, body); v.Configured || v.Mode != "tenant" {
			t.Errorf("%s GET = %+v", name, v)
		}
	}
	if code, _ := b.call(t, "GET", p, "", b.outsider); code != 403 && code != 404 {
		t.Errorf("outsider GET = %d", code)
	}
	if code, _ := b.call(t, "GET", p, "", nil); code != 401 {
		t.Errorf("anonymous GET = %d", code)
	}

	put := `{"token":"` + hBotToken + `"}`
	for name, hdr := range map[string]map[string]string{"member": b.member, "viewer": b.viewer} {
		for _, m := range []struct{ method, body string }{{"PUT", put}, {"PATCH", `{"detail_level":"full"}`}, {"DELETE", ""}} {
			if code, _ := b.call(t, m.method, p, m.body, hdr); code != 403 {
				t.Errorf("%s %s = %d, want 403", name, m.method, code)
			}
		}
	}
	for _, m := range []struct{ method, body string }{{"PUT", put}, {"PATCH", `{"detail_level":"full"}`}, {"DELETE", ""}} {
		if code, _ := b.call(t, m.method, p, m.body, b.outsider); code != 403 && code != 404 {
			t.Errorf("outsider %s = %d", m.method, code)
		}
		// Org B's admin cannot touch org A's bot.
		if code, _ := b.call(t, m.method, p, m.body, b.adminB); code != 404 {
			t.Errorf("org B admin %s on org A = %d, want 404", m.method, code)
		}
	}
	if n := len(b.api.callsOf("getMe")); n != 0 {
		t.Fatalf("getMe called %d times by unauthorised requests", n)
	}

	if code, body := b.call(t, "PUT", p, put, b.admin); code != 200 {
		t.Fatalf("admin PUT = %d %s", code, body)
	}
	if code, body := b.call(t, "PATCH", p, `{"detail_level":"full"}`, b.owner); code != 200 {
		t.Fatalf("owner PATCH = %d %s", code, body)
	}
	if code, body := b.call(t, "PUT", p, put, b.owner); code != 200 {
		t.Fatalf("owner PUT = %d %s", code, body)
	}
	if code, body := b.call(t, "PATCH", p, `{"detail_level":"minimal"}`, b.admin); code != 200 {
		t.Fatalf("admin PATCH = %d %s", code, body)
	}
	// Org B is still unconfigured; org A's bot is untouched by org B's admin.
	if code, body := b.call(t, "GET", b.botPath(b.orgB), "", b.adminB); code != 200 || decodeBot(t, body).Configured {
		t.Errorf("org B GET = %d %s", code, body)
	}
	if code, body := b.call(t, "DELETE", b.botPath(b.orgB), "", b.adminB); code != 204 {
		t.Errorf("org B DELETE = %d %s", code, body)
	}
	if n := b.count(t, fmt.Sprintf(`SELECT count(*) FROM alert_bots WHERE org_id=%d`, b.org)); n != 1 {
		t.Fatalf("org A bots = %d, want 1", n)
	}
	if code, _ := b.call(t, "DELETE", p, "", b.admin); code != 204 {
		t.Errorf("admin DELETE = %d", code)
	}
	if code, _ := b.call(t, "DELETE", p, "", b.owner); code != 204 {
		t.Errorf("owner DELETE = %d", code)
	}
}

func TestPutBotStoresAndHidesToken(t *testing.T) {
	b := newBotEnv(t, "bot-store")
	code, body := b.call(t, "PUT", b.botPath(b.org), `{"token":"`+hBotToken+`","detail_level":"full"}`, b.admin)
	if code != 200 {
		t.Fatalf("PUT = %d %s", code, body)
	}
	v := decodeBot(t, body)
	if !v.Configured || v.BotUsername != "TestBot" || v.Status != "active" || v.DetailLevel != "full" || v.Mode != "tenant" || v.UpdatedAt == nil {
		t.Fatalf("PUT view = %+v", v)
	}
	if calls := b.api.callsOf("getMe"); len(calls) != 1 || calls[0].token != hBotToken {
		t.Fatalf("getMe calls = %+v", calls)
	}

	code, body = b.call(t, "GET", b.botPath(b.org), "", b.viewer)
	if code != 200 {
		t.Fatalf("GET = %d %s", code, body)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"token", "token_ciphertext", "ciphertext", "key_id"} {
		if _, ok := raw[k]; ok {
			t.Errorf("GET exposes %q: %s", k, body)
		}
	}
	for _, k := range []string{"configured", "bot_username", "status", "status_reason", "detail_level", "updated_at", "mode"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("GET missing %q: %s", k, body)
		}
	}
	if strings.Contains(string(body), hBotToken) || strings.Contains(string(body), hBotToken[:9]) {
		t.Fatalf("GET leaks the token: %s", body)
	}
	if v := decodeBot(t, body); v.BotUsername != "TestBot" || v.DetailLevel != "full" {
		t.Fatalf("GET view = %+v", v)
	}
	if n := b.count(t, fmt.Sprintf(`SELECT count(*) FROM alert_bots WHERE org_id=%d AND position(convert_to('%s','UTF8') in token_ciphertext) = 0 AND created_by=%d`,
		b.org, hBotToken, b.adminID)); n != 1 {
		t.Fatal("token not stored sealed (or created_by wrong)")
	}

	// Replacing keeps the level when none is given.
	code, body = b.call(t, "PUT", b.botPath(b.org), `{"token":"`+hBotToken+`"}`, b.admin)
	if code != 200 || decodeBot(t, body).DetailLevel != "full" {
		t.Fatalf("replace = %d %s", code, body)
	}
}

func TestBotEndpointsNeverEchoToken(t *testing.T) {
	b := newBotEnv(t, "bot-echo")
	logs := captureSlog(t)
	p := b.botPath(b.org)
	put := `{"token":"` + hBotToken + `"}`
	var bodies [][]byte
	record := func(what string, wantCode int, code int, body []byte) {
		t.Helper()
		if code != wantCode {
			t.Errorf("%s = %d %s, want %d", what, code, body, wantCode)
		}
		bodies = append(bodies, body)
	}

	code, body := b.call(t, "PUT", p, put, b.admin)
	record("PUT ok", 200, code, body)
	code, body = b.call(t, "GET", p, "", b.viewer)
	record("GET", 200, code, body)
	code, body = b.call(t, "GET", b.chanPath(b.pid), "", b.viewer)
	record("GET channels", 200, code, body)

	b.api.set("getMe", 401, `{"ok":false,"error_code":401,"description":"Unauthorized `+hBotToken+`"}`)
	code, body = b.call(t, "PUT", p, put, b.admin)
	record("PUT 401", 400, code, body)
	b.api.set("getMe", 500, `{"ok":false,"error_code":500,"description":"boom `+hBotToken+`"}`)
	code, body = b.call(t, "PUT", p, put, b.admin)
	record("PUT 500", 502, code, body)
	b.api.reset("getMe")

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	b.svc.WithAPIBase(closed.URL)
	code, body = b.call(t, "PUT", p, put, b.admin)
	record("PUT unreachable", 502, code, body)
	b.svc.WithAPIBase(b.api.URL)

	code, body = b.call(t, "PUT", p, `{"token":"`+hBotToken+`","detail_level":"loud"}`, b.admin)
	record("PUT bad level", 400, code, body)
	code, body = b.call(t, "PUT", p, `{"token":"`+hBotToken+`","extra":1}`, b.admin)
	record("PUT unknown field", 400, code, body)
	code, body = b.call(t, "PUT", p, `{"token":"`+hBotToken+strings.Repeat("A", 300)+`"}`, b.admin)
	record("PUT oversize", 400, code, body)
	code, body = b.call(t, "PUT", p, `{"token":"`+hBotToken, b.admin)
	record("PUT truncated JSON", 400, code, body)
	code, body = b.call(t, "PUT", p, put, b.member)
	record("PUT member", 403, code, body)
	b.h.WithAlerts(alerts.NewService(b.pool, nil, "http://app").WithMode(alerts.ModeTenant).WithAPIBase(b.api.URL))
	code, body = b.call(t, "PUT", p, put, b.admin)
	record("PUT no sealer", 503, code, body)
	b.h.WithAlerts(alerts.NewService(b.pool, nil, "http://app"))
	code, body = b.call(t, "PUT", p, put, b.admin)
	record("PUT shared", 404, code, body)

	out := logs()
	for i, body := range append(bodies, []byte(out)) {
		for _, needle := range []string{hBotToken, hBotToken[:9] + ":", strings.SplitN(hBotToken, ":", 2)[1]} {
			if strings.Contains(string(body), needle) {
				t.Fatalf("output %d leaks the token (%q): %s", i, needle, body)
			}
		}
	}
}

func TestPutBotErrors(t *testing.T) {
	b := newBotEnv(t, "bot-err")
	p := b.botPath(b.org)
	put := `{"token":"` + hBotToken + `"}`

	code, body := b.call(t, "PUT", p, `{"token":"abc"}`, b.admin)
	wantError(t, "bad format", code, body, 400, "invalid bot token")
	if n := len(b.api.callsOf("getMe")); n != 0 {
		t.Fatalf("bad format reached Telegram %d times", n)
	}
	oversize := `{"token":"123456789:` + strings.Repeat("A", 250) + `"}`
	code, body = b.call(t, "PUT", p, oversize, b.admin)
	wantError(t, "oversize token", code, body, 400, "invalid bot token")
	if n := len(b.api.callsOf("getMe")); n != 0 {
		t.Fatalf("an oversize token reached Telegram %d times", n)
	}
	for _, bad := range []string{`not json`, `null`, `[]`, `[{"token":"x"}]`, `"tok"`, `{"token":123}`, `{"tok":"x"}`, `{}`, ``, `{"token":"` + hBotToken + `"} {}`} {
		code, body := b.call(t, "PUT", p, bad, b.admin)
		if code != 400 {
			t.Errorf("PUT %q = %d %s, want 400", bad, code, body)
		}
	}
	code, body = b.call(t, "PUT", p, `{"token":"`+hBotToken+`","detail_level":"loud"}`, b.admin)
	wantError(t, "bad level", code, body, 400, "invalid detail level")
	code, body = b.call(t, "PUT", p, `{"token":"`+hBotToken+`","detail_level":""}`, b.admin)
	wantError(t, "empty level", code, body, 400, "invalid detail level")

	b.api.set("getMe", 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
	code, body = b.call(t, "PUT", p, put, b.admin)
	wantError(t, "telegram 401", code, body, 400, "invalid bot token")
	b.api.set("getMe", 500, `{"ok":false,"error_code":500,"description":"upstream detail"}`)
	code, body = b.call(t, "PUT", p, put, b.admin)
	wantError(t, "telegram 500", code, body, 502, "could not reach Telegram")
	b.api.reset("getMe")
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	b.svc.WithAPIBase(closed.URL)
	code, body = b.call(t, "PUT", p, put, b.admin)
	wantError(t, "unreachable", code, body, 502, "could not reach Telegram")
	b.svc.WithAPIBase(b.api.URL)
	if n := b.count(t, `SELECT count(*) FROM alert_bots`); n != 0 {
		t.Fatalf("a failed PUT stored a bot (%d rows)", n)
	}

	b.h.WithAlerts(alerts.NewService(b.pool, nil, "http://app").WithMode(alerts.ModeTenant).WithAPIBase(b.api.URL))
	code, body = b.call(t, "PUT", p, put, b.admin)
	wantError(t, "no sealer", code, body, 503, "alerts master key is not configured")

	b.h.WithAlerts(alerts.NewService(b.pool, nil, "http://app"))
	code, body = b.call(t, "PUT", p, put, b.admin)
	wantError(t, "shared PUT", code, body, 404, "tenant bots are disabled in this deployment")
	code, body = b.call(t, "PATCH", p, `{"detail_level":"full"}`, b.admin)
	wantError(t, "shared PATCH", code, body, 404, "tenant bots are disabled in this deployment")
	code, body = b.call(t, "DELETE", p, "", b.admin)
	wantError(t, "shared DELETE", code, body, 404, "tenant bots are disabled in this deployment")
	code, body = b.call(t, "GET", p, "", b.viewer)
	if code != 200 {
		t.Fatalf("shared GET = %d %s", code, body)
	}
	if v := decodeBot(t, body); v.Configured || v.Mode != "shared" || v.DetailLevel != "minimal" {
		t.Fatalf("shared GET = %+v", v)
	}

	b.h.WithAlerts(nil)
	code, body = b.call(t, "GET", p, "", b.viewer)
	if v := decodeBot(t, body); code != 200 || v.Mode != "shared" || v.DetailLevel != "minimal" {
		t.Fatalf("GET without service = %d %s", code, body)
	}
	code, body = b.call(t, "PUT", p, put, b.admin)
	wantError(t, "PUT without service", code, body, 404, "tenant bots are disabled in this deployment")
}

func TestBotEndpointsWithoutMasterKey(t *testing.T) {
	b := newBotEnv(t, "bot-nokey")
	b.h.WithAlerts(alerts.NewService(b.pool, nil, "http://app").WithMode(alerts.ModeTenant).WithAPIBase(b.api.URL))
	p := b.botPath(b.org)

	code, body := b.call(t, "GET", p, "", b.viewer)
	if code != 200 {
		t.Fatalf("GET = %d %s", code, body)
	}
	if v := decodeBot(t, body); v.Configured || v.Mode != "tenant" {
		t.Fatalf("GET = %+v", v)
	}
	if _, err := b.pool.Exec(context.Background(), `
INSERT INTO alert_bots(org_id, token_ciphertext, key_id, bot_username, status, detail_level)
VALUES ($1, '\x00', 'v1', 'StoredBot', 'active', 'full')`, b.org); err != nil {
		t.Fatal(err)
	}
	code, body = b.call(t, "GET", p, "", b.viewer)
	if code != 200 {
		t.Fatalf("GET = %d %s", code, body)
	}
	if v := decodeBot(t, body); !v.Configured || v.BotUsername != "StoredBot" || v.Status != "active" {
		t.Fatalf("GET with a stored bot = %+v", v)
	}
	code, body = b.call(t, "PUT", p, `{"token":"`+hBotToken+`"}`, b.admin)
	wantError(t, "PUT", code, body, 503, "alerts master key is not configured")
	if n := len(b.api.callsOf("getMe")); n != 0 {
		t.Fatalf("getMe called %d times without a master key", n)
	}
}

func TestPatchAndDeleteBot(t *testing.T) {
	b := newBotEnv(t, "bot-patch")
	p := b.botPath(b.org)

	code, body := b.call(t, "PATCH", p, `{"detail_level":"full"}`, b.admin)
	wantError(t, "PATCH without bot", code, body, 404, "no bot configured")

	b.putBot(t)
	code, body = b.call(t, "PATCH", p, `{"detail_level":"full"}`, b.admin)
	if code != 200 {
		t.Fatalf("PATCH = %d %s", code, body)
	}
	if v := decodeBot(t, body); v.DetailLevel != "full" || v.BotUsername != "TestBot" || v.Mode != "tenant" {
		t.Fatalf("PATCH view = %+v", v)
	}
	code, body = b.call(t, "PATCH", p, `{"detail_level":"loud"}`, b.admin)
	wantError(t, "PATCH bad level", code, body, 400, "invalid detail level")
	for _, bad := range []string{`{}`, `null`, `[]`, `not json`, `{"detail_level":1}`, `{"token":"x"}`} {
		if code, body := b.call(t, "PATCH", p, bad, b.admin); code != 400 {
			t.Errorf("PATCH %q = %d %s, want 400", bad, code, body)
		}
	}
	if v := decodeBot(t, func() []byte { _, b2 := b.call(t, "GET", p, "", b.viewer); return b2 }()); v.DetailLevel != "full" {
		t.Fatalf("level after rejected PATCHes = %q", v.DetailLevel)
	}

	if code, body := b.call(t, "DELETE", p, "", b.admin); code != 204 {
		t.Fatalf("DELETE = %d %s", code, body)
	}
	if code, body := b.call(t, "DELETE", p, "", b.admin); code != 204 {
		t.Fatalf("second DELETE = %d %s", code, body)
	}
	code, body = b.call(t, "GET", p, "", b.viewer)
	if v := decodeBot(t, body); code != 200 || v.Configured {
		t.Fatalf("GET after DELETE = %d %+v", code, v)
	}
}

type channelResp struct {
	ID          int64   `json:"id"`
	Kind        string  `json:"kind"`
	Title       string  `json:"title"`
	ChatType    string  `json:"chat_type"`
	Enabled     bool    `json:"enabled"`
	Origin      string  `json:"origin"`
	Destination string  `json:"destination"`
	Active      bool    `json:"active"`
	Warning     *string `json:"warning"`
}

func TestCreateManualChannel(t *testing.T) {
	b := newBotEnv(t, "bot-chan")
	p := b.chanPath(b.pid)
	body := func(chat, label string) string {
		out, _ := json.Marshal(map[string]string{"chat": chat, "label": label})
		return string(out)
	}

	code, resp := b.call(t, "POST", p, body("-1001234567890", "Ops"), b.admin)
	wantError(t, "no bot", code, resp, 409, "connect a Telegram bot first")
	if _, err := b.pool.Exec(context.Background(), `
INSERT INTO alert_bots(org_id, token_ciphertext, key_id, bot_username, status, status_reason)
VALUES ($1, '\x00', 'v1', 'DeadBot', 'invalid', 'token rejected by Telegram')`, b.org); err != nil {
		t.Fatal(err)
	}
	code, resp = b.call(t, "POST", p, body("-1001234567890", "Ops"), b.admin)
	wantError(t, "invalid bot", code, resp, 409, "connect a Telegram bot first")
	b.putBot(t)

	if code, _ := b.call(t, "POST", p, body("-1001234567890", "Ops"), b.viewer); code != 403 {
		t.Errorf("viewer POST = %d, want 403", code)
	}
	if code, _ := b.call(t, "POST", p, body("-1001234567890", "Ops"), b.member); code != 403 {
		t.Errorf("member POST = %d, want 403", code)
	}

	for _, chat := range []string{"-1001234567890", "123456", "@my_channel"} {
		code, resp := b.call(t, "POST", p, body(chat, "Label "+chat), b.admin)
		if code != 201 {
			t.Errorf("POST %q = %d %s, want 201", chat, code, resp)
			continue
		}
		var ch channelResp
		if err := json.Unmarshal(resp, &ch); err != nil {
			t.Fatal(err)
		}
		if ch.Origin != "manual" || ch.Destination != chat || !ch.Active || ch.Title != "Label "+chat ||
			ch.ChatType != "" || ch.Warning != nil || ch.Kind != "telegram" || !ch.Enabled {
			t.Errorf("POST %q channel = %+v (%s)", chat, ch, resp)
		}
		if strings.Contains(string(resp), `"warning"`) {
			t.Errorf("POST %q response carries a warning: %s", chat, resp)
		}
		q := fmt.Sprintf(`SELECT count(*) FROM alert_channels WHERE id=%d AND project_id=%d AND origin='manual'
			AND telegram_chat_id IS NULL AND created_by=%d AND destination='%s'`, ch.ID, b.pid, b.adminID, chat)
		if n := b.count(t, q); n != 1 {
			t.Errorf("row for %q not stored as expected", chat)
		}
	}
	// Creating a channel asks Telegram nothing: the platform only ever calls
	// getMe (when a token is saved) and sendMessage.
	if got := b.api.methods(); len(got) != 1 || got[0] != "getMe" {
		t.Errorf("Bot API methods called = %v, want only the getMe from PUT", got)
	}

	for _, chat := range []string{"00123", "-0123456", "+123456", " 123456", "123456 ", "12", "@a", "abc", "", "@my channel",
		"1234567890123456789012", "@1abcde", "-", "@" + strings.Repeat("a", 33), "@my-channel", "-123456789012345678901"} {
		code, resp := b.call(t, "POST", p, body(chat, "Ops"), b.admin)
		wantError(t, fmt.Sprintf("chat %q", chat), code, resp, 400, "invalid chat")
	}

	for i, label := range []string{"Ops – prod alerts", "Équipe 🚨 on-call"} {
		code, resp := b.call(t, "POST", p, body(fmt.Sprintf("-10055%d0", i), label), b.admin)
		if code != 201 {
			t.Errorf("label %q = %d %s, want 201", label, code, resp)
		}
	}
	emoji80 := strings.Repeat("😀", 80)
	code, resp = b.call(t, "POST", p, body("-100555", emoji80), b.admin)
	if code != 201 {
		t.Errorf("80-emoji label = %d %s", code, resp)
	}
	for _, label := range []string{strings.Repeat("😀", 81), strings.Repeat("a", 81), "", "   ", "\t\n ",
		"a\u0000b", "a\nb", "a\tb", "\u0085", "Ops\r", "x\u007f"} {
		code, resp := b.call(t, "POST", p, body("-100556", label), b.admin)
		wantError(t, fmt.Sprintf("label %q", label), code, resp, 400, "invalid label")
	}
	for _, bad := range []string{`not json`, `null`, `[]`, `{"chat":123,"label":"x"}`} {
		if code, resp := b.call(t, "POST", p, bad, b.admin); code != 400 {
			t.Errorf("POST %q = %d %s, want 400", bad, code, resp)
		}
	}

	code, resp = b.call(t, "POST", p, body("@my_channel", "Again"), b.admin)
	wantError(t, "duplicate", code, resp, 409, "chat already added")

	if got := b.api.methods(); len(got) != 1 || got[0] != "getMe" {
		t.Errorf("Bot API methods called after all creates = %v, want only getMe", got)
	}

	// Shared mode has no manual channels.
	b.h.WithAlerts(alerts.NewService(b.pool, nil, "http://app").WithBotUsername("ArtBot"))
	code, resp = b.call(t, "POST", p, body("-100888", "Shared"), b.admin)
	wantError(t, "shared mode", code, resp, 400, "manual channels are only available in tenant mode")
	b.h.WithAlerts(nil)
	code, resp = b.call(t, "POST", p, body("-100888", "Shared"), b.admin)
	wantError(t, "no service", code, resp, 400, "manual channels are only available in tenant mode")
}

func TestChannelListShowsOriginAndActive(t *testing.T) {
	b := newBotEnv(t, "bot-list")
	linked := b.insertLinked(t, b.pid, 987654321012)
	manual := b.insertManual(t, b.pid, "-1009876")

	type listResp struct {
		Telegram *struct {
			Configured  bool   `json:"configured"`
			BotUsername string `json:"bot_username"`
		} `json:"telegram"`
		Mode  string        `json:"mode"`
		Bot   *botResp      `json:"bot"`
		Items []channelResp `json:"items"`
	}
	get := func() listResp {
		t.Helper()
		code, body := b.call(t, "GET", b.chanPath(b.pid), "", b.viewer)
		if code != 200 {
			t.Fatalf("GET channels = %d %s", code, body)
		}
		if strings.Contains(string(body), "987654321012") || strings.Contains(string(body), "chat_id") {
			t.Fatalf("list leaks the linked chat id: %s", body)
		}
		var out listResp
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		if out.Telegram == nil || out.Bot == nil || len(out.Items) != 2 {
			t.Fatalf("list = %s", body)
		}
		return out
	}
	byID := func(out listResp, id int64) channelResp {
		for _, c := range out.Items {
			if c.ID == id {
				return c
			}
		}
		t.Fatalf("channel %d missing", id)
		return channelResp{}
	}

	out := get()
	if out.Mode != "tenant" || out.Bot.Mode != "tenant" || out.Bot.Configured {
		t.Errorf("tenant list mode=%q bot=%+v", out.Mode, out.Bot)
	}
	if l := byID(out, linked); l.Origin != "linked" || l.Active || l.Destination != "" {
		t.Errorf("linked in tenant mode = %+v", l)
	}
	if m := byID(out, manual); m.Origin != "manual" || !m.Active || m.Destination != "-1009876" {
		t.Errorf("manual in tenant mode = %+v", m)
	}
	b.putBot(t)
	if out := get(); !out.Bot.Configured || out.Bot.BotUsername != "TestBot" || out.Bot.Status != "active" {
		t.Errorf("bot after PUT = %+v", out.Bot)
	}

	b.h.WithAlerts(alerts.NewService(b.pool, &fakeNotifier{}, "http://app").WithBotUsername("ArtBot"))
	out = get()
	if out.Mode != "shared" || out.Bot.Mode != "shared" || out.Bot.Configured || out.Bot.DetailLevel != "minimal" ||
		!out.Telegram.Configured || out.Telegram.BotUsername != "ArtBot" {
		t.Errorf("shared list mode=%q bot=%+v telegram=%+v", out.Mode, out.Bot, out.Telegram)
	}
	if l := byID(out, linked); l.Origin != "linked" || !l.Active {
		t.Errorf("linked in shared mode = %+v", l)
	}
	if m := byID(out, manual); m.Origin != "manual" || m.Active {
		t.Errorf("manual in shared mode = %+v", m)
	}
}

func TestLinkCodeDisabledInTenantMode(t *testing.T) {
	b := newBotEnv(t, "bot-link")
	path := b.chanPath(b.pid) + "/link-code"
	if code, _ := b.call(t, "POST", path, "", b.viewer); code != 403 {
		t.Errorf("viewer = %d, want 403", code)
	}
	code, body := b.call(t, "POST", path, "", b.admin)
	wantError(t, "tenant link-code", code, body, 404, "link flow is disabled in tenant mode")
	if n := b.count(t, `SELECT count(*) FROM alert_link_codes`); n != 0 {
		t.Fatalf("link codes = %d, want 0", n)
	}
}

func TestTestChannelUsesOrgBot(t *testing.T) {
	b := newBotEnv(t, "bot-test")
	b.putBot(t)
	code, body := b.call(t, "POST", b.chanPath(b.pid), `{"chat":"@ops_room","label":"Ops"}`, b.admin)
	if code != 201 {
		t.Fatalf("create = %d %s", code, body)
	}
	var ch channelResp
	if err := json.Unmarshal(body, &ch); err != nil {
		t.Fatal(err)
	}
	tp := fmt.Sprintf("%s/%d/test", b.chanPath(b.pid), ch.ID)
	if code, _ := b.call(t, "POST", tp, "", b.viewer); code != 403 {
		t.Errorf("viewer test = %d", code)
	}
	if code, body := b.call(t, "POST", tp, "", b.admin); code != 204 {
		t.Fatalf("test = %d %s", code, body)
	}
	sends := b.api.callsOf("sendMessage")
	if len(sends) != 1 || sends[0].token != hBotToken || sends[0].body["chat_id"] != "@ops_room" {
		t.Fatalf("sendMessage calls = %+v", sends)
	}

	b.api.set("sendMessage", 400, `{"ok":false,"error_code":400,"description":"Bad Request: secretdetail"}`)
	code, body = b.call(t, "POST", tp, "", b.admin)
	wantError(t, "telegram failure", code, body, 502, "telegram rejected the test message")
	if strings.Contains(string(body), "secretdetail") {
		t.Errorf("upstream text echoed: %s", body)
	}
	b.api.set("sendMessage", 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
	code, body = b.call(t, "POST", tp, "", b.admin)
	wantError(t, "telegram 401", code, body, 502, "telegram rejected the test message")
	code, body = b.call(t, "GET", b.botPath(b.org), "", b.viewer)
	if v := decodeBot(t, body); code != 200 || v.Status != "invalid" {
		t.Fatalf("bot after 401 = %d %+v", code, v)
	}
	// With the bot invalid there is no working bot.
	code, body = b.call(t, "POST", tp, "", b.admin)
	wantError(t, "invalid bot", code, body, 409, "connect a Telegram bot first")

	// A linked channel is not testable in tenant mode.
	linked := b.insertLinked(t, b.pid, 5551)
	code, body = b.call(t, "POST", fmt.Sprintf("%s/%d/test", b.chanPath(b.pid), linked), "", b.admin)
	wantError(t, "linked channel", code, body, 404, "channel not found")
}

func TestTestChannelNoBot409(t *testing.T) {
	b := newBotEnv(t, "bot-nobot")
	id := b.insertManual(t, b.pid, "-100123456")
	code, body := b.call(t, "POST", fmt.Sprintf("%s/%d/test", b.chanPath(b.pid), id), "", b.admin)
	wantError(t, "no bot", code, body, 409, "connect a Telegram bot first")
	if n := len(b.api.callsOf("sendMessage")); n != 0 {
		t.Fatalf("sendMessage called %d times", n)
	}
}

func TestCreateChannelCrossProjectIsolation(t *testing.T) {
	b := newBotEnv(t, "bot-iso")
	b.putBot(t)
	req := `{"chat":"-1001234567890","label":"Ops"}`

	// Org A's admin cannot add a channel to org B's project.
	code, body := b.call(t, "POST", b.chanPath(b.pidB), req, b.admin)
	if code != 404 {
		t.Fatalf("POST to org B's project = %d %s, want 404", code, body)
	}
	if code, _ := b.call(t, "POST", b.chanPath(b.pid), req, b.outsider); code != 403 && code != 404 {
		t.Errorf("outsider POST = %d", code)
	}
	if n := b.count(t, `SELECT count(*) FROM alert_channels`); n != 0 {
		t.Fatalf("channels = %d after rejected POSTs", n)
	}

	code, body = b.call(t, "POST", b.chanPath(b.pid), req, b.admin)
	if code != 201 {
		t.Fatalf("POST = %d %s", code, body)
	}
	var ch channelResp
	if err := json.Unmarshal(body, &ch); err != nil {
		t.Fatal(err)
	}
	// The same chat in another project of the org is a separate channel.
	if code, body := b.call(t, "POST", b.chanPath(b.other), req, b.admin); code != 201 {
		t.Fatalf("same chat in other project = %d %s", code, body)
	}
	// The channel is invisible and untestable from the other project.
	code, body = b.call(t, "GET", b.chanPath(b.other), "", b.viewer)
	if code != 200 || strings.Contains(string(body), fmt.Sprintf(`"id":%d,`, ch.ID)) {
		t.Fatalf("other project's list = %d %s", code, body)
	}
	code, body = b.call(t, "POST", fmt.Sprintf("%s/%d/test", b.chanPath(b.other), ch.ID), "", b.admin)
	wantError(t, "cross-project test", code, body, 404, "channel not found")
	code, body = b.call(t, "POST", fmt.Sprintf("%s/%d/test", b.chanPath(b.pidB), ch.ID), "", b.adminB)
	wantError(t, "org B test of org A channel", code, body, 404, "channel not found")
	if n := len(b.api.callsOf("sendMessage")); n != 0 {
		t.Fatalf("sendMessage called %d times for a foreign channel", n)
	}
}
