package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func limitBodyApp() *fiber.App {
	app := fiber.New(fiber.Config{BodyLimit: 64 << 20, StreamRequestBody: true, ErrorHandler: ErrorHandler})
	app.Use(LimitBody(4<<20, "/api/v1/sdk/releases/"))
	app.Post("/api/v1/ingest", func(c *fiber.Ctx) error { return c.SendString(strconv.Itoa(len(c.Body()))) })
	app.Post("/api/v1/sdk/releases/:v/proguard", func(c *fiber.Ctx) error { return c.SendString(strconv.Itoa(len(c.Body()))) })
	return app
}

func postBody(t *testing.T, app *fiber.App, path string, n int, chunked bool) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", path, bytes.NewReader(make([]byte, n)))
	if chunked {
		req.ContentLength = -1
		req.TransferEncoding = []string{"chunked"}
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestLimitBody(t *testing.T) {
	app := limitBodyApp()
	const five = 5 << 20

	cases := []struct {
		name    string
		path    string
		n       int
		chunked bool
		want    int
		body    string
	}{
		{"big body to ingest", "/api/v1/ingest", five, false, http.StatusRequestEntityTooLarge, ""},
		{"big chunked body to ingest", "/api/v1/ingest", five, true, http.StatusRequestEntityTooLarge, ""},
		{"small body to ingest", "/api/v1/ingest", 1024, false, 200, "1024"},
		{"small chunked body to ingest", "/api/v1/ingest", 1024, true, 200, "1024"},
		{"big body to upload route", "/api/v1/sdk/releases/1.0/proguard", five, false, 200, strconv.Itoa(five)},
		{"big chunked body to upload route", "/api/v1/sdk/releases/1.0/proguard", five, true, 200, strconv.Itoa(five)},
	}
	for _, c := range cases {
		code, body := postBody(t, app, c.path, c.n, c.chunked)
		if code != c.want {
			t.Errorf("%s: status = %d, want %d (%s)", c.name, code, c.want, body)
			continue
		}
		if c.body != "" && body != c.body {
			t.Errorf("%s: body = %q, want %q", c.name, body, c.body)
		}
	}
}
