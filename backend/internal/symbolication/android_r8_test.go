package symbolication

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseProguardMappingAndRetrace(t *testing.T) {
	mappingContent := `# compiler: R8
# compiler_version: 8.2.33
com.example.artracker.network.ApiClient -> a.b.c:
    int timeoutMs -> a
    10:25:void authenticate(java.lang.String) -> a
    30:45:com.example.artracker.model.User getUser() -> b
com.example.artracker.ui.LoginViewModel -> d.e.f:
    50:65:void onLoginClicked() -> c
com.example.artracker.exception.AuthException -> x.y.z:
`

	retracer, err := ParseProguardMapping([]byte(mappingContent))
	if err != nil {
		t.Fatalf("ParseProguardMapping failed: %v", err)
	}

	// Test class retrace
	if got := retracer.RetraceClass("a.b.c"); got != "com.example.artracker.network.ApiClient" {
		t.Errorf("RetraceClass(a.b.c) = %s, want com.example.artracker.network.ApiClient", got)
	}
	if got := retracer.RetraceClass("x.y.z"); got != "com.example.artracker.exception.AuthException" {
		t.Errorf("RetraceClass(x.y.z) = %s, want com.example.artracker.exception.AuthException", got)
	}

	// Test method retrace with line numbers
	origClass, origMethod, line := retracer.RetraceMethod("a.b.c", "a", 15)
	if origClass != "com.example.artracker.network.ApiClient" || origMethod != "authenticate" || line != 15 {
		t.Errorf("RetraceMethod(a.b.c, a, 15) = (%s, %s, %d), want (com.example.artracker.network.ApiClient, authenticate, 15)", origClass, origMethod, line)
	}

	// Test full stack trace retrace
	obfStack := `x.y.z: Invalid credentials provided
    at a.b.c.a(SourceFile:18)
    at d.e.f.c(SourceFile:55)
    at android.view.View.performClick(View.java:7125)`

	retraced := retracer.RetraceStackTrace(obfStack)

	if !strings.Contains(retraced, "com.example.artracker.exception.AuthException: Invalid credentials provided") {
		t.Errorf("Expected retraced exception class, got:\n%s", retraced)
	}
	if !strings.Contains(retraced, "at com.example.artracker.network.ApiClient.authenticate(ApiClient.kt:18)") {
		t.Errorf("Expected retraced ApiClient.authenticate, got:\n%s", retraced)
	}
	if !strings.Contains(retraced, "at com.example.artracker.ui.LoginViewModel.onLoginClicked(LoginViewModel.kt:55)") {
		t.Errorf("Expected retraced LoginViewModel.onLoginClicked, got:\n%s", retraced)
	}
	if !strings.Contains(retraced, "android.view.View.performClick(View.java:7125)") {
		t.Errorf("Expected framework frame preserved, got:\n%s", retraced)
	}
}

const inlineMapping = `com.example.Shop -> a.b:
    1:3:void com.example.Cart.load():40:42 -> a
    1:3:void start():12 -> a
    4:4:void start():13 -> a
    void boot() -> b
`

func TestRetraceFrameInline(t *testing.T) {
	r, err := ParseProguardMapping([]byte(inlineMapping))
	if err != nil {
		t.Fatalf("ParseProguardMapping failed: %v", err)
	}

	got, ok := r.RetraceFrame("a.b", "a", "SourceFile", 2)
	want := []RetracedFrame{
		{Class: "com.example.Cart", Method: "load", File: "Cart.kt", Line: 41},
		{Class: "com.example.Shop", Method: "start", File: "Shop.kt", Line: 12},
	}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("RetraceFrame(a.b, a, SourceFile, 2) = %+v, %v; want %+v, true", got, ok, want)
	}

	got, ok = r.RetraceFrame("a.b", "a", "SourceFile", 4)
	want = []RetracedFrame{{Class: "com.example.Shop", Method: "start", File: "Shop.kt", Line: 13}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("RetraceFrame(a.b, a, SourceFile, 4) = %+v, %v; want %+v, true", got, ok, want)
	}

	if got, ok := r.RetraceFrame("x.y", "z", "SourceFile", 1); ok || got != nil {
		t.Errorf("RetraceFrame(x.y, z, SourceFile, 1) = %+v, %v; want nil, false", got, ok)
	}
}

func TestRetraceFrameNoRange(t *testing.T) {
	r, err := ParseProguardMapping([]byte(inlineMapping))
	if err != nil {
		t.Fatalf("ParseProguardMapping failed: %v", err)
	}
	got, ok := r.RetraceFrame("a.b", "b", "", 0)
	want := []RetracedFrame{{Class: "com.example.Shop", Method: "boot", File: "Shop.kt", Line: 0}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("RetraceFrame(a.b, b, \"\", 0) = %+v, %v; want %+v, true", got, ok, want)
	}
}

func TestParseProguardMappingKeepsRangedLines(t *testing.T) {
	r, err := ParseProguardMapping([]byte(inlineMapping))
	if err != nil {
		t.Fatalf("ParseProguardMapping failed: %v", err)
	}
	_, method, _ := r.RetraceMethod("a.b", "a", 2)
	if method != "load" {
		t.Errorf("RetraceMethod(a.b, a, 2) method = %q, want load", method)
	}
}
