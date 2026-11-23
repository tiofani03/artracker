package handler

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"mime/multipart"
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

func TestReadArtifactUploadMultipartDSYM(t *testing.T) {
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	fw, err := zw.Create("Alfagift.app.dSYM/Contents/Resources/DWARF/Alfagift.debug.dylib")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("macho-dwarf-payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	var mpBuf bytes.Buffer
	mw := multipart.NewWriter(&mpBuf)
	_ = mw.WriteField("app_id", "com.alfamart.alfagift.debug")
	_ = mw.WriteField("bundle_name", "Alfagift.app.dSYM")
	part, err := mw.CreateFormFile("file", "Alfagift.app.dSYM.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(zipBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	app.Post("/u", func(c *fiber.Ctx) error {
		body, fn, fields, err := readArtifactUpload(c)
		if err != nil {
			return err
		}
		if fn != "Alfagift.app.dSYM.zip" {
			t.Errorf("fileName = %q, want Alfagift.app.dSYM.zip", fn)
		}
		if fields["bundle_name"] != "Alfagift.app.dSYM" {
			t.Errorf("bundle_name = %q, want Alfagift.app.dSYM", fields["bundle_name"])
		}
		return c.Send(body)
	})

	// Test both with Content-Type header and without Content-Type (auto-detected envelope)
	for _, withHeader := range []bool{true, false} {
		req := httptest.NewRequest("POST", "/u", bytes.NewReader(mpBuf.Bytes()))
		if withHeader {
			req.Header.Set("Content-Type", mw.FormDataContentType())
		}
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || !bytes.Equal(got, zipBuf.Bytes()) {
			t.Fatalf("withHeader=%v: status=%d len=%d, want 200 len=%d", withHeader, resp.StatusCode, len(got), zipBuf.Len())
		}
	}
}
