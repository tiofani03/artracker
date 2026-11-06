package issues

import "testing"

func TestFingerprintGroupsIdenticalStackTraces(t *testing.T) {
	stack := "TypeError: Cannot read properties of undefined (reading 'x')\n" +
		"    at handleSubmit (https://cdn.example.com/static/js/app.min.js:1:240)\n" +
		"    at onSubmit (https://cdn.example.com/static/js/app.min.js:1:9810)"

	a := Fingerprint(stack, "TypeError: Cannot read properties of undefined (reading 'x')", "TypeError")
	b := Fingerprint(stack, "TypeError: Cannot read properties of undefined (reading 'x')", "TypeError")
	if a != b {
		t.Fatalf("identical stack traces must share a fingerprint: %s vs %s", a, b)
	}
	if a == "" {
		t.Fatal("fingerprint must not be empty")
	}
}

func TestFingerprintStableAcrossMinifiedColumnChanges(t *testing.T) {
	// Minified line/column shifts must not split an issue into two groups.
	first := Fingerprint(
		"at handleSubmit (app.min.js:1:240)",
		"boom", "TypeError",
	)
	second := Fingerprint(
		"at handleSubmit (app.min.js:2:15000)",
		"boom", "TypeError",
	)
	if first != second {
		t.Fatalf("minified line/column changes must not change fingerprint: %s vs %s", first, second)
	}
}

func TestFingerprintDistinguishesOriginalSourceLines(t *testing.T) {
	// Original (non-minified) sources keep the line number.
	a := Fingerprint("at handleSubmit (PaymentForm.tsx:42:18)", "boom", "TypeError")
	b := Fingerprint("at handleSubmit (PaymentForm.tsx:43:18)", "boom", "TypeError")
	if a == b {
		t.Fatal("different original source lines must produce different fingerprints")
	}
}

func TestFingerprintFallsBackToMessage(t *testing.T) {
	a := Fingerprint("", "database connection timeout", "Error")
	b := Fingerprint("", "database connection timeout", "Error")
	if a != b {
		t.Fatalf("message-only fingerprints must match: %s vs %s", a, b)
	}
	if a == Fingerprint("", "a different error entirely", "Error") {
		t.Fatal("different messages must produce different fingerprints")
	}
}

func TestNormalizeFrameStripsHost(t *testing.T) {
	got := NormalizeFrame("at handleSubmit (https://cdn.example.com/static/js/app.min.js:1:240)")
	want := "at handleSubmit@static/js/app.min.js"
	if got != want {
		t.Fatalf("NormalizeFrame = %q, want %q", got, want)
	}
}

func TestErrorTypeAndTitle(t *testing.T) {
	if got := ErrorType("TypeError: Cannot read properties of undefined"); got != "TypeError" {
		t.Fatalf("ErrorType = %q, want TypeError", got)
	}
	if got := ErrorType("java.lang.NullPointerException: attempt to invoke virtual method"); got != "NullPointerException" {
		t.Fatalf("ErrorType = %q, want NullPointerException", got)
	}
	if got := ErrorType("SIGSEGV: segmentation fault"); got != "SIGSEGV" {
		t.Fatalf("ErrorType = %q, want SIGSEGV", got)
	}
	if got := ErrorType("ANRError: Main thread blocked for 5000ms"); got != "ANRError" {
		t.Fatalf("ErrorType = %q, want ANRError", got)
	}
	if got := ErrorType("something went wrong"); got != "error" {
		t.Fatalf("ErrorType fallback = %q, want error", got)
	}
	if got := Title("   boom   boom  "); got != "boom boom" {
		t.Fatalf("Title = %q, want %q", got, "boom boom")
	}
}
