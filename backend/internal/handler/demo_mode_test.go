package handler

import (
	"fmt"
	"strings"
	"testing"
)

type demoFixture struct {
	e        *testEnv
	pid      int64
	adminHdr map[string]string // a real member
	otherHdr map[string]string // a signed-in user who is NOT a member of the project's org
	keysPath string
}

func newDemoFixture(t *testing.T) demoFixture {
	t.Helper()
	e := newTestEnv(t)
	org := e.addOrg(t, "acme-demo-mode")
	adminID, adminHdr := e.addUser(t, "demoadmin")
	e.addMember(t, org, adminID, "admin")
	_, otherHdr := e.addUser(t, "demooutsider")
	pid := e.addProject(t, org, "demo-mode-app")
	return demoFixture{e: e, pid: pid, adminHdr: adminHdr, otherHdr: otherHdr, keysPath: fmt.Sprintf("/api/v1/projects/%d/keys", pid)}
}

func (f demoFixture) get(t *testing.T, path string, hdr map[string]string) int {
	t.Helper()
	return f.e.do(t, "GET", path, strings.NewReader(""), hdr).StatusCode
}

func TestDemoModeStillWorksInDevelopment(t *testing.T) {
	f := newDemoFixture(t)
	f.e.h.cfg.Env = "development"
	if got := f.get(t, f.keysPath, map[string]string{"X-Demo-Mode": "1"}); got != 200 {
		t.Fatalf("demo header in development = %d, want 200", got)
	}
	if got := f.get(t, f.keysPath+"?demo=1", nil); got != 200 {
		t.Fatalf("demo query in development = %d, want 200", got)
	}
}

func TestDemoModeIsIgnoredOutsideDevelopment(t *testing.T) {
	for _, env := range []string{"production", "staging"} {
		t.Run(env, func(t *testing.T) {
			f := newDemoFixture(t)
			f.e.h.cfg.Env = env

			// no credentials: the demo header / query must not stand in for a login
			if got := f.get(t, f.keysPath, map[string]string{"X-Demo-Mode": "1"}); got != 401 {
				t.Errorf("anonymous + demo header = %d, want 401", got)
			}
			if got := f.get(t, f.keysPath+"?demo=1", nil); got != 401 {
				t.Errorf("anonymous + demo query = %d, want 401", got)
			}

			// a signed-in non-member must not skip the project role check with the demo header
			withDemo := map[string]string{"X-Demo-Mode": "1"}
			for k, v := range f.otherHdr {
				withDemo[k] = v
			}
			if got := f.get(t, f.keysPath, withDemo); got != 404 {
				t.Errorf("non-member + demo header = %d, want 404", got)
			}

			// real members are unaffected
			if got := f.get(t, f.keysPath, f.adminHdr); got != 200 {
				t.Errorf("member without demo header = %d, want 200", got)
			}
		})
	}
}
