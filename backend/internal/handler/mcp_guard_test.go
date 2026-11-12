package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// postMCPBody posts an arbitrary body to /mcp and returns status, headers and body.
func postMCPBody(t *testing.T, url, key, body string) (*http.Response, string) {
	t.Helper()
	hdr := map[string]string{}
	if key != "" {
		hdr["Authorization"] = "Bearer " + key
	}
	return rawMCPBody(t, "POST", url, body, hdr)
}

// nestedJSON returns a JSON object nested n levels deep: {"a":{"a":...1...}}.
func nestedJSON(n int) string {
	return strings.Repeat(`{"a":`, n) + "1" + strings.Repeat("}", n)
}

// initWithExperimental is an initialize request whose capabilities.experimental
// holds x. The envelope adds 3 levels (root, params, capabilities).
func initWithExperimental(x, clientName string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
		`"capabilities":{"experimental":` + x + `},"clientInfo":{"name":` + clientName + `,"version":"1"}}}`
}

func TestMCPRequestGuards(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	url := e.serveMCP(t)
	k := e.createKey(t, project, "mcp", hdr)

	deep := initWithExperimental(nestedJSON(37), `"t"`) // total depth 40
	batch := "  \n[" + mcpInitBody + "]"
	huge := initWithExperimental(`{}`, `"`+strings.Repeat("x", 70<<10)+`"`)

	// Authentication runs first: every malformed body is a 401 without a key.
	for name, body := range map[string]string{"deep": deep, "batch": batch, "huge": huge} {
		if resp, out := postMCPBody(t, url, "", body); resp.StatusCode != 401 {
			t.Fatalf("unauthenticated %s: status = %d (%s), want 401", name, resp.StatusCode, out)
		}
	}

	if resp, out := postMCPBody(t, url, k.Secret, huge); resp.StatusCode != 413 || !strings.Contains(out, `"error"`) {
		t.Fatalf("oversize: status = %d (%s), want 413 JSON error", resp.StatusCode, out)
	}
	if resp, out := postMCPBody(t, url, k.Secret, deep); resp.StatusCode != 400 || !strings.Contains(out, "nest") {
		t.Fatalf("depth 40: status = %d (%s), want 400", resp.StatusCode, out)
	}
	if resp, out := postMCPBody(t, url, k.Secret, batch); resp.StatusCode != 400 ||
		!strings.Contains(out, `"jsonrpc":"2.0"`) || !strings.Contains(out, "batch") {
		t.Fatalf("batch: status = %d (%s), want 400 JSON-RPC error", resp.StatusCode, out)
	}

	// Depth 20 passes, and brackets and escaped quotes inside strings are not counted.
	tricky := `"t\"` + strings.Repeat("[{", 40) + `\\"`
	if resp, out := postMCPBody(t, url, k.Secret, initWithExperimental(nestedJSON(17), tricky)); resp.StatusCode != 200 {
		t.Fatalf("depth 20: status = %d (%s), want 200", resp.StatusCode, out)
	}

	// A normal tools/call still works through the SDK client.
	cs, err := connectMCP(t, url, k.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if out, isErr := callTool(t, cs, "list_issues", nil); isErr {
		t.Fatalf("list_issues: %s", out)
	}
}

// postMCPEncoded posts body to /mcp with a Content-Encoding header (key may be "").
func postMCPEncoded(t *testing.T, url, key, body, encoding string) (*http.Response, string) {
	t.Helper()
	hdr := map[string]string{"Content-Encoding": encoding}
	if key != "" {
		hdr["Authorization"] = "Bearer " + key
	}
	return rawMCPBody(t, "POST", url, body, hdr)
}

// The guard must inspect the same raw bytes the SDK parses. Fiber's c.Body()
// decodes per Content-Encoding (and returns nothing or an error string for an
// unknown or malformed encoding), so any encoding other than identity is
// refused with 415 before the body is touched.
func TestMCPRejectsContentEncoding(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	url := e.serveMCP(t)
	k := e.createKey(t, project, "mcp", hdr)

	deep := initWithExperimental(nestedJSON(37), `"t"`)
	batch := "[" + mcpInitBody + "]"
	huge := initWithExperimental(`{}`, `"`+strings.Repeat("x", 70<<10)+`"`)
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(mcpInitBody))
	_ = zw.Close()

	// Authentication still runs first.
	if resp, out := postMCPEncoded(t, url, "", deep, "gzip"); resp.StatusCode != 401 {
		t.Fatalf("unauthenticated gzip: status = %d (%s), want 401", resp.StatusCode, out)
	}

	cases := []struct{ name, body, enc string }{
		{"deep identity,identity", deep, "identity, identity"},
		{"deep fake gzip", deep, "gzip"},
		{"huge identity,identity", huge, "identity, identity"},
		{"batch x", batch, "x"},
		{"batch x,y", batch, "x, y"},
		{"real gzip", gz.String(), "gzip"},
		{"real gzip upper", gz.String(), "GZIP"},
		{"deflate", mcpInitBody, "deflate"},
		{"br", mcpInitBody, "br"},
	}
	for _, c := range cases {
		resp, out := postMCPEncoded(t, url, k.Secret, c.body, c.enc)
		if resp.StatusCode != 415 || !strings.Contains(out, `"error"`) {
			t.Errorf("%s: status = %d (%.80s), want 415 JSON error", c.name, resp.StatusCode, out)
		}
	}

	// Without the header the existing guards still answer.
	if resp, out := postMCPBody(t, url, k.Secret, huge); resp.StatusCode != 413 {
		t.Fatalf("plain oversize: status = %d (%.200s), want 413", resp.StatusCode, out)
	}
	if resp, out := postMCPBody(t, url, k.Secret, batch); resp.StatusCode != 400 {
		t.Fatalf("plain batch: status = %d (%.200s), want 400", resp.StatusCode, out)
	}

	// identity (any case, surrounding spaces) is a no-op encoding and allowed.
	for _, enc := range []string{"identity", "Identity", " IDENTITY "} {
		if resp, out := postMCPEncoded(t, url, k.Secret, mcpInitBody, enc); resp.StatusCode != 200 {
			t.Fatalf("Content-Encoding %q: status = %d (%.200s), want 200", enc, resp.StatusCode, out)
		}
	}
	// identity does not bypass the size and depth guards.
	if resp, out := postMCPEncoded(t, url, k.Secret, deep, "identity"); resp.StatusCode != 400 {
		t.Fatalf("deep identity: status = %d (%.200s), want 400", resp.StatusCode, out)
	}
}

func TestJSONDepthExceeds(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want bool
	}{
		{`{}`, 1, false},
		{`{"a":[]}`, 1, true},
		{`{"a":[]}`, 2, false},
		{`"[[[[[["`, 1, false},
		{`{"a":"\"[[[["}`, 1, false},
		{`{"a":"\\"}`, 1, false},
		{`{"a":"\\",[[]]}`, 2, true},
		{`[[1],[2],[3]]`, 2, false},
	}
	for _, c := range cases {
		if got := jsonDepthExceeds([]byte(c.in), c.max); got != c.want {
			t.Errorf("jsonDepthExceeds(%s, %d) = %v, want %v", c.in, c.max, got, c.want)
		}
	}
}

func TestMCPRateLimit(t *testing.T) {
	e, org, project, hdr := keyFixture(t)
	other := e.addProject(t, org, "other-app")
	url := e.serveMCP(t)
	k := e.createKey(t, project, "mcp", hdr)
	ko := e.createKey(t, other, "mcp", hdr)

	var limited *http.Response
	for i := 0; i < 3*mcpBurst && limited == nil; i++ {
		resp, out := postMCPBody(t, url, k.Secret, mcpInitBody)
		switch resp.StatusCode {
		case 200:
		case 429:
			limited = resp
		default:
			t.Fatalf("request %d: status = %d (%s)", i, resp.StatusCode, out)
		}
	}
	if limited == nil {
		t.Fatalf("no 429 after %d requests", 3*mcpBurst)
	}
	if ra := limited.Header.Get("Retry-After"); ra == "" {
		t.Fatal("429 without Retry-After")
	}
	// Buckets are per project: another project is not affected.
	if resp, out := postMCPBody(t, url, ko.Secret, mcpInitBody); resp.StatusCode != 200 {
		t.Fatalf("other project: status = %d (%s)", resp.StatusCode, out)
	}
	// Ingest's limiter is a separate instance.
	if e.h.mcpLimiter == e.h.ingestSvc.RateLimiter() {
		t.Fatal("mcp shares the ingest limiter")
	}
}

func TestMCPHandlerPanicIsRecovered(t *testing.T) {
	t.Cleanup(func() { mcpTestRegister = nil })
	mcpTestRegister = func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{Name: "boom", InputSchema: &jsonschema.Schema{Type: "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var m map[string]int
			m["x"] = 1 // nil map write: runtime panic
			return nil, nil
		})
		s.AddPrompt(&mcp.Prompt{Name: "boom_prompt"}, func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			panic("prompt exploded")
		})
	}
	e, project, _, cs := mcpSession(t)

	out, isErr := callTool(t, cs, "boom", nil)
	if !isErr || !strings.Contains(out, "tool failed; try again") {
		t.Fatalf("panicking tool: isErr=%v out=%q", isErr, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "boom_prompt"}); err == nil ||
		!strings.Contains(err.Error(), "tool failed; try again") {
		t.Fatalf("panicking prompt: err = %v", err)
	}
	// The process and the session keep working.
	if out, isErr := callTool(t, cs, "list_issues", nil); isErr {
		t.Fatalf("list_issues after panic: %s", out)
	}
	if n, _, ok := e.mcpCalls(t, project, "boom"); n != 1 || ok {
		t.Fatalf("boom log = %d ok=%v, want 1 row ok=false", n, ok)
	}
	if n, _, ok := e.mcpCalls(t, project, "prompt:boom_prompt"); n != 1 || ok {
		t.Fatalf("boom_prompt log = %d ok=%v, want 1 row ok=false", n, ok)
	}
}
