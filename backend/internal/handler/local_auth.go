package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tiofani03/artracker/backend/internal/auth"
)

const (
	localAuthMaxFailures = 10
	localAuthWindow      = 15 * time.Minute
)

// One message for every way a login can fail, so the response never says which emails exist.
var errBadLogin = fiber.NewError(fiber.StatusUnauthorized, "invalid email or password")

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// localAuthLimiter caps failed attempts per key. Successful requests do not count, so a real user is never
// locked out by their own logins. Keyed by what the caller supplies (email, invite token) instead of IP,
// because behind the Coolify proxy every client shares one address.
func localAuthLimiter(key func(*fiber.Ctx) string) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:                    localAuthMaxFailures,
		Expiration:             localAuthWindow,
		KeyGenerator:           key,
		SkipSuccessfulRequests: true,
		LimitReached: func(*fiber.Ctx) error {
			return fiber.NewError(fiber.StatusTooManyRequests, "too many attempts, try again later")
		},
	})
}

// statusFromError copies a returned *fiber.Error's code onto the response before the limiter inspects it.
// Fiber only applies ErrorHandler after the whole chain returns, so without this the limiter sees 200 for a
// failed login and never counts it. Place it directly after localAuthLimiter.
func statusFromError(c *fiber.Ctx) error {
	err := c.Next()
	var fe *fiber.Error
	if errors.As(err, &fe) {
		c.Status(fe.Code)
	}
	return err
}

// loginLimiterKey keys the login limiter by the normalized email in the JSON body.
func loginLimiterKey(c *fiber.Ctx) string {
	var b struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(c.Body(), &b); err != nil || normalizeEmail(b.Email) == "" {
		return "ip:" + c.IP()
	}
	return "email:" + normalizeEmail(b.Email)
}

type localLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LocalLogin signs in with email + password and sets the refresh cookie (204). Any user row that carries
// a password hash can sign in, whichever provider created it.
func (h *Handler) LocalLogin(c *fiber.Ctx) error {
	var req localLoginRequest
	if err := c.BodyParser(&req); err != nil || req.Email == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "email and password are required")
	}
	ctx := c.Context()

	var (
		uid  int64
		hash string
	)
	err := h.db.QueryRow(ctx,
		`SELECT id, password_hash FROM users WHERE lower(email) = $1 AND password_hash IS NOT NULL`,
		normalizeEmail(req.Email)).Scan(&uid, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		auth.DummyCheck(req.Password) // same cost as a real check
		return errBadLogin
	}
	if err != nil {
		return err
	}
	if !auth.CheckPassword(hash, req.Password) {
		return errBadLogin
	}
	if err := h.startSession(ctx, c, uid); err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	return c.SendStatus(fiber.StatusNoContent)
}

// registerLimiterKey keys the register limiter by the invite_token in the JSON body.
func registerLimiterKey(c *fiber.Ctx) string {
	var b struct {
		InviteToken string `json:"invite_token"`
	}
	if err := json.Unmarshal(c.Body(), &b); err != nil || strings.TrimSpace(b.InviteToken) == "" {
		return "ip:" + c.IP()
	}
	return "invite:" + strings.TrimSpace(b.InviteToken)
}

type localRegisterRequest struct {
	InviteToken string `json:"invite_token"`
	Name        string `json:"name"`
	Password    string `json:"password"`
}

// LocalRegister creates (or attaches a password to) a user from an organization invitation,
// joins the organization, marks the invite accepted, and starts a session (201).
func (h *Handler) LocalRegister(c *fiber.Ctx) error {
	var req localRegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}
	req.InviteToken = strings.TrimSpace(req.InviteToken)
	req.Name = strings.TrimSpace(req.Name)
	if req.InviteToken == "" {
		return fiber.NewError(fiber.StatusBadRequest, "invite_token is required")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()

	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		inviteID, orgID int64
		invEmail, role  string
		expiresAt       time.Time
		acceptedAt      *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT id, org_id, email, role, expires_at, accepted_at
		FROM organization_invitations
		WHERE token_hash = $1
		FOR UPDATE`, hashInviteToken(req.InviteToken)).
		Scan(&inviteID, &orgID, &invEmail, &role, &expiresAt, &acceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "invitation not found")
	}
	if err != nil {
		return err
	}
	if acceptedAt != nil {
		return fiber.NewError(fiber.StatusConflict, "this invitation has already been accepted")
	}
	if time.Now().After(expiresAt) {
		return fiber.NewError(fiber.StatusGone, "this invitation has expired")
	}

	// Validate password before consuming the invitation or touching user rows.
	if err := auth.ValidatePassword(req.Password); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	email := normalizeEmail(invEmail)

	var (
		uid          int64
		existingHash *string
	)
	err = tx.QueryRow(ctx, `
		SELECT id, password_hash FROM users
		WHERE lower(email) = $1
		ORDER BY id ASC LIMIT 1
		FOR UPDATE`, email).Scan(&uid, &existingHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && existingHash != nil {
		return fiber.NewError(fiber.StatusConflict, "an account with this email already exists")
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return err
	}

	if errors.Is(err, pgx.ErrNoRows) || existingHash == nil && uid == 0 {
		err = tx.QueryRow(ctx, `
			INSERT INTO users (email, name, sso_provider, sso_id, password_hash)
			VALUES ($1, $2, 'local', $1, $3)
			RETURNING id`, email, req.Name, hash).Scan(&uid)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return fiber.NewError(fiber.StatusConflict, "an account with this email already exists")
			}
			return err
		}
	} else {
		// Adopt an existing passwordless account (e.g. Google user) while keeping its profile and provider.
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, hash, uid); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_members (org_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		orgID, uid, role); err != nil {
		return err
	}

	var revoked int64
	if role == "viewer" {
		if revoked, err = revokeMemberMCPKeys(ctx, tx, orgID, uid); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE organization_invitations
		SET accepted_at = now(), accepted_by = $1
		WHERE id = $2`, uid, inviteID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if revoked > 0 {
		h.invalidateKeyCache()
	}

	if err := h.startSession(ctx, c, uid); err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"user_id": uid,
		"org_id":  orgID,
		"role":    role,
	})
}
