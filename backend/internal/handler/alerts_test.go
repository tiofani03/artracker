package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tiofani03/artracker/backend/internal/alerts"
)

func TestCreateProjectSeedsAlertRules(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "acme-alerts")
	uid, hdr := e.addUser(t, "alertadmin")
	e.addMember(t, org, uid, "admin")

	resp := e.post(t, fmt.Sprintf("/api/v1/orgs/%d/projects", org), `{"name":"Alerts App","platform":"web"}`, hdr)
	if resp.StatusCode != 201 {
		t.Fatalf("create project = %d", resp.StatusCode)
	}
	var p struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}

	q := fmt.Sprintf(`SELECT count(*) FROM alert_rules WHERE project_id=%d AND enabled AND environment='production'`, p.ID)
	if n := e.count(t, q); n != 3 {
		t.Fatalf("seeded enabled production rules = %d, want 3", n)
	}
}

func TestMigrationColumns(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `SELECT environment FROM issue_events LIMIT 0`); err != nil {
		t.Fatalf("issue_events.environment: %v", err)
	}

	org := e.addOrg(t, "acme-mig")
	pid := e.addProject(t, org, "mig-app")
	e.addIssue(t, pid, "fp-mig", "open")
	var issueID int64
	if err := e.pool.QueryRow(ctx, `SELECT id FROM issues WHERE project_id=$1`, pid).Scan(&issueID); err != nil {
		t.Fatal(err)
	}

	const ins = `INSERT INTO alert_incidents(project_id, rule_kind, issue_id, status, title) VALUES ($1,'new_issue',$2,$3,'t')`
	if _, err := e.pool.Exec(ctx, ins, pid, issueID, "firing"); err != nil {
		t.Fatal(err)
	}
	_, err := e.pool.Exec(ctx, ins, pid, issueID, "firing")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second firing insert err = %v, want unique violation 23505", err)
	}

	if _, err := e.pool.Exec(ctx, `UPDATE alert_incidents SET status='resolved' WHERE project_id=$1`, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, ins, pid, issueID, "firing"); err != nil {
		t.Fatalf("firing after resolved should succeed: %v", err)
	}
}

func TestInsertProjectRollsBackWhenSeedFails(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "acme-atomic")
	boom := errors.New("seed boom")

	_, err := e.h.insertProject(context.Background(), org, "Atomic", "atomic-app", "web",
		func(context.Context, pgx.Tx, int64) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped seed error", err)
	}
	if n := e.count(t, `SELECT count(*) FROM projects WHERE slug='atomic-app'`); n != 0 {
		t.Fatalf("project rows after failed seed = %d, want 0", n)
	}

	// The slug stays free, so a retry succeeds and gets its rules.
	p, err := e.h.insertProject(context.Background(), org, "Atomic", "atomic-app", "web", seedAlertRules)
	if err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, fmt.Sprintf(`SELECT count(*) FROM alert_rules WHERE project_id=%d`, p.ID)); n != 3 {
		t.Fatalf("rules = %d, want 3", n)
	}
}

// fakeNotifier records SendMessage calls and returns err when set.
type fakeNotifier struct {
	mu   sync.Mutex
	sent []string
	err  error
}

func (f *fakeNotifier) SendMessage(_ context.Context, _ int64, html string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, html)
	return f.err
}

func (f *fakeNotifier) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

type alertEnv struct {
	*testEnv
	pid, other    int64
	admin, viewer map[string]string
	base          string
	fn            *fakeNotifier
}

// newAlertEnv builds a project with an admin and a viewer plus a second project in the same org.
func newAlertEnv(t *testing.T, name string, withSvc bool) *alertEnv {
	t.Helper()
	e := newTestEnv(t)
	a := &alertEnv{testEnv: e, fn: &fakeNotifier{}}
	org := e.addOrg(t, name)
	aid, ah := e.addUser(t, name+"admin")
	vid, vh := e.addUser(t, name+"viewer")
	e.addMember(t, org, aid, "admin")
	e.addMember(t, org, vid, "viewer")
	a.admin, a.viewer = ah, vh
	a.pid = e.addProject(t, org, name+"-app")
	a.other = e.addProject(t, org, name+"-other")
	for _, p := range []int64{a.pid, a.other} {
		if _, err := e.pool.Exec(context.Background(), alerts.SeedRulesSQL, p); err != nil {
			t.Fatal(err)
		}
	}
	if withSvc {
		e.h.WithAlerts(alerts.NewService(e.pool, a.fn, "http://app").WithBotUsername("ArtBot"))
	}
	a.base = fmt.Sprintf("/api/v1/projects/%d/alerts", a.pid)
	return a
}

func (a *alertEnv) req(t *testing.T, method, path, body string, hdr map[string]string) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	resp := a.do(t, method, path, r, hdr)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (a *alertEnv) addChannel(t *testing.T, pid, chatID int64, title string) int64 {
	t.Helper()
	var id int64
	err := a.pool.QueryRow(context.Background(),
		`INSERT INTO alert_channels(project_id, telegram_chat_id, title, chat_type) VALUES ($1,$2,$3,'group') RETURNING id`,
		pid, chatID, title).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (a *alertEnv) addIncident(t *testing.T, pid int64, fp, status string, ageSQL string) int64 {
	t.Helper()
	ctx := context.Background()
	var issueID int64
	err := a.pool.QueryRow(ctx,
		`INSERT INTO issues(project_id, fingerprint, title, status) VALUES ($1,$2,$2,'open') RETURNING id`, pid, fp).Scan(&issueID)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	err = a.pool.QueryRow(ctx, `
INSERT INTO alert_incidents(project_id, rule_kind, issue_id, status, title, fired_at, resolved_at, detail)
VALUES ($1,'issue_spike',$2,$3,$4, now() - $5::interval, CASE WHEN $3='resolved' THEN now() END,
        '{"count":42,"baseline":3.5,"window_minutes":60}'::jsonb) RETURNING id`,
		pid, issueID, status, fp, ageSQL).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAlertRulesListAndPatch(t *testing.T) {
	a := newAlertEnv(t, "al-rules", true)
	rules := a.base + "/rules"

	code, body := a.req(t, "GET", rules, "", a.viewer)
	if code != 200 {
		t.Fatalf("GET rules = %d %s", code, body)
	}
	var list struct {
		Items []struct {
			Kind               string  `json:"kind"`
			Enabled            bool    `json:"enabled"`
			Environment        string  `json:"environment"`
			SpikeMultiplier    float64 `json:"spike_multiplier"`
			SpikeMinCount      int     `json:"spike_min_count"`
			SpikeWindowMinutes int     `json:"spike_window_minutes"`
			CooldownMinutes    int     `json:"cooldown_minutes"`
			UpdatedAt          string  `json:"updated_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 3 || list.Items[0].Kind != "new_issue" || list.Items[1].Kind != "regression" || list.Items[2].Kind != "issue_spike" {
		t.Fatalf("rules = %+v", list.Items)
	}
	if strings.Contains(string(body), "project_id") {
		t.Errorf("rule JSON leaks project_id: %s", body)
	}

	if code, _ := a.req(t, "PATCH", rules+"/issue_spike", `{"enabled":false}`, a.viewer); code != 403 {
		t.Errorf("viewer PATCH = %d, want 403", code)
	}

	code, body = a.req(t, "PATCH", rules+"/issue_spike", `{"spike_min_count":50,"spike_window_minutes":30}`, a.admin)
	if code != 200 {
		t.Fatalf("admin PATCH = %d %s", code, body)
	}
	var r struct {
		Kind               string  `json:"kind"`
		Enabled            bool    `json:"enabled"`
		Environment        string  `json:"environment"`
		SpikeMultiplier    float64 `json:"spike_multiplier"`
		SpikeMinCount      int     `json:"spike_min_count"`
		SpikeWindowMinutes int     `json:"spike_window_minutes"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	if r.SpikeMinCount != 50 || r.SpikeWindowMinutes != 30 || r.SpikeMultiplier != 5 || !r.Enabled || r.Environment != "production" {
		t.Errorf("patched rule = %+v (omitted fields must be unchanged)", r)
	}
	q := fmt.Sprintf(`SELECT count(*) FROM alert_rules WHERE project_id=%d AND kind='issue_spike' AND spike_min_count=50 AND spike_window_minutes=30`, a.pid)
	if n := a.count(t, q); n != 1 {
		t.Errorf("values not persisted")
	}
	if n := a.count(t, fmt.Sprintf(`SELECT count(*) FROM alert_rules WHERE project_id=%d AND spike_min_count=50`, a.other)); n != 0 {
		t.Errorf("other project's rule was modified")
	}

	// Explicit false and empty string are applied.
	code, body = a.req(t, "PATCH", rules+"/new_issue", `{"enabled":false,"environment":""}`, a.admin)
	if code != 200 {
		t.Fatalf("PATCH new_issue = %d %s", code, body)
	}
	q = fmt.Sprintf(`SELECT count(*) FROM alert_rules WHERE project_id=%d AND kind='new_issue' AND NOT enabled AND environment=''`, a.pid)
	if n := a.count(t, q); n != 1 {
		t.Errorf("explicit false/empty not applied")
	}
	// updated_at is bumped.
	if n := a.count(t, fmt.Sprintf(`SELECT count(*) FROM alert_rules WHERE project_id=%d AND kind='new_issue' AND updated_at > now() - interval '1 minute'`, a.pid)); n != 1 {
		t.Errorf("updated_at not bumped")
	}

	bad := []struct {
		kind, body string
		want       int
	}{
		{"issue_spike", `{"spike_window_minutes":45}`, 400},
		{"issue_spike", `{"spike_multiplier":1.4}`, 400},
		{"issue_spike", `{"spike_multiplier":101}`, 400},
		{"issue_spike", `{"spike_min_count":0}`, 400},
		{"issue_spike", `{"spike_min_count":100001}`, 400},
		{"issue_spike", `{"cooldown_minutes":-1}`, 400},
		{"issue_spike", `{"cooldown_minutes":1441}`, 400},
		{"issue_spike", `{"environment":"` + strings.Repeat("x", 65) + `"}`, 400},
		{"new_issue", `{"spike_multiplier":3}`, 400},
		{"regression", `{"cooldown_minutes":5}`, 400},
		{"regression", `{"spike_min_count":5}`, 400},
		{"issue_spike", `{}`, 400},
		{"issue_spike", `{"bogus":1}`, 400},
		{"issue_spike", `not json`, 400},
		{"nope", `{"enabled":true}`, 404},
	}
	for _, c := range bad {
		if code, body := a.req(t, "PATCH", rules+"/"+c.kind, c.body, a.admin); code != c.want {
			t.Errorf("PATCH %s %s = %d %s, want %d", c.kind, c.body, code, body, c.want)
		}
	}
	// Valid boundaries.
	if code, body := a.req(t, "PATCH", rules+"/issue_spike", `{"spike_multiplier":1.5,"cooldown_minutes":0,"spike_window_minutes":180,"spike_min_count":100000}`, a.admin); code != 200 {
		t.Errorf("boundary PATCH = %d %s", code, body)
	}
}

func TestAlertChannelsHideChatID(t *testing.T) {
	a := newAlertEnv(t, "al-chan", true)
	a.addChannel(t, a.pid, 987654321012, "Ops room")
	a.addChannel(t, a.pid, 987654321013, "Second")

	code, body := a.req(t, "GET", a.base+"/channels", "", a.viewer)
	if code != 200 {
		t.Fatalf("GET channels = %d %s", code, body)
	}
	if strings.Contains(string(body), "987654321012") || strings.Contains(string(body), "chat_id") {
		t.Fatalf("response leaks chat id: %s", body)
	}
	var out struct {
		Telegram struct {
			Configured  bool   `json:"configured"`
			BotUsername string `json:"bot_username"`
		} `json:"telegram"`
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Telegram.Configured || out.Telegram.BotUsername != "ArtBot" {
		t.Errorf("telegram = %+v", out.Telegram)
	}
	if len(out.Items) != 2 || out.Items[0]["title"] != "Ops room" || out.Items[1]["title"] != "Second" {
		t.Fatalf("items = %v", out.Items)
	}
	for _, k := range []string{"id", "kind", "title", "chat_type", "enabled", "disabled_reason", "last_sent_at", "last_error", "created_at"} {
		if _, ok := out.Items[0][k]; !ok {
			t.Errorf("channel JSON missing %q", k)
		}
	}
	if out.Items[0]["last_sent_at"] != nil {
		t.Errorf("last_sent_at = %v, want null", out.Items[0]["last_sent_at"])
	}
}

func TestAlertChannelsWithoutService(t *testing.T) {
	a := newAlertEnv(t, "al-nosvc", false)
	a.addChannel(t, a.pid, 5001, "Room")
	code, body := a.req(t, "GET", a.base+"/channels", "", a.viewer)
	if code != 200 {
		t.Fatalf("GET channels = %d %s", code, body)
	}
	var out struct {
		Telegram struct {
			Configured  bool   `json:"configured"`
			BotUsername string `json:"bot_username"`
		} `json:"telegram"`
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Telegram.Configured || out.Telegram.BotUsername != "" || len(out.Items) != 1 {
		t.Errorf("out = %+v", out)
	}
	id := a.addChannel(t, a.pid, 5002, "Room2")
	code, body = a.req(t, "POST", fmt.Sprintf("%s/channels/%d/test", a.base, id), "", a.admin)
	if code != 503 {
		t.Errorf("test without service = %d %s, want 503", code, body)
	}
}

func TestLinkCodeAdminOnly(t *testing.T) {
	a := newAlertEnv(t, "al-link", true)
	path := a.base + "/channels/link-code"
	if code, _ := a.req(t, "POST", path, "", a.viewer); code != 403 {
		t.Errorf("viewer = %d, want 403", code)
	}
	code, body := a.req(t, "POST", path, "", a.admin)
	if code != 201 {
		t.Fatalf("admin = %d %s", code, body)
	}
	var lc alerts.LinkCode
	if err := json.Unmarshal(body, &lc); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(lc.GroupLink, "https://t.me/ArtBot?startgroup=") || lc.Code == "" {
		t.Errorf("link code = %+v", lc)
	}

	b := newAlertEnv(t, "al-link2", false)
	code, body = b.req(t, "POST", b.base+"/channels/link-code", "", b.admin)
	if code != 503 || !strings.Contains(string(body), "telegram is not configured on this server") {
		t.Errorf("without service = %d %s", code, body)
	}
}

func TestChannelCrossProjectIsolation(t *testing.T) {
	a := newAlertEnv(t, "al-iso", true)
	foreign := a.addChannel(t, a.other, 7001, "Foreign")
	p := fmt.Sprintf("%s/channels/%d", a.base, foreign)

	if code, body := a.req(t, "PATCH", p, `{"enabled":false}`, a.admin); code != 404 {
		t.Errorf("PATCH = %d %s", code, body)
	}
	if code, body := a.req(t, "DELETE", p, "", a.admin); code != 404 {
		t.Errorf("DELETE = %d %s", code, body)
	}
	if code, body := a.req(t, "POST", p+"/test", "", a.admin); code != 404 {
		t.Errorf("test = %d %s", code, body)
	}
	if n := a.count(t, fmt.Sprintf(`SELECT count(*) FROM alert_channels WHERE id=%d AND enabled`, foreign)); n != 1 {
		t.Errorf("foreign channel was modified or deleted")
	}
	if a.fn.sentCount() != 0 {
		t.Errorf("a message was sent for a foreign channel")
	}
}

func TestChannelPatchDeleteAndBadIDs(t *testing.T) {
	a := newAlertEnv(t, "al-crud", true)
	id := a.addChannel(t, a.pid, 8001, "Room")
	if _, err := a.pool.Exec(context.Background(),
		`UPDATE alert_channels SET enabled=false, disabled_reason='bot removed' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	p := fmt.Sprintf("%s/channels/%d", a.base, id)

	if code, _ := a.req(t, "PATCH", p, `{"enabled":true}`, a.viewer); code != 403 {
		t.Errorf("viewer PATCH = %d", code)
	}
	if code, body := a.req(t, "PATCH", p, `{}`, a.admin); code != 400 {
		t.Errorf("empty PATCH = %d %s", code, body)
	}
	code, body := a.req(t, "PATCH", p, `{"enabled":true}`, a.admin)
	if code != 200 {
		t.Fatalf("PATCH = %d %s", code, body)
	}
	var ch map[string]any
	if err := json.Unmarshal(body, &ch); err != nil {
		t.Fatal(err)
	}
	if ch["enabled"] != true || ch["disabled_reason"] != "" || strings.Contains(string(body), "8001") {
		t.Errorf("channel = %v", ch)
	}
	if code, _ := a.req(t, "PATCH", p, `{"enabled":false}`, a.admin); code != 200 {
		t.Errorf("disable = %d", code)
	}

	for _, bad := range []string{"abc", "0", "-3", "99999999999999999999"} {
		bp := a.base + "/channels/" + bad
		if code, _ := a.req(t, "PATCH", bp, `{"enabled":true}`, a.admin); code != 404 {
			t.Errorf("PATCH %s = %d, want 404", bad, code)
		}
		if code, _ := a.req(t, "DELETE", bp, "", a.admin); code != 404 {
			t.Errorf("DELETE %s = %d, want 404", bad, code)
		}
		if code, _ := a.req(t, "POST", bp+"/test", "", a.admin); code != 404 {
			t.Errorf("test %s = %d, want 404", bad, code)
		}
	}

	if code, _ := a.req(t, "DELETE", p, "", a.viewer); code != 403 {
		t.Errorf("viewer DELETE = %d", code)
	}
	if code, _ := a.req(t, "DELETE", p, "", a.admin); code != 204 {
		t.Errorf("DELETE = %d, want 204", code)
	}
	if code, _ := a.req(t, "DELETE", p, "", a.admin); code != 404 {
		t.Errorf("second DELETE = %d, want 404", code)
	}
}

func TestChannelSendTest(t *testing.T) {
	a := newAlertEnv(t, "al-test", true)
	id := a.addChannel(t, a.pid, 9001, "Room")
	p := fmt.Sprintf("%s/channels/%d/test", a.base, id)

	if code, _ := a.req(t, "POST", p, "", a.viewer); code != 403 {
		t.Errorf("viewer test = %d", code)
	}
	if code, body := a.req(t, "POST", p, "", a.admin); code != 204 {
		t.Fatalf("test = %d %s", code, body)
	}
	if a.fn.sentCount() != 1 {
		t.Errorf("sent = %d, want 1", a.fn.sentCount())
	}

	a.fn.mu.Lock()
	a.fn.err = errors.New("Forbidden: secret telegram text 123:ABC")
	a.fn.mu.Unlock()
	code, body := a.req(t, "POST", p, "", a.admin)
	if code != 502 {
		t.Fatalf("failing test = %d %s, want 502", code, body)
	}
	if !strings.Contains(string(body), "telegram rejected the test message") || strings.Contains(string(body), "secret") {
		t.Errorf("body = %s", body)
	}
}

func TestIncidentsFilter(t *testing.T) {
	a := newAlertEnv(t, "al-inc", true)
	oldest := a.addIncident(t, a.pid, "inc-a", "firing", "3 hours")
	mid := a.addIncident(t, a.pid, "inc-b", "resolved", "2 hours")
	newest := a.addIncident(t, a.pid, "inc-c", "firing", "1 hour")
	a.addIncident(t, a.other, "inc-d", "firing", "1 minute")

	type inc struct {
		ID         int64   `json:"id"`
		RuleKind   string  `json:"rule_kind"`
		Status     string  `json:"status"`
		ResolvedAt *string `json:"resolved_at"`
		Spike      *struct {
			Count         int     `json:"count"`
			Baseline      float64 `json:"baseline"`
			WindowMinutes int     `json:"window_minutes"`
		} `json:"spike"`
	}
	get := func(q string, hdr map[string]string) (int, []inc) {
		code, body := a.req(t, "GET", a.base+"/incidents"+q, "", hdr)
		var out struct {
			Items []inc `json:"items"`
		}
		_ = json.Unmarshal(body, &out)
		return code, out.Items
	}

	code, items := get("", a.viewer)
	if code != 200 || len(items) != 3 {
		t.Fatalf("default = %d, %d items", code, len(items))
	}
	if items[0].ID != newest || items[1].ID != mid || items[2].ID != oldest {
		t.Errorf("order = %d,%d,%d, want newest first", items[0].ID, items[1].ID, items[2].ID)
	}
	if items[0].ResolvedAt != nil || items[1].ResolvedAt == nil {
		t.Errorf("resolved_at: firing=%v resolved=%v", items[0].ResolvedAt, items[1].ResolvedAt)
	}
	if items[0].Spike == nil || items[0].Spike.Count != 42 || items[0].Spike.WindowMinutes != 60 {
		t.Errorf("spike = %+v", items[0].Spike)
	}
	if _, f := get("?status=firing", a.viewer); len(f) != 2 {
		t.Errorf("firing = %d, want 2", len(f))
	}
	if _, r := get("?status=resolved", a.viewer); len(r) != 1 || r[0].Status != "resolved" {
		t.Errorf("resolved = %+v", r)
	}
	if _, all := get("?status=all", a.viewer); len(all) != 3 {
		t.Errorf("all = %d", len(all))
	}
	if code, _ := get("?status=bogus", a.viewer); code != 400 {
		t.Errorf("bogus status = %d, want 400", code)
	}
	if _, l := get("?limit=1", a.viewer); len(l) != 1 || l[0].ID != newest {
		t.Errorf("limit=1 = %+v", l)
	}
	if _, l := get("?limit=0", a.viewer); len(l) != 1 {
		t.Errorf("limit=0 clamps to 1, got %d", len(l))
	}
	if _, l := get("?limit=-5", a.viewer); len(l) != 1 {
		t.Errorf("limit=-5 clamps to 1, got %d", len(l))
	}
	if _, l := get("?limit=9999", a.viewer); len(l) != 3 {
		t.Errorf("limit=9999 = %d", len(l))
	}
	if _, l := get("?limit=abc", a.viewer); len(l) != 3 {
		t.Errorf("limit=abc falls back to default, got %d", len(l))
	}

	// A project without alerts still answers with an empty list, not null.
	_, body := a.req(t, "GET", fmt.Sprintf("/api/v1/projects/%d/alerts/incidents?status=resolved", a.other), "", a.viewer)
	if !strings.Contains(string(body), `"items":[]`) {
		t.Errorf("empty list body = %s", body)
	}
}

func TestAlertWritesForbiddenForViewer(t *testing.T) {
	a := newAlertEnv(t, "al-forbid", true)
	id := a.addChannel(t, a.pid, 6001, "Room")
	ch := fmt.Sprintf("%s/channels/%d", a.base, id)
	for _, c := range []struct{ method, path, body string }{
		{"PATCH", a.base + "/rules/new_issue", `{"enabled":false}`},
		{"POST", a.base + "/channels/link-code", ""},
		{"PATCH", ch, `{"enabled":false}`},
		{"DELETE", ch, ""},
		{"POST", ch + "/test", ""},
	} {
		if code, _ := a.req(t, c.method, c.path, c.body, a.viewer); code != 403 {
			t.Errorf("viewer %s %s = %d, want 403", c.method, c.path, code)
		}
	}
	_, outsider := a.addUser(t, "al-forbid-out")
	for _, p := range []string{"/rules", "/channels", "/incidents"} {
		if code, _ := a.req(t, "GET", a.base+p, "", outsider); code != 403 && code != 404 {
			t.Errorf("outsider GET %s = %d", p, code)
		}
	}
}
