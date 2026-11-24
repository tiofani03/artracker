package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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

func (e *testEnv) seedOrg(t *testing.T, ownerID int64, slug string) int64 {
	t.Helper()
	var orgID int64
	err := e.pool.QueryRow(context.Background(),
		`INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id`,
		"Org "+slug, slug).Scan(&orgID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`,
		orgID, ownerID); err != nil {
		t.Fatal(err)
	}
	return orgID
}

func (e *testEnv) seedInvite(t *testing.T, orgID, invitedBy int64, email, role string, expiresAt time.Time, accepted bool) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	raw := hex.EncodeToString(b)
	var acceptedAt *time.Time
	var acceptedBy *int64
	if accepted {
		now := time.Now().Add(-time.Minute)
		acceptedAt = &now
		acceptedBy = &invitedBy
	}
	_, err := e.pool.Exec(context.Background(), `
		INSERT INTO organization_invitations (org_id, email, role, token_hash, invited_by, expires_at, accepted_at, accepted_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		orgID, email, role, hashInviteToken(raw), invitedBy, expiresAt, acceptedAt, acceptedBy)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (e *testEnv) localRegister(t *testing.T, token, name, password string) *http.Response {
	t.Helper()
	return e.do(t, "POST", "/api/v1/auth/local/register",
		jsonBody(t, map[string]string{"invite_token": token, "name": name, "password": password}),
		jsonHeaders)
}

func TestRegisterWithInviteCreatesUserMembershipAndSession(t *testing.T) {
	e := newLocalTestEnv(t)
	owner := e.seedLocalUser(t, "owner@example.com", localTestPassword)
	orgID := e.seedOrg(t, owner, "acme")
	tok := e.seedInvite(t, orgID, owner, "newbie@example.com", "member", time.Now().Add(time.Hour), false)

	resp := e.localRegister(t, tok, "Newbie", localTestPassword)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body %s", resp.StatusCode, readBody(t, resp))
	}
	rc := cookieNamed(resp, "refresh_token")
	if rc == nil || !rc.HttpOnly || rc.Value == "" {
		t.Fatalf("refresh_token cookie missing: %+v", rc)
	}
	if got := e.count(t, `SELECT count(*) FROM users WHERE lower(email)='newbie@example.com' AND sso_provider='local'`); got != 1 {
		t.Fatalf("users count = %d, want 1", got)
	}
	var memberCount int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM organization_members m JOIN users u ON u.id=m.user_id WHERE m.org_id=$1 AND u.email='newbie@example.com' AND m.role='member'`,
		orgID).Scan(&memberCount); err != nil || memberCount != 1 {
		t.Fatalf("member count = %d (err=%v), want 1", memberCount, err)
	}
	var accepted int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM organization_invitations WHERE token_hash=$1 AND accepted_at IS NOT NULL`,
		hashInviteToken(tok)).Scan(&accepted); err != nil || accepted != 1 {
		t.Fatalf("invite not marked accepted (count=%d, err=%v)", accepted, err)
	}
	if got := e.localLogin(t, "newbie@example.com", localTestPassword).StatusCode; got != http.StatusNoContent {
		t.Fatalf("login after register = %d, want 204", got)
	}
}

func TestRegisterRejectsAcceptedInvite(t *testing.T) {
	e := newLocalTestEnv(t)
	owner := e.seedLocalUser(t, "owner@example.com", localTestPassword)
	orgID := e.seedOrg(t, owner, "acme")
	tok := e.seedInvite(t, orgID, owner, "used@example.com", "member", time.Now().Add(time.Hour), true)

	if got := e.localRegister(t, tok, "Used", localTestPassword).StatusCode; got != http.StatusConflict {
		t.Fatalf("status = %d, want 409", got)
	}
	if got := e.count(t, `SELECT count(*) FROM users WHERE lower(email)='used@example.com'`); got != 0 {
		t.Fatalf("must not create user, got %d", got)
	}
}

func TestRegisterRejectsExpiredInvite(t *testing.T) {
	e := newLocalTestEnv(t)
	owner := e.seedLocalUser(t, "owner@example.com", localTestPassword)
	orgID := e.seedOrg(t, owner, "acme")
	tok := e.seedInvite(t, orgID, owner, "late@example.com", "member", time.Now().Add(-time.Minute), false)

	if got := e.localRegister(t, tok, "Late", localTestPassword).StatusCode; got != http.StatusGone {
		t.Fatalf("status = %d, want 410", got)
	}
	if got := e.count(t, `SELECT count(*) FROM users WHERE lower(email)='late@example.com'`); got != 0 {
		t.Fatalf("must not create user, got %d", got)
	}
}

func TestRegisterUnknownToken(t *testing.T) {
	e := newLocalTestEnv(t)
	if got := e.localRegister(t, "nonexistent-token-123456", "No One", localTestPassword).StatusCode; got != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", got)
	}
}

func TestRegisterWeakPassword(t *testing.T) {
	e := newLocalTestEnv(t)
	owner := e.seedLocalUser(t, "owner@example.com", localTestPassword)
	orgID := e.seedOrg(t, owner, "acme")
	tok := e.seedInvite(t, orgID, owner, "weak@example.com", "member", time.Now().Add(time.Hour), false)

	for _, pw := range []string{"short", strings.Repeat("a", 73)} {
		if got := e.localRegister(t, tok, "Weak", pw).StatusCode; got != http.StatusBadRequest {
			t.Fatalf("pw len=%d: status = %d, want 400", len(pw), got)
		}
	}
	if got := e.count(t, `SELECT count(*) FROM users WHERE lower(email)='weak@example.com'`); got != 0 {
		t.Fatalf("must not create user, got %d", got)
	}
	if got := e.count(t, `SELECT count(*) FROM organization_invitations WHERE accepted_at IS NULL`); got != 1 {
		t.Fatalf("invite must stay unaccepted, got %d", got)
	}
}

func TestRegisterDuplicateLocalEmail(t *testing.T) {
	e := newLocalTestEnv(t)
	owner := e.seedLocalUser(t, "owner@example.com", localTestPassword)
	orgID := e.seedOrg(t, owner, "acme")
	e.seedLocalUser(t, "dup@example.com", localTestPassword)
	tok := e.seedInvite(t, orgID, owner, "dup@example.com", "member", time.Now().Add(time.Hour), false)

	if got := e.localRegister(t, tok, "Dup", localTestPassword).StatusCode; got != http.StatusConflict {
		t.Fatalf("status = %d, want 409", got)
	}
	var accepted int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM organization_invitations WHERE token_hash=$1 AND accepted_at IS NULL`,
		hashInviteToken(tok)).Scan(&accepted); err != nil || accepted != 1 {
		t.Fatalf("invite must stay unconsumed (count=%d, err=%v)", accepted, err)
	}
}

func TestRegisterMixedCaseEmail(t *testing.T) {
	e := newLocalTestEnv(t)
	owner := e.seedLocalUser(t, "owner@example.com", localTestPassword)
	orgID := e.seedOrg(t, owner, "acme")
	tok1 := e.seedInvite(t, orgID, owner, "Ada@X.com", "member", time.Now().Add(time.Hour), false)
	tok2 := e.seedInvite(t, orgID, owner, "ADA@x.com", "member", time.Now().Add(time.Hour), false)

	if got := e.localRegister(t, tok1, "Ada", localTestPassword).StatusCode; got != http.StatusCreated {
		t.Fatalf("first register = %d, want 201", got)
	}
	if got := e.localLogin(t, "ada@x.com", localTestPassword).StatusCode; got != http.StatusNoContent {
		t.Fatalf("lowercase login = %d, want 204", got)
	}
	if got := e.localRegister(t, tok2, "Ada2", localTestPassword).StatusCode; got != http.StatusConflict {
		t.Fatalf("second register with different case = %d, want 409", got)
	}
}

func TestRegisterAdoptsExistingGoogleUser(t *testing.T) {
	e := newLocalTestEnv(t)
	owner := e.seedLocalUser(t, "owner@example.com", localTestPassword)
	orgID := e.seedOrg(t, owner, "acme")

	// Seed the Google user that matches fakeProvider ("ada@example.com", sso_id "1001").
	var googleUID int64
	err := e.pool.QueryRow(context.Background(), `
		INSERT INTO users (email, name, sso_provider, sso_id)
		VALUES ('ada@example.com', 'Ada Google', 'google', '1001') RETURNING id`).Scan(&googleUID)
	if err != nil {
		t.Fatal(err)
	}
	beforeCount := e.count(t, `SELECT count(*) FROM users`)

	tok := e.seedInvite(t, orgID, owner, "Ada@Example.com", "admin", time.Now().Add(time.Hour), false)
	resp := e.localRegister(t, tok, "Ignored Name", localTestPassword)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register = %d, body %s", resp.StatusCode, readBody(t, resp))
	}
	if after := e.count(t, `SELECT count(*) FROM users`); after != beforeCount {
		t.Fatalf("users count changed %d -> %d (must adopt existing row)", beforeCount, after)
	}

	var provider string
	var hasHash bool
	if err := e.pool.QueryRow(context.Background(),
		`SELECT sso_provider, password_hash IS NOT NULL FROM users WHERE id=$1`, googleUID).
		Scan(&provider, &hasHash); err != nil {
		t.Fatal(err)
	}
	if provider != "google" || !hasHash {
		t.Fatalf("adopted row = (provider=%q, hasHash=%v), want ('google', true)", provider, hasHash)
	}

	// Password login resolves to the same user ID.
	loginResp := e.localLogin(t, "ada@example.com", localTestPassword)
	if loginResp.StatusCode != http.StatusNoContent {
		t.Fatalf("password login = %d", loginResp.StatusCode)
	}
	acc := e.accessToken(t, cookieNamed(loginResp, "refresh_token"))
	meResp := e.do(t, "GET", "/api/v1/auth/me", nil, map[string]string{"Authorization": "Bearer " + acc})
	var me struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(meResp.Body).Decode(&me)
	if me.ID != googleUID {
		t.Fatalf("password /auth/me id = %d, want %d", me.ID, googleUID)
	}

	// Google OAuth callback still resolves to the exact same user ID.
	googleRC := e.login(t)
	googleAcc := e.accessToken(t, googleRC)
	gMeResp := e.do(t, "GET", "/api/v1/auth/me", nil, map[string]string{"Authorization": "Bearer " + googleAcc})
	var gMe struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(gMeResp.Body).Decode(&gMe)
	if gMe.ID != googleUID {
		t.Fatalf("google /auth/me id = %d, want %d", gMe.ID, googleUID)
	}
}

func TestRegisterRateLimited(t *testing.T) {
	e := newLocalTestEnv(t)
	for i := 1; i <= 10; i++ {
		if got := e.localRegister(t, "same-invite-token-1234", "Bob", localTestPassword).StatusCode; got != http.StatusNotFound {
			t.Fatalf("attempt %d = %d, want 404", i, got)
		}
	}
	if got := e.localRegister(t, "same-invite-token-1234", "Bob", localTestPassword).StatusCode; got != http.StatusTooManyRequests {
		t.Fatalf("11th attempt = %d, want 429", got)
	}
}

func TestLocalAuthDisabledHasNoRegisterRoute(t *testing.T) {
	e := newTestEnv(t)
	got := e.localRegister(t, "any-token-123456", "Bob", localTestPassword).StatusCode
	if got != http.StatusNotFound && got != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 404/405", got)
	}
}

func TestBootstrapCreatesAdminOnce(t *testing.T) {
	e := newLocalTestEnv(t)
	e.h.cfg.AdminEmail = "Admin@Example.com"
	e.h.cfg.AdminPassword = localTestPassword

	ctx := context.Background()
	if err := e.h.EnsureBootstrapAdmin(ctx); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if got := e.localLogin(t, "admin@example.com", localTestPassword).StatusCode; got != http.StatusNoContent {
		t.Fatalf("login after first bootstrap = %d, want 204", got)
	}

	// Second call with a different password must not overwrite the existing hash.
	e.h.cfg.AdminPassword = "another-valid-password"
	if err := e.h.EnsureBootstrapAdmin(ctx); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if got := e.localLogin(t, "admin@example.com", localTestPassword).StatusCode; got != http.StatusNoContent {
		t.Fatalf("original password login after second bootstrap = %d, want 204", got)
	}
	if got := e.localLogin(t, "admin@example.com", "another-valid-password").StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("overwritten password login = %d, want 401", got)
	}

	// Adopting an existing Google user (NULL password_hash) sets the password on that row.
	gUID := e.seedGoogleUser(t, "gadmin@example.com")
	e.h.cfg.AdminEmail = "gadmin@example.com"
	e.h.cfg.AdminPassword = localTestPassword
	if err := e.h.EnsureBootstrapAdmin(ctx); err != nil {
		t.Fatalf("google-adopt bootstrap: %v", err)
	}
	var uid int64
	if err := e.pool.QueryRow(ctx, `SELECT id FROM users WHERE lower(email)='gadmin@example.com' AND password_hash IS NOT NULL`).Scan(&uid); err != nil {
		t.Fatalf("adopted user query: %v", err)
	}
	if uid != gUID {
		t.Fatalf("adopted uid = %d, want %d", uid, gUID)
	}
}

func TestBootstrapNoopWhenDisabledOrUnset(t *testing.T) {
	ctx := context.Background()

	// Disabled LocalAuth
	e1 := newTestEnv(t)
	e1.h.cfg.AdminEmail = "admin@example.com"
	e1.h.cfg.AdminPassword = localTestPassword
	if err := e1.h.EnsureBootstrapAdmin(ctx); err != nil {
		t.Fatalf("disabled: %v", err)
	}
	if got := e1.count(t, `SELECT count(*) FROM users`); got != 0 {
		t.Fatalf("disabled created %d users, want 0", got)
	}

	// Missing email or password
	e2 := newLocalTestEnv(t)
	e2.h.cfg.AdminEmail = "admin@example.com"
	if err := e2.h.EnsureBootstrapAdmin(ctx); err != nil {
		t.Fatalf("missing password: %v", err)
	}
	e2.h.cfg.AdminEmail = ""
	e2.h.cfg.AdminPassword = localTestPassword
	if err := e2.h.EnsureBootstrapAdmin(ctx); err != nil {
		t.Fatalf("missing email: %v", err)
	}
	if got := e2.count(t, `SELECT count(*) FROM users`); got != 0 {
		t.Fatalf("unset created %d users, want 0", got)
	}
}

func TestBootstrapRejectsWeakPassword(t *testing.T) {
	e := newLocalTestEnv(t)
	e.h.cfg.AdminEmail = "admin@example.com"
	e.h.cfg.AdminPassword = "short"

	err := e.h.EnsureBootstrapAdmin(context.Background())
	if !errors.Is(err, auth.ErrPasswordPolicy) {
		t.Fatalf("err = %v, want ErrPasswordPolicy", err)
	}
	if got := e.count(t, `SELECT count(*) FROM users`); got != 0 {
		t.Fatalf("created %d users on weak password, want 0", got)
	}
}

func TestAuthConfigReportsProviders(t *testing.T) {
	pool := testDB(t)
	fp := &fakeProvider{profile: Profile{Provider: "google", SSOID: "1", Email: "a@b.c"}}

	for _, tc := range []struct {
		name      string
		hasGoogle bool
		local     bool
	}{
		{"neither", false, false},
		{"google only", true, false},
		{"local only", false, true},
		{"both", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := map[string]OAuthProvider{}
			if tc.hasGoogle {
				providers["google"] = fp
			}
			h := New(pool, config.Config{Env: "development", JWTSecret: testSecret, LocalAuth: tc.local},
				auth.NewIssuer(testSecret, 15*time.Minute), providers)
			app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
			h.Routes(app.Group("/api/v1"))
			e := &testEnv{app: app, h: h, pool: pool}

			resp := e.do(t, "GET", "/api/v1/auth/config", nil, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			var body struct {
				Google bool `json:"google"`
				Local  bool `json:"local"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Google != tc.hasGoogle || body.Local != tc.local {
				t.Fatalf("got %+v, want google=%v local=%v", body, tc.hasGoogle, tc.local)
			}
		})
	}
}
