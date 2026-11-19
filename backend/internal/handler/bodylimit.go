package handler

import (
	"io"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// LimitBody lets only requests under uploadPrefixes (release symbol uploads) use
// the server-wide body limit, which is raised for them. Every other route keeps
// defaultMax. It needs fiber.Config{StreamRequestBody: true}: a body larger
// than the read buffer is then not read before the handler runs, so an
// oversized request is refused without buffering it.
func LimitBody(defaultMax int, uploadPrefixes ...string) fiber.Handler {
	refuse := func(c *fiber.Ctx) error {
		// The rest of the body is never read; close the connection so the server
		// does not parse those bytes as the next request.
		c.Context().SetConnectionClose()
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "request body too large")
	}
	return func(c *fiber.Ctx) error {
		path := c.Path()
		for _, prefix := range uploadPrefixes {
			if strings.HasPrefix(path, prefix) {
				return c.Next()
			}
		}
		req := c.Request()
		switch cl := req.Header.ContentLength(); {
		case cl > defaultMax:
			return refuse(c)
		case cl == -1:
			// No Content-Length (chunked): read at most defaultMax+1 bytes and
			// put the body back, so a chunked upload cannot bypass the limit.
			if stream := c.Context().RequestBodyStream(); stream != nil {
				buf, err := io.ReadAll(io.LimitReader(stream, int64(defaultMax)+1))
				if err != nil {
					return fiber.NewError(fiber.StatusBadRequest, "cannot read request body")
				}
				if len(buf) > defaultMax {
					return refuse(c)
				}
				req.SetBody(buf)
			} else if len(req.Body()) > defaultMax {
				return refuse(c)
			}
		}
		return c.Next()
	}
}
