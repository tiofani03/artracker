package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func (e *testEnv) keyCreatedBy(t *testing.T, keyID int64) *int64 {
	t.Helper()
	var by *int64
	if err := e.pool.QueryRow(context.Background(), `SELECT created_by FROM api_keys WHERE id = $1`, keyID).Scan(&by); err != nil {
		t.Fatal(err)
	}
	return by
}

func (e *testEnv) keyActive(t *testing.T, keyID int64) bool {
	t.Helper()
	var active, revoked bool
	if err := e.pool.QueryRow(context.Background(),
		`SELECT is_active, revoked_at IS NOT NULL FROM api_keys WHERE id = $1`, keyID).Scan(&active, &revoked); err != nil {
		t.Fatal(err)
	}
	if active == revoked {
		t.Fatalf("key %d: is_active=%v but revoked_at set=%v", keyID, active, revoked)
	}
	return active
}

func TestKeyCreatorAndMCPKeyRevocationOnMembershipChange(t *testing.T) {
	e, org, project, adminHdr := keyFixture(t)
	ownerID, ownerHdr := e.addUser(t, "owner")
	e.addMember(t, org, ownerID, "owner")
	m1, m1Hdr := e.addUser(t, "mem1")
	e.addMember(t, org, m1, "member")
	m2, m2Hdr := e.addUser(t, "mem2")
	e.addMember(t, org, m2, "member")
	admin2, admin2Hdr := e.addUser(t, "admin2")
	e.addMember(t, org, admin2, "admin")
	// m1 also belongs to another org: that org's key must survive.
	org2 := e.addOrg(t, "beta")
	e.addMember(t, org2, m1, "member")
	project2 := e.addProject(t, org2, "beta-web")
	url := e.serveMCP(t)

	k1 := e.createKey(t, project, "mcp", m1Hdr)
	k1other := e.createKey(t, project2, "mcp", m1Hdr)
	k2 := e.createKey(t, project, "mcp", m2Hdr)
	pub := e.createKey(t, project, "public_client", admin2Hdr)
	sec := e.createKey(t, project, "secret_admin", admin2Hdr)

	for _, c := range []struct {
		key  int64
		want int64
	}{{k1.Key.ID, m1}, {k2.Key.ID, m2}, {pub.Key.ID, admin2}, {sec.Key.ID, admin2}} {
		if by := e.keyCreatedBy(t, c.key); by == nil || *by != c.want {
			t.Fatalf("key %d created_by = %v, want %d", c.key, by, c.want)
		}
	}
	// Rotation records the rotating user on the new key.
	k3 := e.createKey(t, project, "mcp", m2Hdr)
	resp := e.post(t, fmt.Sprintf("/api/v1/projects/%d/keys/%d/rotate", project, k3.Key.ID), "", adminHdr)
	if resp.StatusCode != 201 {
		t.Fatalf("rotate = %d", resp.StatusCode)
	}
	var rotated keyResp
	_ = json.NewDecoder(resp.Body).Decode(&rotated)
	var adminID int64
	_ = e.pool.QueryRow(context.Background(), `SELECT id FROM users WHERE name = 'admin'`).Scan(&adminID)
	if by := e.keyCreatedBy(t, rotated.Key.ID); by == nil || *by != adminID {
		t.Fatalf("rotated key created_by = %v, want %d", by, adminID)
	}

	bearer := func(k keyResp) map[string]string { return map[string]string{"Authorization": "Bearer " + k.Secret} }
	if r, out := rawMCP(t, "POST", url, bearer(k1)); r.StatusCode != 200 {
		t.Fatalf("k1 before removal = %d (%s)", r.StatusCode, out)
	}

	// Removing m1 revokes m1's mcp keys in this org only.
	if r := e.do(t, "DELETE", fmt.Sprintf("/api/v1/orgs/%d/members/%d", org, m1), nil, adminHdr); r.StatusCode != 204 {
		t.Fatalf("remove member = %d", r.StatusCode)
	}
	if e.keyActive(t, k1.Key.ID) {
		t.Fatal("removed member's mcp key is still active")
	}
	if r, out := rawMCP(t, "POST", url, bearer(k1)); r.StatusCode != 401 {
		t.Fatalf("k1 after removal = %d (%s), want 401", r.StatusCode, out)
	}
	if !e.keyActive(t, k1other.Key.ID) {
		t.Fatal("key in another org was revoked")
	}
	if !e.keyActive(t, k2.Key.ID) {
		t.Fatal("another member's key was revoked")
	}
	if r, out := rawMCP(t, "POST", url, bearer(k2)); r.StatusCode != 200 {
		t.Fatalf("k2 = %d (%s)", r.StatusCode, out)
	}

	// Only mcp keys are auto-revoked: an admin's public/secret keys outlive them.
	if r := e.do(t, "DELETE", fmt.Sprintf("/api/v1/orgs/%d/members/%d", org, admin2), nil, adminHdr); r.StatusCode != 204 {
		t.Fatalf("remove admin2 = %d", r.StatusCode)
	}
	if !e.keyActive(t, pub.Key.ID) || !e.keyActive(t, sec.Key.ID) {
		t.Fatal("public/secret keys of a removed admin were revoked")
	}

	// Demotion to viewer revokes the member's mcp keys (a viewer cannot mint them).
	if r := e.do(t, "PATCH", fmt.Sprintf("/api/v1/orgs/%d/members/%d", org, m2), strings.NewReader(`{"role":"admin"}`), ownerHdr); r.StatusCode != 200 {
		t.Fatalf("promote = %d", r.StatusCode)
	}
	if !e.keyActive(t, k2.Key.ID) {
		t.Fatal("promotion revoked the key")
	}
	if r := e.do(t, "PATCH", fmt.Sprintf("/api/v1/orgs/%d/members/%d", org, m2), strings.NewReader(`{"role":"viewer"}`), ownerHdr); r.StatusCode != 200 {
		t.Fatalf("demote = %d", r.StatusCode)
	}
	if e.keyActive(t, k2.Key.ID) {
		t.Fatal("viewer still holds an active mcp key")
	}
	if r, out := rawMCP(t, "POST", url, bearer(k2)); r.StatusCode != 401 {
		t.Fatalf("k2 after demotion = %d (%s), want 401", r.StatusCode, out)
	}

	// The key list names the creator; legacy keys have none.
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO api_keys(project_id, key_type, key_hash, prefix) VALUES ($1, 'public_client', 'legacyhash', 'art_pub_legacy')`, project); err != nil {
		t.Fatal(err)
	}
	r := e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d/keys", project), nil, adminHdr)
	if r.StatusCode != 200 {
		t.Fatalf("list keys = %d", r.StatusCode)
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(r.Body).Decode(&list)
	byID := map[int64]map[string]any{}
	for _, it := range list.Items {
		byID[int64(it["id"].(float64))] = it
	}
	if got := byID[k1.Key.ID]; got["created_by_name"] != "mem1" || got["created_by_email"] != "mem1@example.com" {
		t.Fatalf("k1 creator = %v", got)
	}
	for _, it := range list.Items {
		if it["prefix"] == "art_pub_legacy" && (it["created_by_name"] != nil || it["created_by_email"] != nil) {
			t.Fatalf("legacy key creator = %v", it)
		}
	}
}
