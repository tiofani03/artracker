package alerts

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func testMC() MessageContext {
	return MessageContext{ProjectName: "Checkout Web", OrgID: 1, ProjectID: 2, PublicURL: "http://app"}
}

func testInc() Incident {
	return Incident{
		IssueID: 9, RuleKind: KindNewIssue, Title: "TypeError: x",
		Environment: "production", ReleaseVersion: "1.2.3", SessionID: "sess_1",
	}
}

func TestFormatFireNewIssue(t *testing.T) {
	mc := testMC()
	out := FormatFire(testInc(), mc)
	for _, want := range []string{
		"🆕 New issue · Checkout Web", "TypeError: x", "production · release 1.2.3",
		`href="http://app/dashboard/issues/9?org=1&amp;project=2"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "replays") {
		t.Errorf("unexpected replay link:\n%s", out)
	}
	mc.HasReplay = true
	out = FormatFire(testInc(), mc)
	if !strings.Contains(out, "/dashboard/replays?session=sess_1") {
		t.Errorf("missing replay link:\n%s", out)
	}
}

func TestFormatSpikeAndRecovered(t *testing.T) {
	inc := testInc()
	inc.RuleKind = KindIssueSpike
	inc.Spike = &SpikeDetail{Count: 142, Baseline: 6, WindowMinutes: 60}
	out := FormatFire(inc, testMC())
	if !strings.Contains(out, "📈") || !strings.Contains(out, "142 events in 60 min, baseline 6") {
		t.Errorf("bad spike message:\n%s", out)
	}
	inc.Spike = nil
	_ = FormatFire(inc, testMC()) // nil-safe
	if rec := FormatRecovered(inc, testMC()); !strings.HasPrefix(rec, "✅") {
		t.Errorf("recovered should start with check: %s", rec)
	}
}

func TestFormatDigest(t *testing.T) {
	var incs []Incident
	for i := 0; i < 7; i++ {
		inc := testInc()
		inc.Title = "title-" + string(rune('A'+i))
		incs = append(incs, inc)
	}
	out := FormatDigest(incs, testMC())
	if !strings.HasPrefix(out, "🚨 7 new alerts · Checkout Web") {
		t.Errorf("bad header:\n%s", out)
	}
	if n := strings.Count(out, "title-"); n != 5 {
		t.Errorf("want 5 titles, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "+2 more") || !strings.Contains(out, "/dashboard/alerts?org=1&amp;project=2") {
		t.Errorf("missing more/link:\n%s", out)
	}
}

func TestFormatEscapesAndTruncates(t *testing.T) {
	inc := testInc()
	inc.Title = "<b>x</b> & " + strings.Repeat("a", 5000)
	out := FormatFire(inc, testMC())
	if !strings.Contains(out, "&lt;b&gt;x&lt;/b&gt; &amp;") {
		t.Errorf("not escaped:\n%s", out)
	}
	if strings.Contains(out, "<b>x") {
		t.Errorf("raw html leaked")
	}
	if got := truncateRunes(inc.Title, 200); utf8.RuneCountInString(got) != 200 || !strings.HasSuffix(got, "…") {
		t.Errorf("bad truncation: %d runes", utf8.RuneCountInString(got))
	}
	if len(out) > 4096 {
		t.Errorf("message too long: %d", len(out))
	}
}
