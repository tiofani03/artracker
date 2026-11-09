package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
)

func TestArtifactsValidationErrors(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	issuer := auth.NewIssuer("test-jwt-secret-min-32-chars-long!", time.Hour)
	h := New(nil, config.Config{}, issuer, map[string]OAuthProvider{})

	// Dummy route without projectID in locals (unauthorized)
	app.Post("/no-auth", h.UploadDSYM)
	req := httptest.NewRequest("POST", "/no-auth", bytes.NewReader([]byte("test")))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", resp.StatusCode)
	}

	// Middleware injects projectID
	authed := app.Group("/api", func(c *fiber.Ctx) error {
		c.Locals("projectID", int64(42))
		return c.Next()
	})
	authed.Post("/projects/:projectID/releases/:version/dsyms", h.UploadDSYM)
	authed.Post("/projects/:projectID/releases/:version/proguard", h.UploadProguard)

	// 1. Missing version
	req = httptest.NewRequest("POST", "/api/projects/42/releases/%20/dsyms", bytes.NewReader([]byte("dummy")))
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for empty version, got %d", resp.StatusCode)
	}

	// 2. Empty body dSYM
	req = httptest.NewRequest("POST", "/api/projects/42/releases/1.0.0/dsyms", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for empty dsym body, got %d", resp.StatusCode)
	}

	// 3. Empty body ProGuard
	req = httptest.NewRequest("POST", "/api/projects/42/releases/1.0.0/proguard", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for empty proguard body, got %d", resp.StatusCode)
	}

	// 4. Corrupted dSYM zip
	corruptedZip := []byte{'P', 'K', 0x03, 0x04, 0x00, 0x00}
	req = httptest.NewRequest("POST", "/api/projects/42/releases/1.0.0/dsyms", bytes.NewReader(corruptedZip))
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for corrupted zip, got %d", resp.StatusCode)
	}
}

func TestArtifactsDBRoundtrip(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	var orgID, projectID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO organizations(name, slug) VALUES ('Mobile Org', 'mobile-org') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects(org_id, name, slug, platform) VALUES ($1, 'Mobile App', 'mobile-app', 'android') RETURNING id`, orgID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}

	issuer := auth.NewIssuer("test-jwt-secret-min-32-chars-long!", time.Hour)
	h := New(pool, config.Config{}, issuer, map[string]OAuthProvider{})
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	api := app.Group("/api", func(c *fiber.Ctx) error {
		c.Locals("projectID", projectID)
		return c.Next()
	})
	api.Post("/projects/:projectID/releases/:version/dsyms", h.UploadDSYM)
	api.Post("/projects/:projectID/releases/:version/proguard", h.UploadProguard)
	api.Get("/projects/:projectID/releases/:version/artifacts", h.ListReleaseArtifacts)

	// 1. Upload Proguard Mapping
	mappingBody := `# R8 mapping
com.example.MainActivity -> a.b.c:
    void onCreate(android.os.Bundle) -> a
`
	req := httptest.NewRequest("POST", "/api/projects/1/releases/2.1.0/proguard", bytes.NewReader([]byte(mappingBody)))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for proguard upload, got %d", resp.StatusCode)
	}

	// 2. Upload dSYM Zip
	zipBuf := new(bytes.Buffer)
	zw := zip.NewWriter(zipBuf)
	f, err := zw.Create("App.app.dSYM/Contents/Resources/DWARF/App")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("mock mach-o dwarf content"))
	zw.Close()

	req = httptest.NewRequest("POST", "/api/projects/1/releases/2.1.0/dsyms?file=App.dSYM.zip", bytes.NewReader(zipBuf.Bytes()))
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for dsym upload, got %d", resp.StatusCode)
	}

	// 3. List Artifacts
	req = httptest.NewRequest("GET", "/api/projects/1/releases/2.1.0/artifacts", nil)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for list artifacts, got %d", resp.StatusCode)
	}

	var listResult struct {
		Artifacts []ReleaseArtifactItem `json:"artifacts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResult); err != nil {
		t.Fatal(err)
	}

	if len(listResult.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(listResult.Artifacts))
	}
}
