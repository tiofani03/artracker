package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpInitBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

// serveMCP starts the test app on a real loopback listener so the SDK's
// HTTP client can reach /api/v1/mcp. Returns the endpoint URL.
func (e *testEnv) serveMCP(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = e.app.Listener(ln) }()
	t.Cleanup(func() { _ = e.app.ShutdownWithTimeout(2 * time.Second) })
	return "http://" + ln.Addr().String() + "/api/v1/mcp"
}

// rawMCP posts an initialize request with the given headers.
func rawMCP(t *testing.T, method, url string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(mcpInitBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp, b.String()
}

type headerTransport struct {
	key  string
	base http.RoundTripper
}

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+h.key)
	return h.base.RoundTrip(r)
}

func connectMCP(t *testing.T, url, key string) (*mcp.ClientSession, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	tr := &mcp.StreamableClientTransport{
		Endpoint:             url,
		HTTPClient:           &http.Client{Transport: headerTransport{key: key, base: http.DefaultTransport}},
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}
	cs, err := client.Connect(ctx, tr, nil)
	if err == nil {
		t.Cleanup(func() { _ = cs.Close() })
	}
	return cs, err
}

func TestMCPRejectsMissingAndWrongKeys(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	url := e.serveMCP(t)

	assert401 := func(name string, h map[string]string) {
		t.Helper()
		resp, body := rawMCP(t, "POST", url, h)
		if resp.StatusCode != 401 {
			t.Fatalf("%s: status = %d (%s), want 401", name, resp.StatusCode, body)
		}
		var out APIError
		if err := json.Unmarshal([]byte(body), &out); err != nil || out.Error != "invalid mcp key" {
			t.Fatalf("%s: body = %s, want JSON invalid mcp key", name, body)
		}
	}

	assert401("no key", nil)
	assert401("garbage key", map[string]string{"Authorization": "Bearer art_mcp_nope"})
	pub := e.createKey(t, project, "public_client", hdr)
	assert401("public_client", map[string]string{"Authorization": "Bearer " + pub.Secret})
	sec := e.createKey(t, project, "secret_admin", hdr)
	assert401("secret_admin", map[string]string{"X-Artracker-Key": sec.Secret})

	k := e.createKey(t, project, "mcp", hdr)
	if resp, body := rawMCP(t, "POST", url, map[string]string{"Authorization": "Bearer " + k.Secret}); resp.StatusCode != 200 {
		t.Fatalf("active mcp key: status = %d (%s), want 200", resp.StatusCode, body)
	}
	if resp, body := rawMCP(t, "POST", url, map[string]string{"X-Artracker-Key": k.Secret}); resp.StatusCode != 200 {
		t.Fatalf("X-Artracker-Key header: status = %d (%s), want 200", resp.StatusCode, body)
	}

	if r := e.post(t, fmt.Sprintf("/api/v1/projects/%d/keys/%d/revoke", project, k.Key.ID), "", hdr); r.StatusCode != 200 {
		t.Fatalf("revoke = %d", r.StatusCode)
	}
	assert401("revoked mcp", map[string]string{"Authorization": "Bearer " + k.Secret})
	if _, err := connectMCP(t, url, k.Secret); err == nil {
		t.Fatal("SDK client connected with a revoked key")
	}
}

func TestMCPGetAndDeleteNotAllowed(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	url := e.serveMCP(t)
	k := e.createKey(t, project, "mcp", hdr)
	for _, m := range []string{"GET", "DELETE"} {
		resp, body := rawMCP(t, m, url, map[string]string{"Authorization": "Bearer " + k.Secret})
		if resp.StatusCode != 405 || resp.Header.Get("Allow") != "POST" {
			t.Fatalf("%s: status = %d allow=%q (%s), want 405 Allow: POST", m, resp.StatusCode, resp.Header.Get("Allow"), body)
		}
	}
}

func TestMCPInitializeNamesProject(t *testing.T) {
	e, org, project, hdr := keyFixture(t)
	other := e.addProject(t, org, "other-app")
	url := e.serveMCP(t)

	for slug, pid := range map[string]int64{"web": project, "other-app": other} {
		k := e.createKey(t, pid, "mcp", hdr)
		cs, err := connectMCP(t, url, k.Secret)
		if err != nil {
			t.Fatalf("%s: connect: %v", slug, err)
		}
		init := cs.InitializeResult()
		if init == nil || init.ServerInfo == nil || init.ServerInfo.Name != "artracker-"+slug {
			t.Fatalf("%s: server info = %+v", slug, init)
		}
		if !strings.Contains(init.Instructions, slug) || !strings.Contains(init.Instructions, "web") {
			t.Fatalf("%s: instructions = %q", slug, init.Instructions)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		tools, err := cs.ListTools(ctx, nil)
		cancel()
		if err != nil || tools == nil || tools.Tools == nil {
			t.Fatalf("%s: tools/list = %+v, %v", slug, tools, err)
		}
	}
}

func TestLogMCPCall(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	k := e.createKey(t, project, "mcp", hdr)
	mc := mcpContext{KeyID: k.Key.ID, ProjectID: project, ProjectName: "web", Slug: "web", Platform: "web"}
	issue := int64(42)
	ctx := context.Background()
	e.h.logMCPCall(ctx, mc, "update_issue:resolved", &issue, true)
	e.h.logMCPCall(ctx, mc, "list_issues", nil, false)

	var n int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM mcp_tool_calls WHERE project_id=$1 AND api_key_id=$2 AND tool='update_issue:resolved' AND issue_id=42 AND ok`,
		project, k.Key.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("update_issue row count = %d, want 1", n)
	}
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM mcp_tool_calls WHERE tool='list_issues' AND issue_id IS NULL AND NOT ok`).Scan(&n)
	if n != 1 {
		t.Fatalf("list_issues row count = %d, want 1", n)
	}

	// A failing insert (unknown project violates the FK) must not panic or block.
	e.h.logMCPCall(ctx, mcpContext{KeyID: k.Key.ID, ProjectID: 999999}, "list_issues", nil, true)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	e.h.logMCPCall(cancelled, mc, "list_issues", nil, true)
}
