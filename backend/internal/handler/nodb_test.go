package handler

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
)

// With SKIP_DB the pool is nil: auth/tenant routes must answer 503, never panic,
// while the in-memory artwork routes keep working.
func TestAuthRoutesReturn503WithoutDatabase(t *testing.T) {
	cfg := config.Config{Env: "development", PublicURL: "http://localhost:5173", JWTSecret: testSecret}
	h := New(nil, cfg, auth.NewIssuer(testSecret, time.Minute), map[string]OAuthProvider{})
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	h.Routes(app.Group("/api/v1"))

	tok, _, _ := h.issuer.IssueAccess(1)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/auth/google/login"},
		{"POST", "/api/v1/auth/refresh"},
		{"GET", "/api/v1/auth/me"},
		{"GET", "/api/v1/orgs"},
		{"POST", "/api/v1/orgs/1/projects"},
		{"GET", "/api/v1/projects/1/keys"},
		{"GET", "/api/v1/sdk/validate"},
	} {
		req := httptest.NewRequest(c.method, c.path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		if resp.StatusCode != 503 {
			t.Errorf("%s %s = %d, want 503", c.method, c.path, resp.StatusCode)
		}
	}

	resp, _ := app.Test(httptest.NewRequest("GET", "/api/v1/artworks", nil), -1)
	if resp.StatusCode != 200 {
		t.Errorf("in-memory artworks = %d, want 200", resp.StatusCode)
	}
}
