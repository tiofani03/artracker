package symbolication

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestDemangleSwift(t *testing.T) {
	tests := []struct {
		mangled string
		want    string
	}{
		{
			mangled: "$s5MyApp14ViewControllerC11didTapLoginyyF",
			want:    "MyApp.ViewController.didTapLogin()",
		},
		{
			mangled: "_$s4Core7NetworkC8sendDatayyF",
			want:    "Core.Network.sendData()",
		},
		{
			mangled: "+[MyObjcClass performAction]",
			want:    "+[MyObjcClass performAction]",
		},
		{
			mangled: "\x01-[PLCrashReporter setCrashCallbacks:].cold.1",
			want:    "-[PLCrashReporter setCrashCallbacks:].cold.1",
		},
		{
			mangled: "$s8Alfagift20BasketViewControllerC014updateCheckoutC0031_8347FBAD5C489AB6D9CBA5A0EF8C35L0LL9isLoadingySb_tF",
			want:    "Alfagift.BasketViewController.updateCheckoutView()",
		},
		{
			mangled: "_$s8Alfagift20BasketViewControllerC04initC5Model031_8347FBAD5C489AB6D9CBA5A0EF8C35L0LLyyFySbSgcfU_",
			want:    "Alfagift.BasketViewController.initViewModel()",
		},
		{
			mangled: "__ZN6kotlin31NativeOrUnregisteredThreadGuardD1Ev",
			want:    "kotlin::NativeOrUnregisteredThreadGuard::~NativeOrUnregisteredThreadGuard()",
		},
		{
			mangled: "plain_c_function",
			want:    "plain_c_function",
		},
	}

	for _, tt := range tests {
		got := DemangleSwift(tt.mangled)
		if got != tt.want {
			t.Errorf("DemangleSwift(%q) = %q, want %q", tt.mangled, got, tt.want)
		}
	}
}

func TestExtractDSYMBinaryFromZip(t *testing.T) {
	// Create a dummy in-memory zip containing a dSYM bundle
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	dummyContent := []byte("dummy macho dwarf content")
	w, err := zw.Create("App.app.dSYM/Contents/Resources/DWARF/App")
	if err != nil {
		t.Fatalf("Create in zip failed: %v", err)
	}
	if _, err := w.Write(dummyContent); err != nil {
		t.Fatalf("Write in zip failed: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close zip failed: %v", err)
	}

	extracted, name, err := ExtractDSYMBinaryFromZip(buf.Bytes())
	if err != nil {
		t.Fatalf("ExtractDSYMBinaryFromZip failed: %v", err)
	}

	if name != "App" {
		t.Errorf("Extracted binary name = %q, want App", name)
	}
	if string(extracted) != string(dummyContent) {
		t.Errorf("Extracted content = %q, want %q", string(extracted), string(dummyContent))
	}
}

func TestExtractDSYMBinaryFromZipPrefersLargestDebugDylib(t *testing.T) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	// Stub executable (12 bytes) placed BEFORE the larger .debug.dylib
	w1, _ := zw.Create("Alfagift.app.dSYM/Contents/Resources/DWARF/Alfagift")
	_, _ = w1.Write([]byte("tiny-stub-12"))

	largeDylib := bytes.Repeat([]byte("D"), 1024)
	w2, _ := zw.Create("Alfagift.app.dSYM/Contents/Resources/DWARF/Alfagift.debug.dylib")
	_, _ = w2.Write(largeDylib)
	_ = zw.Close()

	extracted, name, err := ExtractDSYMBinaryFromZip(buf.Bytes())
	if err != nil {
		t.Fatalf("ExtractDSYMBinaryFromZip failed: %v", err)
	}
	if name != "Alfagift.debug.dylib" {
		t.Errorf("name = %q, want Alfagift.debug.dylib", name)
	}
	if !bytes.Equal(extracted, largeDylib) {
		t.Errorf("extracted len = %d, want %d", len(extracted), len(largeDylib))
	}
}

func TestDemangleIOSStackTraceRegex(t *testing.T) {
	rawStack := `0   MyApp                               0x0000000104a12340 0x104a00000 + 74560
1   UIKitCore                           0x000000019204b2a0 0x192000000 + 308800`

	// Nil symbolicator falls back cleanly to raw stack trace
	demangled := DemangleIOSStackTrace(rawStack, nil, 0x104a00000)
	if demangled != rawStack {
		t.Errorf("DemangleIOSStackTrace with nil symbolicator should return raw stack, got:\n%s", demangled)
	}

	lines := strings.Split(rawStack, "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(lines))
	}
}
