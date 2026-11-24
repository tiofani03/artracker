package handler

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/jackc/pgx/v5"

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
