package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"testing"
)

func (e *testEnv) addSessionForUser(t *testing.T, pid int64, id, userID string, hasError bool) {
	t.Helper()
	_, err := e.pool.Exec(context.Background(),
		`INSERT INTO replay_sessions(id, project_id, user_id, has_error) VALUES ($1,$2,$3,$4)`, id, pid, userID, hasError)
	if err != nil {
		t.Fatal(err)
	}
}

func TestListSessionsByUserPrefix(t *testing.T) {
	e := newTestEnv(t)
	uid, authHdr := e.addUser(t, "usersearch")
	orgID := e.addOrg(t, "acme-usersearch")
	e.addMember(t, orgID, uid, "owner")
	pid := e.addProject(t, orgID, "search-app")
	other := e.addProject(t, orgID, "search-other")

	e.addSessionForUser(t, pid, "ses_a_0001", "user_42", false)
	e.addSessionForUser(t, pid, "ses_a_0002", "user_43", true)
	e.addSessionForUser(t, pid, "ses_a_0003", "userX42", false) // '_' must not act as a wildcard
	e.addSessionForUser(t, pid, "ses_a_0004", "100%_x", false)  // '%' must not act as a wildcard
	e.addSessionForUser(t, pid, "ses_a_0005", "Alice", false)
	e.addSessionForUser(t, other, "ses_b_0001", "user_44", false) // another project

	ids := func(query string) []string {
		resp := e.do(t, "GET", fmt.Sprintf("/api/v1/projects/%d/sessions?%s", pid, query), nil, authHdr)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status %d", query, resp.StatusCode)
		}
		var body struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			Total int64 `json:"total"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		out := []string{}
		for _, it := range body.Items {
			out = append(out, it.ID)
		}
		sort.Strings(out)
		if body.Total != int64(len(out)) {
			t.Fatalf("%s: total %d != %d items", query, body.Total, len(out))
		}
		return out
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("got %v want %v", got, want)
		}
	}

	eq(ids("user_prefix=user_4"), "ses_a_0001", "ses_a_0002")
	eq(ids("user_prefix=user_"), "ses_a_0001", "ses_a_0002")
	eq(ids("user_prefix=" + url.QueryEscape("%")))                // '%' matches nothing, not everything
	eq(ids("user_prefix="+url.QueryEscape("100%")), "ses_a_0004") // literal percent
	eq(ids("user_prefix=alice"), "ses_a_0005")                    // case-insensitive
	eq(ids("user_prefix=user_4&has_error=true"), "ses_a_0002")    // combines with other filters
	eq(ids("user_prefix=user_44"))                                // another project's user is not visible
	eq(ids("user_prefix=zzz"))

	// the exact user_id filter keeps its old, exact meaning
	eq(ids("user_id=user_4"))
	eq(ids("user_id=user_42"), "ses_a_0001")
}
