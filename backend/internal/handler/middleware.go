package handler

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

var errUnauthorized = fiber.NewError(fiber.StatusUnauthorized, "unauthorized")

// demoMode reports whether this request may use the dashboard's demo preview: the demo header / query is
// honoured only when APP_ENV=development. Anywhere else (production, staging, ...) it is ignored, so it can
// neither stand in for a login nor skip a project role check.
func (h *Handler) demoMode(c *fiber.Ctx) bool {
	return h.cfg.Env == "development" && (c.Get("X-Demo-Mode") == "1" || c.Query("demo") == "1")
}

// RequireAuth validates the bearer access token and stores the user id.
func (h *Handler) RequireAuth(c *fiber.Ctx) error {
	hdr := c.Get("Authorization")
	token, ok := strings.CutPrefix(hdr, "Bearer ")
	if !ok || token == "" {
		if h.demoMode(c) {
			c.Locals("userID", int64(1))
			return c.Next()
		}
		return errUnauthorized
	}
	id, err := h.issuer.VerifyAccess(token)
	if err != nil {
		if h.demoMode(c) {
			c.Locals("userID", int64(1))
			return c.Next()
		}
		return errUnauthorized
	}
	c.Locals("userID", id)
	return c.Next()
}

func userID(c *fiber.Ctx) int64 {
	id, _ := c.Locals("userID").(int64)
	return id
}
