package alerts

import (
	"context"
	"strings"
	"testing"
	"time"
)

func minMC() MessageContext {
	return MessageContext{ProjectName: "Shop", OrgID: 1, ProjectID: 2, PublicURL: "http://app", HasReplay: true, Detail: DetailMinimal}
}

func detailInc(kind RuleKind) Incident {
	inc := Incident{IssueID: 9, RuleKind: kind, Title: "Cannot read email a@b.c", Environment: "production", ReleaseVersion: "1.2", SessionID: "sess-secret-123"}
	if kind == KindIssueSpike {
		inc.Spike = &SpikeDetail{Count: 142, WindowMinutes: 60, Baseline: 6}
	}
	return inc
}

func mustNotContain(t *testing.T, out string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if strings.Contains(out, s) {
			t.Fatalf("output contains %q:\n%s", s, out)
		}
	}
}

func mustContain(t *testing.T, out string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(out, s) {
			t.Fatalf("output lacks %q:\n%s", s, out)
		}
	}
}

func TestMinimalFireOmitsTitleSessionAndReplay(t *testing.T) {
	cases := map[RuleKind][2]string{
		KindNewIssue:   {"🆕 New issue", "A new error was detected"},
		KindRegression: {"🔁 Regression", "A resolved error occurred again"},
		KindIssueSpike: {"📈 Spike", "Error volume is unusually high"},
	}
	for kind, want := range cases {
		out := FormatFire(detailInc(kind), minMC())
		mustContain(t, out, want[0], want[1], "Shop", "production · release 1.2", `<a href="http://app/dashboard/issues/9?org=1&amp;project=2">Open issue</a>`)
		mustNotContain(t, out, "Cannot read", "a@b.c", "sess-secret-123", "Watch replay", "replay")
	}
}

func TestMinimalSpikeKeepsCounts(t *testing.T) {
	out := FormatFire(detailInc(KindIssueSpike), minMC())
	mustContain(t, out, "142 events in 60 min, baseline 6")
	mustNotContain(t, out, "Cannot read")
}

func TestMinimalRecoveredOmitsTitle(t *testing.T) {
	out := FormatRecovered(detailInc(KindIssueSpike), minMC())
	mustContain(t, out, "✅ Spike recovered · Shop", "The error volume is back to normal", "production · release 1.2", "Open issue")
	mustNotContain(t, out, "Cannot read", "a@b.c", "sess-secret-123")
}

func TestMinimalDigestListsCountsNotTitles(t *testing.T) {
	mk := func(k RuleKind, n int) []Incident {
		var out []Incident
		for i := 0; i < n; i++ {
			out = append(out, detailInc(k))
		}
		return out
	}
	incs := append(append(mk(KindIssueSpike, 1), mk(KindNewIssue, 3)...), mk(KindRegression, 2)...)
	out := FormatDigest(incs, minMC())
	mustContain(t, out, "🚨 6 new alerts · Shop\n", "3 new issues, 2 regressions, 1 spike\n", `<a href="http://app/dashboard/alerts?org=1&amp;project=2">Open alerts</a>`)
	mustNotContain(t, out, "Cannot read", "•", "more")

	out = FormatDigest(append(mk(KindNewIssue, 1), mk(KindRegression, 1)...), minMC())
	mustContain(t, out, "1 new issue, 1 regression\n")
	mustNotContain(t, out, "spike")
}

func TestMinimalTestMessageUnchanged(t *testing.T) {
	mc := minMC()
	full := mc
	full.Detail = DetailFull
	if FormatTest(mc) != FormatTest(full) {
		t.Fatal("test message differs between levels")
	}
}

func TestZeroDetailBehavesAsFull(t *testing.T) {
	zero, full := minMC(), minMC()
	zero.Detail, full.Detail = "", DetailFull
	for _, k := range []RuleKind{KindNewIssue, KindRegression, KindIssueSpike} {
		inc := detailInc(k)
		if FormatFire(inc, zero) != FormatFire(inc, full) || FormatRecovered(inc, zero) != FormatRecovered(inc, full) {
			t.Fatalf("zero detail differs for %s", k)
		}
		if !strings.Contains(FormatFire(inc, zero), "Cannot read email a@b.c") {
			t.Fatal("full fire lacks title")
		}
	}
	incs := []Incident{detailInc(KindNewIssue), detailInc(KindRegression)}
	if FormatDigest(incs, zero) != FormatDigest(incs, full) || FormatTest(zero) != FormatTest(full) {
		t.Fatal("zero detail differs for digest/test")
	}
	mustContain(t, FormatDigest(incs, zero), "• Cannot read email a@b.c")
}

func TestMinimalEscapesProjectName(t *testing.T) {
	mc := minMC()
	mc.ProjectName = `<b>&"x"`
	for name, out := range map[string]string{
		"fire":      FormatFire(detailInc(KindNewIssue), mc),
		"recovered": FormatRecovered(detailInc(KindIssueSpike), mc),
		"digest":    FormatDigest([]Incident{detailInc(KindNewIssue)}, mc),
	} {
		mustContain(t, out, "&lt;b&gt;&amp;&#34;x&#34;")
		mustNotContain(t, out, "<b>")
		_ = name
	}
}

func TestMinimalNeverContainsUserFields(t *testing.T) {
	const hostTitle, hostSess = "TITLE<script>HOSTILE", "SESS&<i>HOSTILE"
	inc := Incident{IssueID: 1, RuleKind: KindIssueSpike, Title: hostTitle, SessionID: hostSess,
		Environment: "e<nv>", ReleaseVersion: "r&1", Spike: &SpikeDetail{Count: 1, WindowMinutes: 5, Baseline: 1}}
	mc := minMC()
	outs := map[string]string{
		"fire":      FormatFire(inc, mc),
		"recovered": FormatRecovered(inc, mc),
		"digest":    FormatDigest([]Incident{inc, inc, inc, inc, inc, inc, inc}, mc),
		"test":      FormatTest(mc),
	}
	for name, out := range outs {
		mustNotContain(t, out, hostTitle, esc(hostTitle), "HOSTILE", url_(hostSess))
		if name == "fire" || name == "recovered" {
			mustContain(t, out, "e&lt;nv&gt; · release r&amp;1")
		}
	}
}

func url_(s string) string { return strings.NewReplacer("&", "%26", "<", "%3C").Replace(s) }

func TestDispatchTenantRespectsDetailLevel(t *testing.T) {
	pool := testDB(t)
	s, srv := tenantService(t, pool)
	orgA := seedOrgWithBot(t, pool, s, "org-a", testBotToken)
	orgB := seedOrgWithBot(t, pool, s, "org-b", testBotToken2)
	if _, err := pool.Exec(context.Background(), `UPDATE alert_bots SET detail_level='minimal' WHERE org_id=$1`, orgA); err != nil {
		t.Fatal(err)
	}
	pa := seedOrgProject(t, pool, orgA, "proj-a")
	pb := seedOrgProject(t, pool, orgB, "proj-b")
	seedManualChannel(t, pool, pa, "-100111")
	seedManualChannel(t, pool, pb, "-100222")
	seedTenantIncident(t, pool, pa, time.Minute)
	seedTenantIncident(t, pool, pb, time.Minute)

	dispatch(t, s, dispatchNow)

	a, b := callsForToken(srv, testBotToken), callsForToken(srv, testBotToken2)
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("requests a=%+v b=%+v", a, b)
	}
	ta, _ := a[0].Body["text"].(string)
	tb, _ := b[0].Body["text"].(string)
	mustNotContain(t, ta, "TypeError: boom")
	mustContain(t, ta, "A new error was detected")
	mustContain(t, tb, "TypeError: boom")
}

func TestDispatchSharedModeIsFull(t *testing.T) {
	pool := testDB(t)
	_, pid := seedProject(t, pool)
	seedChannel(t, pool, pid, -42)
	seedIncident(t, pool, pid, KindNewIssue, "TypeError: boom")
	fn := &fakeNotifier{}
	s := NewService(pool, fn, "http://app")
	dispatch(t, s, dispatchNow)
	sent := fn.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %+v", sent)
	}
	mustContain(t, sent[0].HTML, "TypeError: boom")
}
