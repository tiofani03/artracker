package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
)

const localTestPassword = "correct-horse-battery"

// newLocalTestEnv is newTestEnv with local email/password login switched on.
func newLocalTestEnv(t *testing.T) *testEnv {
	t.Helper()
	pool := testDB(t)
	cfg := config.Config{Env: "development", PublicURL: "http://localhost:5173", JWTSecret: testSecret, LocalAuth: true}
	fp := &fakeProvider{profile: Profile{Provider: "google", SSOID: "1001", Email: "ada@example.com", Name: "Ada"}}
	h := New(pool, cfg, auth.NewIssuer(testSecret, 15*time.Minute), map[string]OAuthProvider{"google": fp})
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	app.Get("/auth/callback/:provider", h.OAuthCallback)
	h.Routes(app.Group("/api/v1"))
	return &testEnv{app: app, h: h, pool: pool}
}

// seedLocalUser inserts a password account the way the register flow would and returns its id.
func (e *testEnv) seedLocalUser(t *testing.T, email, password string) int64 {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	err = e.pool.QueryRow(context.Background(), `
		INSERT INTO users (email, name, sso_provider, sso_id, password_hash)
		VALUES ($1, 'Local', 'local', $2, $3) RETURNING id`, email, normalizeEmail(email), hash).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *testEnv) seedGoogleUser(t *testing.T, email string) int64 {
	t.Helper()
	var id int64
	err := e.pool.QueryRow(context.Background(), `
		INSERT INTO users (email, name, sso_provider, sso_id) VALUES ($1, 'Goog', 'google', 'g-'||$1) RETURNING id`, email).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func jsonBody(t *testing.T, v any) io.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReader(string(b))
}

var jsonHeaders = map[string]string{"Content-Type": "application/json"}

func (e *testEnv) localLogin(t *testing.T, email, password string) *http.Response {
	t.Helper()
	return e.do(t, "POST", "/api/v1/auth/local/login", jsonBody(t, map[string]string{"email": email, "password": password}), jsonHeaders)
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestLocalLoginOK(t *testing.T) {
	e := newLocalTestEnv(t)
	e.seedLocalUser(t, "bob@example.com", localTestPassword)

	resp := e.localLogin(t, "bob@example.com", localTestPassword)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, body %s", resp.StatusCode, readBody(t, resp))
	}
	rc := cookieNamed(resp, "refresh_token")
	if rc == nil || !rc.HttpOnly || rc.Value == "" {
		t.Fatalf("refresh_token cookie missing or not httpOnly: %+v", rc)
	}
	if got := e.do(t, "POST", "/api/v1/auth/refresh", nil, nil, rc).StatusCode; got != http.StatusOK {
		t.Fatalf("refresh with the login cookie = %d", got)
	}
}

func TestLocalLoginCaseInsensitive(t *testing.T) {
	e := newLocalTestEnv(t)
	e.seedLocalUser(t, "Bob@Example.com", localTestPassword)
	if got := e.localLogin(t, "  bOb@example.COM ", localTestPassword).StatusCode; got != http.StatusNoContent {
		t.Fatalf("status = %d", got)
	}
}

func TestLocalLoginFailuresAreIndistinguishable(t *testing.T) {
	e := newLocalTestEnv(t)
	e.seedLocalUser(t, "bob@example.com", localTestPassword)
	e.seedGoogleUser(t, "ada@example.com") // Google-only: NULL password_hash

	var bodies []string
	for name, c := range map[string][2]string{
		"wrong password":   {"bob@example.com", "not-the-password"},
		"unknown email":    {"nobody@example.com", localTestPassword},
		"google-only user": {"ada@example.com", localTestPassword},
	} {
		resp := e.localLogin(t, c[0], c[1])
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d", name, resp.StatusCode)
		}
		if cookieNamed(resp, "refresh_token") != nil {
			t.Fatalf("%s: must not set a session", name)
		}
		bodies = append(bodies, readBody(t, resp))
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("failure bodies differ: %q vs %q", bodies[0], b)
		}
	}
	if !strings.Contains(bodies[0], "invalid email or password") {
		t.Fatalf("unexpected message %q", bodies[0])
	}
}

func TestLocalLoginRateLimited(t *testing.T) {
	e := newLocalTestEnv(t)
	e.seedLocalUser(t, "bob@example.com", localTestPassword)
	for i := 1; i <= 10; i++ {
		if got := e.localLogin(t, "bob@example.com", "wrong-password-x").StatusCode; got != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d", i, got)
		}
	}
	if got := e.localLogin(t, "BOB@example.com", localTestPassword).StatusCode; got != http.StatusTooManyRequests {
		t.Fatalf("11th attempt (even with the right password, other casing) = %d, want 429", got)
	}
	// Another email is unaffected.
	e.seedLocalUser(t, "eve@example.com", localTestPassword)
	if got := e.localLogin(t, "eve@example.com", localTestPassword).StatusCode; got != http.StatusNoContent {
		t.Fatalf("other email = %d", got)
	}
}

func TestLocalAuthDisabledHasNoLoginRoute(t *testing.T) {
	e := newTestEnv(t) // LocalAuth off
	got := e.localLogin(t, "bob@example.com", localTestPassword).StatusCode
	if got != http.StatusNotFound && got != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 404/405 (route must not exist)", got)
	}
}
