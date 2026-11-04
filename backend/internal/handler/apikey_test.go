package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tiofani03/artracker/backend/internal/auth"
)

func (e *testEnv) addProject(t *testing.T, orgID int64, slug string) int64 {
	t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(),
		`INSERT INTO projects(org_id,name,slug,platform) VALUES ($1,$2,$2,'web') RETURNING id`, orgID, slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

type keyResp struct {
	Key struct {
		ID       int64  `json:"id"`
		KeyType  string `json:"key_type"`
		Prefix   string `json:"prefix"`
		IsActive bool   `json:"is_active"`
	} `json:"key"`
	Secret string `json:"secret"`
}

func (e *testEnv) createKey(t *testing.T, projectID int64, keyType string, hdr map[string]string) keyResp {
	t.Helper()
	resp := e.post(t, fmt.Sprintf("/api/v1/projects/%d/keys", projectID), fmt.Sprintf(`{"key_type":%q}`, keyType), hdr)
	if resp.StatusCode != 201 {
		t.Fatalf("create key = %d", resp.StatusCode)
	}
	var out keyResp
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func (e *testEnv) validate(t *testing.T, path, key string) *http.Response {
	t.Helper()
	h := map[string]string{}
	if key != "" {
		h["X-Artracker-Key"] = key
	}
	return e.do(t, "GET", path, nil, h)
}

// keyFixture: one org, one project, one admin.
func keyFixture(t *testing.T) (*testEnv, int64, int64, map[string]string) {
	e := newTestEnv(t)
	org := e.addOrg(t, "acme")
	admin, hdr := e.addUser(t, "admin")
	e.addMember(t, org, admin, "admin")
	return e, org, e.addProject(t, org, "web"), hdr
}

func TestCreateKeyReturnsPlaintextOnce(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	k := e.createKey(t, project, "public_client", hdr)
	if !strings.HasPrefix(k.Secret, "art_live_") || k.Key.KeyType != "public_client" || !k.Key.IsActive {
		t.Fatalf("created = %+v", k)
	}
	var hash, prefix string
	_ = e.pool.QueryRow(context.Background(), `SELECT key_hash, prefix FROM api_keys WHERE id=$1`, k.Key.ID).Scan(&hash, &prefix)
	if hash != auth.HashKey(k.Secret) || prefix == k.Secret || !strings.HasPrefix(k.Secret, prefix) {
		t.Fatalf("stored hash/prefix wrong: %q %q", hash, prefix)
	}

	resp := e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d/keys", project), nil, hdr)
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	if resp.StatusCode != 200 || strings.Contains(body, k.Secret) || strings.Contains(body, hash) || !strings.Contains(body, prefix) {
		t.Fatalf("list must show prefix only: %d %s", resp.StatusCode, body)
	}
}

func TestCreateKeyRejectsBadType(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	if r := e.post(t, fmt.Sprintf("/api/v1/projects/%d/keys", project), `{"key_type":"root"}`, hdr); r.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", r.StatusCode)
	}
}

func TestKeyRolePermissions(t *testing.T) {
	e, org, project, _ := keyFixture(t)
	for role, wantCreate := range map[string]int{"viewer": 403, "member": 403, "owner": 201} {
		uid, hdr := e.addUser(t, role)
		e.addMember(t, org, uid, role)
		path := fmt.Sprintf("/api/v1/projects/%d/keys", project)
		if r := e.post(t, path, `{"key_type":"public_client"}`, hdr); r.StatusCode != wantCreate {
			t.Errorf("%s create = %d, want %d", role, r.StatusCode, wantCreate)
		}
		if r := e.do(t, "GET", path, nil, hdr); r.StatusCode != 200 {
			t.Errorf("%s list = %d, want 200", role, r.StatusCode)
		}
	}
}

func TestValidateKey(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	k := e.createKey(t, project, "public_client", hdr)

	resp := e.validate(t, "/api/v1/sdk/validate", k.Secret)
	var out struct {
		ProjectID int64  `json:"project_id"`
		KeyType   string `json:"key_type"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 || out.ProjectID != project || out.KeyType != "public_client" {
		t.Fatalf("validate = %d %+v", resp.StatusCode, out)
	}
	if r := e.validate(t, "/api/v1/sdk/validate", "art_live_notarealkey"); r.StatusCode != 401 {
		t.Errorf("random key = %d, want 401", r.StatusCode)
	}
	if r := e.validate(t, "/api/v1/sdk/validate", ""); r.StatusCode != 401 {
		t.Errorf("no header = %d, want 401", r.StatusCode)
	}
}

func TestRevokedKeyRejected(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	k := e.createKey(t, project, "public_client", hdr)
	path := fmt.Sprintf("/api/v1/projects/%d/keys/%d/revoke", project, k.Key.ID)
	for i := 0; i < 2; i++ {
		if r := e.post(t, path, ``, hdr); r.StatusCode != 200 {
			t.Fatalf("revoke #%d = %d, want 200", i+1, r.StatusCode)
		}
	}
	if r := e.validate(t, "/api/v1/sdk/validate", k.Secret); r.StatusCode != 401 {
		t.Fatalf("revoked key = %d, want 401", r.StatusCode)
	}
}

func TestRotateKey(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	old := e.createKey(t, project, "secret_admin", hdr)
	path := fmt.Sprintf("/api/v1/projects/%d/keys/%d/rotate", project, old.Key.ID)

	resp := e.post(t, path, ``, hdr)
	if resp.StatusCode != 201 {
		t.Fatalf("rotate = %d, want 201", resp.StatusCode)
	}
	var fresh keyResp
	_ = json.NewDecoder(resp.Body).Decode(&fresh)
	if fresh.Key.KeyType != "secret_admin" || fresh.Secret == old.Secret || !strings.HasPrefix(fresh.Secret, "art_sec_") {
		t.Fatalf("rotated = %+v", fresh)
	}
	if r := e.validate(t, "/api/v1/sdk/validate", old.Secret); r.StatusCode != 401 {
		t.Errorf("old key = %d, want 401", r.StatusCode)
	}
	if r := e.validate(t, "/api/v1/sdk/validate", fresh.Secret); r.StatusCode != 200 {
		t.Errorf("new key = %d, want 200", r.StatusCode)
	}
	if r := e.post(t, path, ``, hdr); r.StatusCode != 409 {
		t.Errorf("rotating a revoked key = %d, want 409", r.StatusCode)
	}
}

func TestPublicKeyRejectedOnAdminEndpoint(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	pub := e.createKey(t, project, "public_client", hdr)
	sec := e.createKey(t, project, "secret_admin", hdr)
	if r := e.validate(t, "/api/v1/sdk/validate-admin", pub.Secret); r.StatusCode != 403 {
		t.Errorf("public key on admin endpoint = %d, want 403", r.StatusCode)
	}
	if r := e.validate(t, "/api/v1/sdk/validate-admin", sec.Secret); r.StatusCode != 200 {
		t.Errorf("secret key on admin endpoint = %d, want 200", r.StatusCode)
	}
	if r := e.validate(t, "/api/v1/sdk/validate-admin", "art_sec_bogus"); r.StatusCode != 401 {
		t.Errorf("bogus key = %d, want 401", r.StatusCode)
	}
}

func TestCrossTenantKeyAccess404(t *testing.T) {
	e, _, projectA, hdrA := keyFixture(t)
	orgB := e.addOrg(t, "orgb")
	ownerB, hdrB := e.addUser(t, "ownerb")
	e.addMember(t, orgB, ownerB, "owner")
	projectB := e.addProject(t, orgB, "bproj")
	keyB := e.createKey(t, projectB, "public_client", hdrB)

	// (a) admin of org A targets project B directly
	if r := e.post(t, fmt.Sprintf("/api/v1/projects/%d/keys/%d/revoke", projectB, keyB.Key.ID), ``, hdrA); r.StatusCode != 404 {
		t.Errorf("revoke via foreign project = %d, want 404", r.StatusCode)
	}
	// (b) admin of org A uses own project id with B's key id
	if r := e.post(t, fmt.Sprintf("/api/v1/projects/%d/keys/%d/revoke", projectA, keyB.Key.ID), ``, hdrA); r.StatusCode != 404 {
		t.Errorf("revoke foreign key via own project = %d, want 404", r.StatusCode)
	}
	if r := e.post(t, fmt.Sprintf("/api/v1/projects/%d/keys/%d/rotate", projectA, keyB.Key.ID), ``, hdrA); r.StatusCode != 404 {
		t.Errorf("rotate foreign key via own project = %d, want 404", r.StatusCode)
	}
	if r := e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d/keys", projectB), nil, hdrA); r.StatusCode != 404 {
		t.Errorf("list foreign keys = %d, want 404", r.StatusCode)
	}
	if r := e.validate(t, "/api/v1/sdk/validate", keyB.Secret); r.StatusCode != 200 {
		t.Errorf("B's key must stay active, got %d", r.StatusCode)
	}
}
