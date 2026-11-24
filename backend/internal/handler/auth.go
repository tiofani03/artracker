package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tiofani03/artracker/backend/internal/auth"
)

const (
	stateCookie   = "oauth_state"
	refreshCookie = "refresh_token"
	refreshTTL    = 30 * 24 * time.Hour
	// A rotated token presented again within this window is a concurrent
	// refresh (second tab, lost response), not theft: reject without revoking.
	refreshGrace = 10 * time.Second
	authPath     = "/api/v1/auth"

	// Native app (Tauri) login: PKCE challenge kept in a cookie of the system browser
	// between /login and /callback, then redeemed through /auth/app/exchange.
	appChallengeCookie = "oauth_app_challenge"
	appCodeTTL         = 60 * time.Second
	appScheme          = "artracker://auth/callback"
)

// A S256 PKCE challenge is 43 base64url characters (RFC 7636).
var pkceChallengeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// loginError sends a browser-facing OAuth failure back to the SPA login page.
func (h *Handler) loginError(c *fiber.Ctx, code string) error {
	if c.Cookies(appChallengeCookie) != "" {
		h.setCookie(c, appChallengeCookie, "", -time.Hour)
		return appBounce(c, appScheme+"?error="+code)
	}
	return c.Redirect(h.cfg.PublicURL+"/login?error="+code, fiber.StatusFound)
}

func (h *Handler) redirectURL(provider string) string {
	return h.cfg.PublicURL + "/auth/callback/" + provider
}

func (h *Handler) setCookie(c *fiber.Ctx, name, value string, ttl time.Duration) {
	c.Cookie(&fiber.Cookie{
		Name: name, Value: value, Path: "/",
		HTTPOnly: true, SameSite: "Lax", Secure: h.cfg.Env == "production",
		Expires: time.Now().Add(ttl),
	})
}

// OAuthLogin starts the authorization-code flow with a CSRF state cookie.
func (h *Handler) OAuthLogin(c *fiber.Ctx) error {
	p, ok := h.providers[c.Params("provider")]
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "unknown provider")
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	state := base64.RawURLEncoding.EncodeToString(b)
	h.setCookie(c, stateCookie, state, 10*time.Minute)
	if c.Query("client") == "app" {
		ch := c.Query("code_challenge")
		if !pkceChallengeRe.MatchString(ch) {
			return fiber.NewError(fiber.StatusBadRequest, "invalid code_challenge")
		}
		h.setCookie(c, appChallengeCookie, ch, 10*time.Minute)
	} else {
		h.setCookie(c, appChallengeCookie, "", -time.Hour)
	}
	return c.Redirect(p.AuthURL(state, h.redirectURL(c.Params("provider"))), fiber.StatusFound)
}

func (h *Handler) OAuthCallback(c *fiber.Ctx) error {
	name := c.Params("provider")
	p, ok := h.providers[name]
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "unknown provider")
	}
	want, got := c.Cookies(stateCookie), c.Query("state")
	if want == "" || got == "" || subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
		return h.loginError(c, "invalid_state")
	}
	h.setCookie(c, stateCookie, "", -time.Hour)

	if c.Query("error") != "" { // user cancelled consent or provider refused
		return h.loginError(c, "access_denied")
	}
	code := c.Query("code")
	if code == "" {
		return h.loginError(c, "invalid_state")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 15*time.Second)
	defer cancel()

	prof, err := p.Exchange(ctx, code, h.redirectURL(name))
	if err != nil {
		slog.Warn("oauth exchange failed", "provider", name, "err", err)
		return h.loginError(c, "provider_failed")
	}

	var uid int64
	err = h.db.QueryRow(ctx, `
		INSERT INTO users (email, name, avatar_url, sso_provider, sso_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (sso_provider, sso_id)
		DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name, avatar_url = EXCLUDED.avatar_url
		RETURNING id`, prof.Email, prof.Name, prof.AvatarURL, prof.Provider, prof.SSOID).Scan(&uid)
	if err != nil {
		return err
	}

	if challenge := c.Cookies(appChallengeCookie); challenge != "" {
		h.setCookie(c, appChallengeCookie, "", -time.Hour)
		code, codeHash, err := auth.NewRefreshToken() // same opaque random format
		if err != nil {
			return err
		}
		if _, err := h.db.Exec(ctx, `INSERT INTO app_auth_codes (code_hash, user_id, code_challenge, expires_at) VALUES ($1, $2, $3, $4)`,
			codeHash, uid, challenge, time.Now().Add(appCodeTTL)); err != nil {
			return err
		}
		return appBounce(c, appScheme+"?code="+code)
	}

	if err := h.startSession(ctx, c, uid); err != nil {
		return err
	}
	return c.Redirect(h.cfg.PublicURL+"/dashboard", fiber.StatusFound)
}

// startSession stores a new refresh token for uid and sets it as the httpOnly refresh cookie.
// Shared by every login method; the access JWT is minted later by /auth/refresh.
func (h *Handler) startSession(ctx context.Context, c *fiber.Ctx, uid int64) error {
	plain, hash, err := auth.NewRefreshToken()
	if err != nil {
		return err
	}
	if _, err := h.db.Exec(ctx, `INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		uid, hash, time.Now().Add(refreshTTL)); err != nil {
		return err
	}
	h.setCookie(c, refreshCookie, plain, refreshTTL)
	return nil
}

// appBounce hands control back to the native app. A plain 302 to a custom scheme is
// dropped by some mobile browsers, so serve a page that navigates and offers a button.
func appBounce(c *fiber.Ctx, target string) error {
	c.Set("Cache-Control", "no-store")
	c.Set("Referrer-Policy", "no-referrer")
	c.Type("html", "utf-8")
	t := html.EscapeString(target)
	return c.SendString(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>ARTracker</title>
<meta http-equiv="refresh" content="0;url=%[1]s"></head>
<body style="font-family:system-ui,sans-serif;text-align:center;padding:3rem 1rem">
<p>Returning to ARTracker&hellip;</p>
<p><a href="%[1]s" style="display:inline-block;padding:.7rem 1.2rem;border-radius:8px;background:#0853c9;color:#fff;text-decoration:none">Open ARTracker</a></p>
<p style="color:#666;font-size:.9rem">You can close this tab afterwards.</p>
</body></html>`, t))
}

type appExchangeRequest struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
}

// AppExchange redeems the one-time code from the deep link. The PKCE verifier proves
// the caller is the app instance that started the login, not another app that
// registered the same URL scheme.
func (h *Handler) AppExchange(c *fiber.Ctx) error {
	var req appExchangeRequest
	if err := c.BodyParser(&req); err != nil || req.Code == "" || req.CodeVerifier == "" {
		return errUnauthorized
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		id, uid   int64
		challenge string
		expires   time.Time
		used      *time.Time
	)
	err = tx.QueryRow(ctx, `SELECT id, user_id, code_challenge, expires_at, used_at FROM app_auth_codes WHERE code_hash = $1 FOR UPDATE`,
		auth.HashToken(req.Code)).Scan(&id, &uid, &challenge, &expires, &used)
	if errors.Is(err, pgx.ErrNoRows) {
		return errUnauthorized
	}
	if err != nil {
		return err
	}
	if used != nil || time.Now().After(expires) {
		return errUnauthorized
	}
	sum := sha256.Sum256([]byte(req.CodeVerifier))
	if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) != 1 {
		return errUnauthorized
	}
	// Burn the code before issuing anything: it is single use even if the verifier was right.
	if _, err := tx.Exec(ctx, `UPDATE app_auth_codes SET used_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	plain, hash, err := auth.NewRefreshToken()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		uid, hash, time.Now().Add(refreshTTL)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	access, exp, err := h.issuer.IssueAccess(uid)
	if err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(fiber.Map{"access_token": access, "expires_at": exp, "refresh_token": plain})
}

// refreshTokenFrom reads the refresh token from the cookie (web) or, for the native
// app which has no usable cookie jar, from a JSON body.
func refreshTokenFrom(c *fiber.Ctx) (plain string, viaBody bool) {
	if v := c.Cookies(refreshCookie); v != "" {
		return v, false
	}
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.BodyParser(&body); err == nil && body.RefreshToken != "" {
		return body.RefreshToken, true
	}
	return "", false
}

// Refresh rotates the refresh token. Presenting an already-rotated token is
// treated as theft: every active token of that user is revoked.
func (h *Handler) Refresh(c *fiber.Ctx) error {
	plain, viaBody := refreshTokenFrom(c)
	if plain == "" {
		return errUnauthorized
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		id, uid int64
		expires time.Time
		revoked *time.Time
	)
	err = tx.QueryRow(ctx, `SELECT id, user_id, expires_at, revoked_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`,
		auth.HashToken(plain)).Scan(&id, &uid, &expires, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return errUnauthorized
	}
	if err != nil {
		return err
	}
	if revoked != nil {
		if time.Since(*revoked) < refreshGrace {
			return errUnauthorized // concurrent refresh; the winner's token stays valid
		}
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, uid); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return errUnauthorized
	}
	if time.Now().After(expires) {
		return errUnauthorized
	}

	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	newPlain, newHash, err := auth.NewRefreshToken()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		uid, newHash, time.Now().Add(refreshTTL)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	access, exp, err := h.issuer.IssueAccess(uid)
	if err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	if viaBody {
		return c.JSON(fiber.Map{"access_token": access, "expires_at": exp, "refresh_token": newPlain})
	}
	h.setCookie(c, refreshCookie, newPlain, refreshTTL)
	return c.JSON(fiber.Map{"access_token": access, "expires_at": exp})
}

func (h *Handler) Logout(c *fiber.Ctx) error {
	if plain, _ := refreshTokenFrom(c); plain != "" {
		ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
		defer cancel()
		if _, err := h.db.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`,
			auth.HashToken(plain)); err != nil {
			return err
		}
	}
	h.setCookie(c, refreshCookie, "", -time.Hour)
	return c.SendStatus(fiber.StatusNoContent)
}

type orgMembership struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}

func (h *Handler) Me(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	var u struct {
		ID        int64           `json:"id"`
		Email     string          `json:"email"`
		Name      string          `json:"name"`
		AvatarURL string          `json:"avatar_url"`
		Orgs      []orgMembership `json:"orgs"`
	}
	err := h.db.QueryRow(ctx, `SELECT id, email, name, avatar_url FROM users WHERE id = $1`, userID(c)).
		Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return errUnauthorized
	}
	if err != nil {
		return err
	}
	orgs, err := h.listOrgs(ctx, u.ID)
	if err != nil {
		return err
	}
	u.Orgs = orgs
	return c.JSON(u)
}

func (h *Handler) listOrgs(ctx context.Context, uid int64) ([]orgMembership, error) {
	rows, err := h.db.Query(ctx, `
		SELECT o.id, o.name, o.slug, m.role FROM organization_members m
		JOIN organizations o ON o.id = m.org_id WHERE m.user_id = $1 ORDER BY o.name`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []orgMembership{}
	for rows.Next() {
		var o orgMembership
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Role); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
