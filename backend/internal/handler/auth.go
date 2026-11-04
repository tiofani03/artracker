package handler

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
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
)

// loginError sends a browser-facing OAuth failure back to the SPA login page.
func (h *Handler) loginError(c *fiber.Ctx, code string) error {
	return c.Redirect(h.cfg.PublicURL+"/login?error="+code, fiber.StatusFound)
}

func (h *Handler) redirectURL(provider string) string {
	return h.cfg.PublicURL + authPath + "/" + provider + "/callback"
}

func (h *Handler) setCookie(c *fiber.Ctx, name, value string, ttl time.Duration) {
	c.Cookie(&fiber.Cookie{
		Name: name, Value: value, Path: authPath,
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

	plain, hash, err := auth.NewRefreshToken()
	if err != nil {
		return err
	}
	if _, err := h.db.Exec(ctx, `INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		uid, hash, time.Now().Add(refreshTTL)); err != nil {
		return err
	}
	h.setCookie(c, refreshCookie, plain, refreshTTL)
	return c.Redirect(h.cfg.PublicURL+"/dashboard", fiber.StatusFound)
}

// Refresh rotates the refresh token. Presenting an already-rotated token is
// treated as theft: every active token of that user is revoked.
func (h *Handler) Refresh(c *fiber.Ctx) error {
	plain := c.Cookies(refreshCookie)
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
	h.setCookie(c, refreshCookie, newPlain, refreshTTL)
	c.Set("Cache-Control", "no-store")
	return c.JSON(fiber.Map{"access_token": access, "expires_at": exp})
}

func (h *Handler) Logout(c *fiber.Ctx) error {
	if plain := c.Cookies(refreshCookie); plain != "" {
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
