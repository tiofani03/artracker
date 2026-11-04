package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
)

const testSecret = "test-secret"

type fakeProvider struct{ profile Profile }

func (f *fakeProvider) AuthURL(state, redirectURL string) string {
	return "https://idp.example/auth?state=" + url.QueryEscape(state) + "&redirect_uri=" + url.QueryEscape(redirectURL)
}

func (f *fakeProvider) Exchange(_ context.Context, code, _ string) (Profile, error) {
	if code == "bad" {
		return Profile{}, errors.New("exchange failed")
	}
	return f.profile, nil
}

type testEnv struct {
	app  *fiber.App
	h    *Handler
	pool *pgxpool.Pool
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	pool := testDB(t)
	cfg := config.Config{Env: "development", PublicURL: "http://localhost:5173", JWTSecret: testSecret}
	fp := &fakeProvider{profile: Profile{Provider: "google", SSOID: "1001", Email: "ada@example.com", Name: "Ada", AvatarURL: "http://a/b.png"}}
	h := New(pool, cfg, auth.NewIssuer(testSecret, 15*time.Minute), map[string]OAuthProvider{"google": fp})
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	app.Get("/auth/callback/:provider", h.OAuthCallback)
	h.Routes(app.Group("/api/v1"))
	return &testEnv{app: app, h: h, pool: pool}
}

func (e *testEnv) do(t *testing.T, method, path string, body io.Reader, headers map[string]string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func cookieNamed(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// login runs login -> callback and returns the refresh cookie.
func (e *testEnv) login(t *testing.T) *http.Cookie {
	t.Helper()
	resp := e.do(t, "GET", "/api/v1/auth/google/login", nil, nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	state := loc.Query().Get("state")
	stateCookie := cookieNamed(resp, "oauth_state")
	if state == "" || stateCookie == nil {
		t.Fatal("login must set state param and oauth_state cookie")
	}
	cb := e.do(t, "GET", "/api/v1/auth/google/callback?code=ok&state="+url.QueryEscape(state), nil, nil, stateCookie)
	if cb.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d", cb.StatusCode)
	}
	if got := cb.Header.Get("Location"); got != "http://localhost:5173/dashboard" {
		t.Fatalf("callback redirect = %q", got)
	}
	rc := cookieNamed(cb, "refresh_token")
	if rc == nil || !rc.HttpOnly || rc.Value == "" {
		t.Fatalf("refresh_token cookie missing or not httpOnly: %+v", rc)
	}
	return rc
}

func (e *testEnv) count(t *testing.T, q string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *testEnv) accessToken(t *testing.T, refresh *http.Cookie) string {
	t.Helper()
	resp := e.do(t, "POST", "/api/v1/auth/refresh", nil, nil, refresh)
	if resp.StatusCode != 200 {
		t.Fatalf("refresh status = %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.AccessToken
}

func TestCallbackCreatesUserAndSetsRefreshCookie(t *testing.T) {
	e := newTestEnv(t)
	e.login(t)
	if n := e.count(t, `SELECT count(*) FROM users WHERE email='ada@example.com' AND sso_provider='google' AND sso_id='1001'`); n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
}

func TestCallbackFailuresRedirectToLoginWithoutCreatingUser(t *testing.T) {
	e := newTestEnv(t)
	resp := e.do(t, "GET", "/api/v1/auth/google/login", nil, nil)
	stateCookie := cookieNamed(resp, "oauth_state")
	loc, _ := url.Parse(resp.Header.Get("Location"))
	state := loc.Query().Get("state")

	cases := []struct {
		name, path string
		cookie     *http.Cookie
		wantErr    string
	}{
		{"state mismatch", "/api/v1/auth/google/callback?code=ok&state=forged", stateCookie, "invalid_state"},
		{"missing state cookie", "/api/v1/auth/google/callback?code=ok&state=forged", nil, "invalid_state"},
		{"user cancelled consent", "/api/v1/auth/google/callback?error=access_denied&state=" + url.QueryEscape(state), stateCookie, "access_denied"},
		{"provider exchange fails", "/api/v1/auth/google/callback?code=bad&state=" + url.QueryEscape(state), stateCookie, "provider_failed"},
	}
	for _, c := range cases {
		var r *http.Response
		if c.cookie != nil {
			r = e.do(t, "GET", c.path, nil, nil, c.cookie)
		} else {
			r = e.do(t, "GET", c.path, nil, nil)
		}
		want := "http://localhost:5173/login?error=" + c.wantErr
		if r.StatusCode != http.StatusFound || r.Header.Get("Location") != want {
			t.Errorf("%s: status=%d location=%q, want 302 %q", c.name, r.StatusCode, r.Header.Get("Location"), want)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM users`); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
}

func TestCallbackSecondLoginReusesUser(t *testing.T) {
	e := newTestEnv(t)
	e.login(t)
	e.login(t)
	if n := e.count(t, `SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
}

func TestUnknownProviderLogin404(t *testing.T) {
	e := newTestEnv(t)
	if resp := e.do(t, "GET", "/api/v1/auth/gitlab/login", nil, nil); resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRefreshRotatesToken(t *testing.T) {
	e := newTestEnv(t)
	c1 := e.login(t)
	resp := e.do(t, "POST", "/api/v1/auth/refresh", nil, nil, c1)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   string `json:"expires_at"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if _, err := e.h.issuer.VerifyAccess(out.AccessToken); err != nil || out.ExpiresAt == "" {
		t.Fatalf("access token invalid: %v (%+v)", err, out)
	}
	c2 := cookieNamed(resp, "refresh_token")
	if c2 == nil || c2.Value == c1.Value {
		t.Fatal("refresh token must be rotated")
	}
}

func TestRefreshReuseRevokesAll(t *testing.T) {
	e := newTestEnv(t)
	c1 := e.login(t)
	c2 := cookieNamed(e.do(t, "POST", "/api/v1/auth/refresh", nil, nil, c1), "refresh_token")
	// Reuse well after rotation (outside the concurrency grace window) is treated as theft.
	if _, err := e.pool.Exec(context.Background(), `UPDATE refresh_tokens SET revoked_at = now() - interval '1 minute' WHERE revoked_at IS NOT NULL`); err != nil {
		t.Fatal(err)
	}

	if resp := e.do(t, "POST", "/api/v1/auth/refresh", nil, nil, c1); resp.StatusCode != 401 {
		t.Fatalf("reused token status = %d, want 401", resp.StatusCode)
	}
	if resp := e.do(t, "POST", "/api/v1/auth/refresh", nil, nil, c2); resp.StatusCode != 401 {
		t.Fatalf("descendant token status = %d, want 401 after reuse", resp.StatusCode)
	}
}

// Two tabs refreshing with the same cookie at once must not end the session:
// the loser gets 401 but the winner's new token stays valid.
func TestRefreshConcurrentReuseWithinGraceKeepsSession(t *testing.T) {
	e := newTestEnv(t)
	c1 := e.login(t)
	winner := e.do(t, "POST", "/api/v1/auth/refresh", nil, nil, c1)
	c2 := cookieNamed(winner, "refresh_token")
	if winner.StatusCode != 200 || c2 == nil {
		t.Fatalf("winner = %d", winner.StatusCode)
	}
	if loser := e.do(t, "POST", "/api/v1/auth/refresh", nil, nil, c1); loser.StatusCode != 401 {
		t.Fatalf("loser = %d, want 401", loser.StatusCode)
	}
	if n := e.count(t, `SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NULL`); n != 1 {
		t.Fatalf("active tokens = %d, want 1 (winner's token must survive)", n)
	}
	if again := e.do(t, "POST", "/api/v1/auth/refresh", nil, nil, c2); again.StatusCode != 200 {
		t.Fatalf("winner's token = %d, want 200", again.StatusCode)
	}
}

func TestRefreshRejectsExpiredAndMissing(t *testing.T) {
	e := newTestEnv(t)
	c := e.login(t)
	if _, err := e.pool.Exec(context.Background(), `UPDATE refresh_tokens SET expires_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if resp := e.do(t, "POST", "/api/v1/auth/refresh", nil, nil, c); resp.StatusCode != 401 {
		t.Errorf("expired status = %d, want 401", resp.StatusCode)
	}
	if resp := e.do(t, "POST", "/api/v1/auth/refresh", nil, nil); resp.StatusCode != 401 {
		t.Errorf("missing cookie status = %d, want 401", resp.StatusCode)
	}
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	e := newTestEnv(t)
	c := e.login(t)
	if resp := e.do(t, "POST", "/api/v1/auth/logout", nil, nil, c); resp.StatusCode != 204 {
		t.Fatalf("logout status = %d", resp.StatusCode)
	}
	if resp := e.do(t, "POST", "/api/v1/auth/refresh", nil, nil, c); resp.StatusCode != 401 {
		t.Fatalf("refresh after logout = %d, want 401", resp.StatusCode)
	}
}

func TestMeRequiresAuth(t *testing.T) {
	e := newTestEnv(t)
	c := e.login(t)

	if resp := e.do(t, "GET", "/api/v1/auth/me", nil, nil); resp.StatusCode != 401 {
		t.Errorf("no header = %d, want 401", resp.StatusCode)
	}
	expired, _, _ := auth.NewIssuer(testSecret, -time.Minute).IssueAccess(1)
	if resp := e.do(t, "GET", "/api/v1/auth/me", nil, map[string]string{"Authorization": "Bearer " + expired}); resp.StatusCode != 401 {
		t.Errorf("expired token = %d, want 401", resp.StatusCode)
	}

	var userID int64
	_ = e.pool.QueryRow(context.Background(), `SELECT id FROM users`).Scan(&userID)
	var orgID int64
	_ = e.pool.QueryRow(context.Background(), `INSERT INTO organizations(name, slug) VALUES ('Acme','acme') RETURNING id`).Scan(&orgID)
	_, _ = e.pool.Exec(context.Background(), `INSERT INTO organization_members(org_id,user_id,role) VALUES ($1,$2,'owner')`, orgID, userID)

	tok := e.accessToken(t, c)
	resp := e.do(t, "GET", "/api/v1/auth/me", nil, map[string]string{"Authorization": "Bearer " + tok})
	if resp.StatusCode != 200 {
		t.Fatalf("valid token = %d, want 200", resp.StatusCode)
	}
	var me struct {
		Email string `json:"email"`
		Orgs  []struct {
			Slug string `json:"slug"`
			Role string `json:"role"`
		} `json:"orgs"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&me)
	if me.Email != "ada@example.com" || len(me.Orgs) != 1 || me.Orgs[0].Role != "owner" {
		t.Fatalf("me = %+v", me)
	}
}
