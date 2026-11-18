package handler

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const testMapping = "com.app.Main -> a:\n    void run() -> b\n"

func (e *testEnv) uploadWithKey(t *testing.T, path, key, body string, extra map[string]string) *http.Response {
	t.Helper()
	h := map[string]string{}
	if key != "" {
		h["X-Artracker-Key"] = key
	}
	for k, v := range extra {
		h[k] = v
	}
	return e.do(t, "POST", path, strings.NewReader(body), h)
}

func TestSecretKeyUploadAuth(t *testing.T) {
	e, org, project, hdr := keyFixture(t)
	other := e.addProject(t, org, "other")
	sec := e.createKey(t, project, "secret_admin", hdr).Secret
	pub := e.createKey(t, project, "public_client", hdr).Secret
	mcp := e.createKey(t, project, "mcp", hdr).Secret

	path := "/api/v1/sdk/releases/1.4.0/proguard"
	cases := []struct {
		name string
		key  string
		want int
	}{
		{"no key", "", 401},
		{"unknown key", "art_sec_nope", 401},
		{"mcp key", mcp, 401},
		{"public key", pub, 403},
		{"secret key", sec, 201},
	}
	for _, c := range cases {
		if got := e.uploadWithKey(t, path, c.key, testMapping, nil).StatusCode; got != c.want {
			t.Errorf("%s: status = %d, want %d", c.name, got, c.want)
		}
	}

	q := fmt.Sprintf(`SELECT count(*) FROM project_release_artifacts WHERE project_id=%d AND version='1.4.0'`, project)
	if n := e.count(t, q); n != 1 {
		t.Errorf("artifacts stored for key's project = %d, want 1", n)
	}
	q = fmt.Sprintf(`SELECT count(*) FROM project_release_artifacts WHERE project_id=%d`, other)
	if n := e.count(t, q); n != 0 {
		t.Errorf("artifacts leaked into another project = %d, want 0", n)
	}
}

func TestSecretKeyUploadRevokedKey(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	k := e.createKey(t, project, "secret_admin", hdr)
	path := "/api/v1/sdk/releases/1.0.0/proguard"
	if got := e.uploadWithKey(t, path, k.Secret, testMapping, nil).StatusCode; got != 201 {
		t.Fatalf("before revoke = %d, want 201", got)
	}
	if got := e.post(t, fmt.Sprintf("/api/v1/projects/%d/keys/%d/revoke", project, k.Key.ID), "", hdr).StatusCode; got != 200 {
		t.Fatalf("revoke = %d", got)
	}
	if got := e.uploadWithKey(t, path, k.Secret, testMapping, nil).StatusCode; got != 401 {
		t.Fatalf("after revoke = %d, want 401", got)
	}
}

func TestSecretKeyUploadEncodedVersion(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	sec := e.createKey(t, project, "secret_admin", hdr).Secret
	if got := e.uploadWithKey(t, "/api/v1/sdk/releases/1.4.0%2Bbuild.7/proguard", sec, testMapping, nil).StatusCode; got != 201 {
		t.Fatalf("status = %d, want 201", got)
	}
	q := fmt.Sprintf(`SELECT count(*) FROM project_release_artifacts WHERE project_id=%d AND version='1.4.0+build.7'`, project)
	if n := e.count(t, q); n != 1 {
		t.Fatalf("rows with decoded version = %d, want 1", n)
	}
}

func TestSecretKeyUploadDSYMAndSourceMap(t *testing.T) {
	e, _, project, hdr := keyFixture(t)
	sec := e.createKey(t, project, "secret_admin", hdr).Secret

	resp := e.uploadWithKey(t, "/api/v1/sdk/releases/2.0.0/dsyms?file=App", sec, "not-a-macho-but-raw-bytes", nil)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("dsym = %d %s", resp.StatusCode, b)
	}
	sm := `{"version":3,"file":"app.js","sources":["a.ts"],"names":[],"mappings":"AAAA"}`
	resp = e.uploadWithKey(t, "/api/v1/sdk/releases/2.0.0%2B1/sourcemaps", sec, sm, nil)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("sourcemap = %d %s", resp.StatusCode, b)
	}
	q := fmt.Sprintf(`SELECT count(*) FROM source_maps WHERE project_id=%d AND release_version='2.0.0+1'`, project)
	if n := e.count(t, q); n != 1 {
		t.Fatalf("source map stored under decoded version = %d rows, want 1", n)
	}
}
