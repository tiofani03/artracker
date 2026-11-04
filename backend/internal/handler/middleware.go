package handler

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

var errUnauthorized = fiber.NewError(fiber.StatusUnauthorized, "unauthorized")

// RequireAuth validates the bearer access token and stores the user id.
func (h *Handler) RequireAuth(c *fiber.Ctx) error {
	hdr := c.Get("Authorization")
	token, ok := strings.CutPrefix(hdr, "Bearer ")
	if !ok || token == "" {
		return errUnauthorized
	}
	id, err := h.issuer.VerifyAccess(token)
	if err != nil {
		return errUnauthorized
	}
	c.Locals("userID", id)
	return c.Next()
}

func userID(c *fiber.Ctx) int64 {
	id, _ := c.Locals("userID").(int64)
	return id
}
