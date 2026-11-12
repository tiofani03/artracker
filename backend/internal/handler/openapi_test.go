package handler

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
)

func TestOpenAPISpecValid(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal(openAPISpecBytes, &parsed); err != nil {
		t.Fatalf("openAPISpecBytes is invalid JSON: %v", err)
	}

	paths, ok := parsed["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths object missing in openapi spec")
	}

	expectedPaths := []string{
		"/ingest",
		"/events",
		"/projects/{projectID}/analytics/catalog",
		"/projects/{projectID}/analytics/funnel",
		"/projects/{projectID}/analytics/retention",
		"/projects/{projectID}/analytics/funnels",
		"/replay/config",
		"/projects/{projectID}/replay-settings",
		"/projects/{projectID}/sessions/{sessionID}/chunks",
		"/projects/{projectID}/sessions/{sessionID}/perf",
		"/orgs/{orgID}/alerts/telegram-bot",
		"/projects/{projectID}/alerts/channels",
	}

	for _, p := range expectedPaths {
		if _, exists := paths[p]; !exists {
			t.Errorf("expected path %s missing in openapi spec", p)
		}
	}
}

func TestOpenAPIDocumentsScreenshotReplay(t *testing.T) {
	var parsed map[string]any
	if err := json.Unmarshal(openAPISpecBytes, &parsed); err != nil {
		t.Fatal(err)
	}
	get := func(v any, keys ...string) any {
		for _, k := range keys {
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[k]
		}
		return v
	}

	if get(parsed, "components", "schemas", "ReplayChunkURLRequest", "properties", "format") == nil {
		t.Error("ReplayChunkURLRequest should document the optional format field")
	}
	if get(parsed, "paths", "/replay/chunk-url", "post", "responses", "403") == nil {
		t.Error("chunk-url should document 403 screenshot_disabled")
	}
	if get(parsed, "paths", "/projects/{projectID}/alerts/channels", "post", "responses", "201") == nil {
		t.Error("POST alerts/channels should be documented")
	}
	for _, m := range []string{"get", "put", "patch", "delete"} {
		if get(parsed, "paths", "/orgs/{orgID}/alerts/telegram-bot", m) == nil {
			t.Errorf("%s /orgs/{orgID}/alerts/telegram-bot should be documented", m)
		}
	}
	if get(parsed, "components", "schemas", "ReplayConfigResponse", "properties", "screenshot_enabled") == nil {
		t.Error("ReplayConfigResponse schema missing")
	}
}

// openAPIUndocumented lists /api/v1 routes that are deliberately absent from the public OpenAPI spec:
// dashboard-internal plumbing (login, org and key management, mock data) and signed-URL endpoints that
// SDKs never call directly. Every other route must appear in the spec, so a new route either gets a
// spec entry or an entry here with a reason. Keys are "METHOD /path/{param}" as in the spec.
var openAPIUndocumented = map[string]string{
	"GET /mcp":                                       "405 by design (stateless MCP)",
	"DELETE /mcp":                                    "405 by design (stateless MCP)",
	"GET /artworks":                                  "legacy starter example",
	"POST /artworks":                                 "legacy starter example",
	"GET /artworks/{id}":                             "legacy starter example",
	"PATCH /artworks/{id}":                           "legacy starter example",
	"DELETE /artworks/{id}":                          "legacy starter example",
	"PUT /replay/upload-chunk":                       "signed URL returned by chunk-url, not called directly",
	"GET /replay/download-chunk":                     "signed URL returned by the sessions API, not called directly",
	"GET /auth/{provider}/login":                     "browser login flow",
	"GET /auth/{provider}/callback":                  "browser login flow",
	"POST /auth/refresh":                             "browser session cookie flow",
	"POST /auth/logout":                              "browser session cookie flow",
	"GET /auth/me":                                   "browser session",
	"POST /orgs":                                     "dashboard organization management",
	"GET /orgs":                                      "dashboard organization management",
	"GET /orgs/{orgID}/projects":                     "dashboard organization management",
	"POST /orgs/{orgID}/projects":                    "dashboard organization management",
	"GET /orgs/{orgID}/members":                      "dashboard organization management",
	"POST /orgs/{orgID}/invites":                     "dashboard organization management",
	"DELETE /orgs/{orgID}/invites/{inviteID}":        "dashboard organization management",
	"PATCH /orgs/{orgID}/members/{userID}":           "dashboard organization management",
	"DELETE /orgs/{orgID}/members/{userID}":          "dashboard organization management",
	"GET /invites/{token}":                           "dashboard invitation flow",
	"POST /invites/{token}/accept":                   "dashboard invitation flow",
	"GET /projects/{projectID}":                      "dashboard project lookup",
	"GET /projects/{projectID}/keys":                 "dashboard API key management",
	"POST /projects/{projectID}/keys":                "dashboard API key management",
	"POST /projects/{projectID}/keys/{keyID}/revoke": "dashboard API key management",
	"POST /projects/{projectID}/keys/{keyID}/rotate": "dashboard API key management",
	"POST /projects/{projectID}/seed-mock":           "development demo data",
	"GET /openapi.json":                              "the spec itself",
}

// fiberParam rewrites Fiber's ":param" into OpenAPI's "{param}" to turn "/projects/:projectID/keys" into "/projects/{projectID}/keys".
var fiberParam = regexp.MustCompile(`:([A-Za-z0-9_]+)`)

func registeredAPIRoutes(t *testing.T) map[string]bool {
	t.Helper()
	// A lazy pool (no connection is made) so Routes registers the full table, not the SKIP_DB subset.
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cfg := config.Config{Env: "development", PublicURL: "http://localhost:5173", JWTSecret: testSecret}
	h := New(pool, cfg, auth.NewIssuer(testSecret, time.Minute), map[string]OAuthProvider{})
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	h.Routes(app.Group("/api/v1"))

	routes := map[string]bool{}
	for _, r := range app.GetRoutes(true) {
		if r.Method == "HEAD" || !strings.HasPrefix(r.Path, "/api/v1/") {
			continue
		}
		path := fiberParam.ReplaceAllString(strings.TrimPrefix(r.Path, "/api/v1"), "{$1}")
		routes[r.Method+" "+path] = true
	}
	return routes
}

func TestOpenAPICoversRoutes(t *testing.T) {
	var parsed struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(openAPISpecBytes, &parsed); err != nil {
		t.Fatal(err)
	}
	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}

	routes := registeredAPIRoutes(t)
	if len(routes) < 50 {
		t.Fatalf("only %d routes registered; the route table was not built", len(routes))
	}

	var missing, stale, phantom []string
	for key := range routes {
		method, path, _ := strings.Cut(key, " ")
		_, inSpec := parsed.Paths[path][strings.ToLower(method)]
		_, allowed := openAPIUndocumented[key]
		if !inSpec && !allowed {
			missing = append(missing, key)
		}
		if inSpec && allowed {
			stale = append(stale, key+" (documented, remove it from openAPIUndocumented)")
		}
	}
	for key := range openAPIUndocumented {
		if !routes[key] {
			stale = append(stale, key+" (no such route, remove it from openAPIUndocumented)")
		}
	}
	for path, ops := range parsed.Paths {
		for m := range ops {
			if methods[m] && !routes[strings.ToUpper(m)+" "+path] {
				phantom = append(phantom, strings.ToUpper(m)+" "+path)
			}
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	sort.Strings(phantom)
	if len(missing) > 0 {
		t.Errorf("routes missing from the OpenAPI spec (document them in openapi.go or allowlist them with a reason):\n  %s", strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("stale openAPIUndocumented entries:\n  %s", strings.Join(stale, "\n  "))
	}
	if len(phantom) > 0 {
		t.Errorf("spec documents operations that are not routes:\n  %s", strings.Join(phantom, "\n  "))
	}
}
