package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tiofani03/artracker/backend/internal/issues"
)

var mcpAndroidTrace = strings.Join([]string{
	"java.lang.NullPointerException: Attempt to invoke virtual method on a null object reference",
	"\tat com.gli.shop.checkout.CheckoutActivity.onPaymentResult(CheckoutActivity.kt:214)",
	"\tat androidx.lifecycle.LiveData.considerNotify(LiveData.java:133)",
	"\tat android.os.Handler.dispatchMessage(Handler.java:106)",
	"Caused by: java.lang.IllegalStateException: no order",
	"\tat com.gli.shop.data.OrderRepository.current(OrderRepository.kt:41)",
	"\t... 12 more",
}, "\n")

func TestFormatStackTraceAndroid(t *testing.T) {
	out := formatStackTrace(issues.ParseStackTrace(mcpAndroidTrace), true)
	for _, want := range []string{
		"→ com.gli.shop.checkout.CheckoutActivity.onPaymentResult (CheckoutActivity.kt:214)",
		"… 2 framework frames",
		"Caused by: java.lang.IllegalStateException",
		"→ com.gli.shop.data.OrderRepository.current (OrderRepository.kt:41)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "not deobfuscated") {
		t.Fatalf("deobfuscated trace flagged as raw:\n%s", out)
	}
}

func TestFormatStackTraceTruncatesHugeTrace(t *testing.T) {
	var b strings.Builder
	b.WriteString("java.lang.StackOverflowError: stack size 8MB")
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&b, "\n\tat com.gli.shop.Recursive.call%d(Recursive.kt:%d)", i, i)
	}
	out := formatStackTrace(issues.ParseStackTrace(b.String()), true)
	lines := strings.Split(out, "\n")
	if len(lines) > 61 {
		t.Fatalf("got %d lines, want <= 61", len(lines))
	}
	if lines[len(lines)-1] != "… truncated" {
		t.Fatalf("last line = %q, want … truncated", lines[len(lines)-1])
	}
}

func TestFormatStackTraceEmpty(t *testing.T) {
	if out := formatStackTrace(issues.ParseStackTrace(""), false); out != "No stack trace captured for this event." {
		t.Fatalf("got %q", out)
	}
}

func TestFormatStackTraceRawSaysNotDeobfuscated(t *testing.T) {
	out := formatStackTrace(issues.ParseStackTrace(mcpAndroidTrace), false)
	if !strings.Contains(out, "not deobfuscated") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestFormatIssueDetailNotDeobfuscatedNamesRelease(t *testing.T) {
	d := IssueDetail{
		Issue:   Issue{ID: 7, Title: "java.lang.NullPointerException: boom", Type: "NullPointerException", Status: "open", Count: 2},
		Summary: IssueSummary{TotalEvents: 2, AffectedUsers: 1},
	}
	ev := IssueEvent{ID: 9, StackTrace: mcpAndroidTrace, StackTraceResolved: mcpAndroidTrace, ReleaseVersion: "3.1.0",
		Breadcrumbs: json.RawMessage(`[]`), DeviceInfo: json.RawMessage(`{}`), CreatedAt: time.Now()}
	out := formatIssueDetail(d, ev)
	if !strings.Contains(out, "not deobfuscated") || !strings.Contains(out, "3.1.0") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestFormatIssueListEmpty(t *testing.T) {
	if out := formatIssueList(nil); !strings.Contains(out, "No issues") {
		t.Fatalf("got %q", out)
	}
}
