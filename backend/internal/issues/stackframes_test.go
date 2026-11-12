package issues

import (
	"reflect"
	"strings"
	"testing"
)

var androidTrace = strings.Join([]string{
	"java.lang.NullPointerException: Attempt to invoke virtual method on a null object reference",
	"\tat com.gli.shop.checkout.CheckoutActivity.onPaymentResult(CheckoutActivity.kt:214)",
	"\tat androidx.lifecycle.LiveData.considerNotify(LiveData.java:133)",
	"\tat android.os.Handler.dispatchMessage(Handler.java:106)",
	"Caused by: java.lang.IllegalStateException: no order",
	"\tat com.gli.shop.data.OrderRepository.current(OrderRepository.kt:41)",
	"\t... 12 more",
}, "\n")

func appFlags(fs []StackFrame) []bool {
	out := []bool{}
	for _, f := range fs {
		out = append(out, f.App)
	}
	return out
}

func TestParseStackTraceSections(t *testing.T) {
	s := ParseStackTrace(androidTrace)
	if len(s) != 2 {
		t.Fatalf("want 2 sections, got %d", len(s))
	}
	if s[0].Title != "java.lang.NullPointerException: Attempt to invoke virtual method on a null object reference" ||
		s[1].Title != "java.lang.IllegalStateException: no order" {
		t.Fatalf("titles: %q / %q", s[0].Title, s[1].Title)
	}
	if s[0].CausedBy || !s[1].CausedBy {
		t.Fatal("causedBy flags wrong")
	}
}

func TestParseStackTraceAppFlags(t *testing.T) {
	s := ParseStackTrace(androidTrace)
	if got := appFlags(s[0].Frames); !reflect.DeepEqual(got, []bool{true, false, false}) {
		t.Fatalf("main: %v", got)
	}
	if got := appFlags(s[1].Frames); !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("cause: %v", got)
	}
}

func TestParseStackTraceJVMFrame(t *testing.T) {
	f := ParseStackTrace(androidTrace)[0].Frames[0]
	if f.Symbol != "com.gli.shop.checkout.CheckoutActivity.onPaymentResult" || f.Location != "CheckoutActivity.kt:214" {
		t.Fatalf("got %+v", f)
	}
}

func TestParseStackTraceJSFrame(t *testing.T) {
	f := ParseStackTrace("TypeError: boom\n    at handleSubmit (src/PaymentForm.tsx:142:19)")[0].Frames[0]
	if f.Symbol != "handleSubmit" || f.Location != "src/PaymentForm.tsx:142:19" || !f.App {
		t.Fatalf("got %+v", f)
	}
}

func TestParseStackTraceNodeModulesIsLibrary(t *testing.T) {
	f := ParseStackTrace("Error: x\n    at render (node_modules/react-dom/cjs/react-dom.js:10:5)")[0].Frames[0]
	if f.App {
		t.Fatal("node_modules frame should be library")
	}
}

func TestParseStackTraceEmpty(t *testing.T) {
	if s := ParseStackTrace(""); len(s) != 0 {
		t.Fatalf("want none, got %v", s)
	}
}

func TestGroupFramesCollapsesLibraryRuns(t *testing.T) {
	groups := GroupFrames(ParseStackTrace(androidTrace)[0].Frames)
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %d", len(groups))
	}
	if groups[0].App == nil || groups[0].Library != nil {
		t.Fatalf("group 0 should be app: %+v", groups[0])
	}
	if groups[1].App != nil || len(groups[1].Library) != 2 {
		t.Fatalf("group 1 should be lib x2: %+v", groups[1])
	}
}
