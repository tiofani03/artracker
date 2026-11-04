package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// addUser inserts a user and returns its id and a valid access token.
func (e *testEnv) addUser(t *testing.T, name string) (int64, map[string]string) {
	t.Helper()
	var id int64
	err := e.pool.QueryRow(context.Background(),
		`INSERT INTO users(email, name, sso_provider, sso_id) VALUES ($1,$2,'google',$3) RETURNING id`,
		name+"@example.com", name, name).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	tok, _, _ := e.h.issuer.IssueAccess(id)
	return id, map[string]string{"Authorization": "Bearer " + tok, "Content-Type": "application/json"}
}

func (e *testEnv) addOrg(t *testing.T, slug string) int64 {
	t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO organizations(name, slug) VALUES ($1,$1) RETURNING id`, slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *testEnv) addMember(t *testing.T, orgID, userID int64, role string) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO organization_members(org_id,user_id,role) VALUES ($1,$2,$3)`, orgID, userID, role); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) post(t *testing.T, path, body string, h map[string]string) *http.Response {
	t.Helper()
	return e.do(t, "POST", path, strings.NewReader(body), h)
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My Org!":     "my-org",
		"  --A__B ":   "a-b",
		"日本":          "",
		"Acme  Corp.": "acme-corp",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
	if got := slugify(strings.Repeat("a", 200)); len(got) > 48 {
		t.Errorf("slug length = %d, want <= 48", len(got))
	}
}

func TestCreateOrgMakesCallerOwner(t *testing.T) {
	e := newTestEnv(t)
	_, hdr := e.addUser(t, "ada")
	resp := e.post(t, "/api/v1/orgs", `{"name":"Acme Inc"}`, hdr)
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Slug string `json:"slug"`
		Role string `json:"role"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Slug != "acme-inc" || out.Role != "owner" {
		t.Fatalf("org = %+v", out)
	}
	if n := e.count(t, `SELECT count(*) FROM organization_members WHERE role='owner'`); n != 1 {
		t.Fatalf("owner rows = %d", n)
	}
	list := e.do(t, "GET", "/api/v1/orgs", nil, hdr)
	var items struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(list.Body).Decode(&items)
	if len(items.Items) != 1 {
		t.Fatalf("list = %+v", items)
	}
}

func TestOrgRequiresAuth(t *testing.T) {
	e := newTestEnv(t)
	if resp := e.post(t, "/api/v1/orgs", `{"name":"X"}`, nil); resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestCreateOrgDuplicateNameGetsSuffixedSlug(t *testing.T) {
	e := newTestEnv(t)
	_, hdr := e.addUser(t, "ada")
	_, hdr2 := e.addUser(t, "bob")
	want := []string{"acme", "acme-2", "acme-3"}
	for i, h := range []map[string]string{hdr, hdr2, hdr} {
		resp := e.post(t, "/api/v1/orgs", `{"name":"ACME!"}`, h)
		var out struct {
			Slug string `json:"slug"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != 201 || out.Slug != want[i] {
			t.Fatalf("create #%d = %d slug %q, want 201 %q", i+1, resp.StatusCode, out.Slug, want[i])
		}
	}
}

func TestCreateOrgNonLatinNameGetsFallbackSlug(t *testing.T) {
	e := newTestEnv(t)
	_, hdr := e.addUser(t, "ada")
	resp := e.post(t, "/api/v1/orgs", `{"name":"日本"}`, hdr)
	var out struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 201 || out.Name != "日本" || !strings.HasPrefix(out.Slug, "org-") || len(out.Slug) != len("org-")+6 {
		t.Fatalf("create = %d %+v", resp.StatusCode, out)
	}
}

func TestCreateOrgBlankName400(t *testing.T) {
	e := newTestEnv(t)
	_, hdr := e.addUser(t, "ada")
	for _, body := range []string{`{"name":"   "}`, `{}`, `not json`} {
		if resp := e.post(t, "/api/v1/orgs", body, hdr); resp.StatusCode != 400 {
			t.Errorf("body %q = %d, want 400", body, resp.StatusCode)
		}
	}
}

func TestRoleMatrixOnProjects(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "acme")
	cases := []struct {
		role       string
		wantCreate int
	}{{"owner", 201}, {"admin", 201}, {"member", 403}, {"viewer", 403}}
	for _, c := range cases {
		uid, hdr := e.addUser(t, c.role)
		e.addMember(t, org, uid, c.role)
		body := fmt.Sprintf(`{"name":"proj-%s","platform":"web"}`, c.role)
		if resp := e.post(t, fmt.Sprintf("/api/v1/orgs/%d/projects", org), body, hdr); resp.StatusCode != c.wantCreate {
			t.Errorf("%s create = %d, want %d", c.role, resp.StatusCode, c.wantCreate)
		}
		if resp := e.do(t, "GET", fmt.Sprintf("/api/v1/orgs/%d/projects", org), nil, hdr); resp.StatusCode != 200 {
			t.Errorf("%s list = %d, want 200", c.role, resp.StatusCode)
		}
	}
}

func TestNonMemberGets404(t *testing.T) {
	e := newTestEnv(t)
	orgB := e.addOrg(t, "orgb")
	owner, ownerHdr := e.addUser(t, "owner")
	e.addMember(t, orgB, owner, "owner")
	resp := e.post(t, fmt.Sprintf("/api/v1/orgs/%d/projects", orgB), `{"name":"p","platform":"ios"}`, ownerHdr)
	var proj struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&proj)
	if proj.ID == 0 {
		t.Fatal("setup: project not created")
	}

	outsider, outHdr := e.addUser(t, "outsider")
	orgA := e.addOrg(t, "orga")
	e.addMember(t, orgA, outsider, "owner")

	if r := e.do(t, "GET", fmt.Sprintf("/api/v1/orgs/%d/projects", orgB), nil, outHdr); r.StatusCode != 404 {
		t.Errorf("list other org = %d, want 404", r.StatusCode)
	}
	if r := e.post(t, fmt.Sprintf("/api/v1/orgs/%d/projects", orgB), `{"name":"x","platform":"web"}`, outHdr); r.StatusCode != 404 {
		t.Errorf("create in other org = %d, want 404", r.StatusCode)
	}
	if r := e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d", proj.ID), nil, outHdr); r.StatusCode != 404 {
		t.Errorf("get other project = %d, want 404", r.StatusCode)
	}
	if r := e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d", proj.ID), nil, ownerHdr); r.StatusCode != 200 {
		t.Errorf("owner get project = %d, want 200", r.StatusCode)
	}
}

func TestCreateProjectValidation(t *testing.T) {
	e := newTestEnv(t)
	org := e.addOrg(t, "acme")
	uid, hdr := e.addUser(t, "ada")
	e.addMember(t, org, uid, "admin")
	path := fmt.Sprintf("/api/v1/orgs/%d/projects", org)

	if r := e.post(t, path, `{"name":"p","platform":"windows"}`, hdr); r.StatusCode != 400 {
		t.Errorf("bad platform = %d, want 400", r.StatusCode)
	}
	if r := e.post(t, path, `{"name":"   ","platform":"web"}`, hdr); r.StatusCode != 400 {
		t.Errorf("blank name = %d, want 400", r.StatusCode)
	}
	if r := e.post(t, path, `{"name":"日本","platform":"web"}`, hdr); r.StatusCode != 201 {
		t.Errorf("non-latin name = %d, want 201 with fallback slug", r.StatusCode)
	}
	if r := e.post(t, path, `{"name":"Web App","platform":"web"}`, hdr); r.StatusCode != 201 {
		t.Errorf("first = %d, want 201", r.StatusCode)
	}
	if r := e.post(t, path, `{"name":"web app","platform":"ios"}`, hdr); r.StatusCode != 409 {
		t.Errorf("duplicate slug same org = %d, want 409", r.StatusCode)
	}

	other := e.addOrg(t, "other")
	e.addMember(t, other, uid, "admin")
	if r := e.post(t, fmt.Sprintf("/api/v1/orgs/%d/projects", other), `{"name":"Web App","platform":"web"}`, hdr); r.StatusCode != 201 {
		t.Errorf("same slug other org = %d, want 201", r.StatusCode)
	}
}
