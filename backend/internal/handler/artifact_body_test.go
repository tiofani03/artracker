package handler

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func gz(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadArtifactBody(t *testing.T) {
	defer func(old int64) { artifactMaxDecoded = old }(artifactMaxDecoded)
	artifactMaxDecoded = 1 << 20 // 1 MiB keeps the bomb case fast

	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	app.Post("/b", func(c *fiber.Ctx) error {
		b, err := readArtifactBody(c)
		if err != nil {
			return err
		}
		return c.Send(b)
	})

	cases := []struct {
		name string
		enc  string
		body []byte
		want int
		out  string
	}{
		{"plain", "", []byte("abc"), 200, "abc"},
		{"identity", "identity", []byte("abc"), 200, "abc"},
		{"gzip", "gzip", gz(t, []byte("abc")), 200, "abc"},
		{"gzip uppercase header", "GZIP", gz(t, []byte("abc")), 200, "abc"},
		{"not gzip", "gzip", []byte("notgzip"), 400, ""},
		{"truncated gzip", "gzip", gz(t, bytes.Repeat([]byte("x"), 1000))[:20], 400, ""},
		{"unsupported encoding", "br", []byte("abc"), 415, ""},
		{"gzip at the cap", "gzip", gz(t, make([]byte, 1<<20)), 200, ""},
		{"gzip bomb over the cap", "gzip", gz(t, make([]byte, 1<<20+1)), 413, ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/b", bytes.NewReader(c.body))
		if c.enc != "" {
			req.Header.Set("Content-Encoding", c.enc)
		}
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != c.want {
			t.Errorf("%s: status = %d, want %d", c.name, resp.StatusCode, c.want)
		}
		if c.out != "" && string(out) != c.out {
			t.Errorf("%s: body = %q, want %q", c.name, out, c.out)
		}
	}
}
