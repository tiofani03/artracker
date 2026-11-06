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
