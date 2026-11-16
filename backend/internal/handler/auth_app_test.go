package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func pkcePair() (verifier, challenge string) {
	verifier = strings.Repeat("v", 64)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// appLogin runs the native-app login up to the deep-link page and returns the one-time code.
func (e *testEnv) appLogin(t *testing.T, challenge string) string {
	t.Helper()
	resp := e.do(t, "GET", "/api/v1/auth/google/login?client=app&code_challenge="+challenge, nil, nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	state := loc.Query().Get("state")
	sc, ac := cookieNamed(resp, "oauth_state"), cookieNamed(resp, "oauth_app_challenge")
	if sc == nil || ac == nil || ac.Value != challenge {
		t.Fatalf("login must set oauth_state and oauth_app_challenge cookies (%v, %v)", sc, ac)
	}
	cb := e.do(t, "GET", "/api/v1/auth/google/callback?code=ok&state="+url.QueryEscape(state), nil, nil, sc, ac)
	if cb.StatusCode != http.StatusOK {
		t.Fatalf("app callback status = %d, want 200 bounce page", cb.StatusCode)
	}
	if cookieNamed(cb, "refresh_token") != nil {
		t.Fatal("app callback must not set the refresh cookie")
	}
	body := new(strings.Builder)
	buf := make([]byte, 4096)
	for {
		n, err := cb.Body.Read(buf)
		body.Write(buf[:n])
		if err != nil {
			break
		}
	}
	const marker = "artracker://auth/callback?code="
	i := strings.Index(body.String(), marker)
	if i < 0 {
		t.Fatalf("bounce page has no deep link: %s", body.String())
	}
	rest := body.String()[i+len(marker):]
	return rest[:strings.IndexAny(rest, `"&`)]
}

func (e *testEnv) exchange(t *testing.T, code, verifier string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"code": code, "code_verifier": verifier})
	return e.do(t, "POST", "/api/v1/auth/app/exchange", strings.NewReader(string(b)),
		map[string]string{"Content-Type": "application/json"})
}

func TestAppLoginExchangeIssuesTokens(t *testing.T) {
	e := newTestEnv(t)
	verifier, challenge := pkcePair()
	code := e.appLogin(t, challenge)

	resp := e.exchange(t, code, verifier)
	if resp.StatusCode != 200 {
		t.Fatalf("exchange status = %d", resp.StatusCode)
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if _, err := e.h.issuer.VerifyAccess(out.AccessToken); err != nil || out.RefreshToken == "" {
		t.Fatalf("bad exchange response: %v %+v", err, out)
	}

	// Refresh through the body (no cookie jar in the app) rotates and returns the new token.
	b, _ := json.Marshal(map[string]string{"refresh_token": out.RefreshToken})
	r := e.do(t, "POST", "/api/v1/auth/refresh", strings.NewReader(string(b)), map[string]string{"Content-Type": "application/json"})
	if r.StatusCode != 200 {
		t.Fatalf("body refresh status = %d", r.StatusCode)
	}
	var out2 struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&out2)
	if out2.RefreshToken == "" || out2.RefreshToken == out.RefreshToken {
		t.Fatal("body refresh must return a rotated refresh_token")
	}
	if cookieNamed(r, "refresh_token") != nil {
		t.Fatal("body refresh must not set a cookie")
	}

	// Logout through the body revokes it.
	b, _ = json.Marshal(map[string]string{"refresh_token": out2.RefreshToken})
	lo := e.do(t, "POST", "/api/v1/auth/logout", strings.NewReader(string(b)), map[string]string{"Content-Type": "application/json"})
	if lo.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", lo.StatusCode)
	}
	again := e.do(t, "POST", "/api/v1/auth/refresh", strings.NewReader(string(b)), map[string]string{"Content-Type": "application/json"})
	if again.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token refresh status = %d, want 401", again.StatusCode)
	}
}

func TestAppExchangeRejectsReuseWrongVerifierAndExpired(t *testing.T) {
	e := newTestEnv(t)
	verifier, challenge := pkcePair()

	code := e.appLogin(t, challenge)
	if r := e.exchange(t, code, strings.Repeat("x", 64)); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong verifier status = %d, want 401", r.StatusCode)
	}
	if r := e.exchange(t, code, verifier); r.StatusCode != 200 {
		t.Fatalf("correct verifier after a failed attempt = %d, want 200", r.StatusCode)
	}
	if r := e.exchange(t, code, verifier); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused code status = %d, want 401", r.StatusCode)
	}

	expired := e.appLogin(t, challenge)
	if _, err := e.pool.Exec(t.Context(), `UPDATE app_auth_codes SET expires_at = now() - interval '1 second' WHERE used_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if r := e.exchange(t, expired, verifier); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired code status = %d, want 401", r.StatusCode)
	}
	if r := e.exchange(t, "", ""); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("empty exchange status = %d, want 401", r.StatusCode)
	}
}

func TestAppLoginRejectsBadChallengeAndWebLoginUnaffected(t *testing.T) {
	e := newTestEnv(t)
	if r := e.do(t, "GET", "/api/v1/auth/google/login?client=app&code_challenge=short", nil, nil); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad challenge status = %d, want 400", r.StatusCode)
	}
	e.login(t) // plain web flow still sets the refresh cookie and redirects to the dashboard
}

func TestAppCallbackErrorBouncesToApp(t *testing.T) {
	e := newTestEnv(t)
	_, challenge := pkcePair()
	resp := e.do(t, "GET", "/api/v1/auth/google/login?client=app&code_challenge="+challenge, nil, nil)
	loc, _ := url.Parse(resp.Header.Get("Location"))
	state := loc.Query().Get("state")
	sc, ac := cookieNamed(resp, "oauth_state"), cookieNamed(resp, "oauth_app_challenge")
	cb := e.do(t, "GET", "/api/v1/auth/google/callback?error=access_denied&state="+url.QueryEscape(state), nil, nil, sc, ac)
	buf := make([]byte, 4096)
	n, _ := cb.Body.Read(buf)
	if cb.StatusCode != 200 || !strings.Contains(string(buf[:n]), "artracker://auth/callback?error=access_denied") {
		t.Fatalf("app error must bounce to the app: %d %s", cb.StatusCode, buf[:n])
	}
}
