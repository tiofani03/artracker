package handler

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
)

// APIError is the single error shape the API returns.
type APIError struct {
	Error string `json:"error"`
}

// ErrorHandler keeps Fiber's default plain-text errors from leaking internals
// and normalises everything to JSON.
func ErrorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	msg := "internal server error"

	var fe *fiber.Error
	if errors.As(err, &fe) {
		code = fe.Code
		msg = fe.Message
	} else {
		slog.Error("unhandled request error", "err", err, "path", c.Path())
	}

	return c.Status(code).JSON(APIError{Error: msg})
}
