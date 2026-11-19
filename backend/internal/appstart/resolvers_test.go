package appstart

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tiofani03/artracker/backend/internal/ingest"
	"github.com/tiofani03/artracker/backend/internal/symbolication"
)

// darwinBinary is a Mach-O arm64 binary with DWARF built from the symbolication testdata/hello
// program. Test helpers in package symbolication are not importable, so it is rebuilt here.
// It is nil (and darwinFixtureSkip is set) when the build is not possible.
var (
	darwinBinary      []byte
	darwinFixtureSkip string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "appstart-hello-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdir temp:", err)
		os.Exit(1)
	}
	darwinBinary, darwinFixtureSkip = buildDarwinBinary(dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func buildDarwinBinary(dir string) ([]byte, string) {
	out := filepath.Join(dir, "hello")
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", out, "../symbolication/testdata/hello")
	cmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Sprintf("cannot build darwin fixture: %v\n%s", err, b)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Sprintf("cannot read darwin fixture: %v", err)
	}
	return data, ""
}

func intp(i int) *int { return &i }

func TestAndroidResolverAppPackages(t *testing.T) {
	r := NewAndroidResolver(nil, []string{"com.shop"})
	if !r.MissingSymbols() {
		t.Error("nil retracer: MissingSymbols() = false, want true")
	}
	app := r.Resolve(ingest.ProfileFrame{C: "com.shop.App", M: "onCreate", L: intp(12), F: "App.kt"}, false)
	if len(app) != 1 || !app[0].IsApp || app[0].Function != "com.shop.App.onCreate" || app[0].Line != 12 || app[0].File != "App.kt" || app[0].Resolved {
		t.Errorf("app frame = %+v", app)
	}
	other := r.Resolve(ingest.ProfileFrame{C: "com.shopping.X", M: "run"}, false)
	if len(other) != 1 || other[0].IsApp {
		t.Errorf("com.shopping.X = %+v, want non-app (dot boundary)", other)
	}
}

func TestAndroidResolverRetraces(t *testing.T) {
	mapping := `com.shop.ui.Home -> a.b:
    10:20:void render() -> a
`
	ret, err := symbolication.ParseProguardMapping([]byte(mapping))
	if err != nil {
		t.Fatal(err)
	}
	r := NewAndroidResolver(ret, []string{"com.shop"})
	if r.MissingSymbols() {
		t.Error("MissingSymbols() = true with a retracer")
	}
	got := r.Resolve(ingest.ProfileFrame{C: "a.b", M: "a", L: intp(15), F: "SourceFile"}, true)
	if len(got) != 1 {
		t.Fatalf("frames = %+v", got)
	}
	if got[0].Function != "com.shop.ui.Home.render" || !got[0].IsApp || !got[0].Resolved {
		t.Errorf("retraced = %+v", got[0])
	}
	// A class absent from the mapping stays raw and unresolved.
	raw := r.Resolve(ingest.ProfileFrame{C: "androidx.core.X", M: "m"}, false)
	if len(raw) != 1 || raw[0].Function != "androidx.core.X.m" || raw[0].Resolved || raw[0].IsApp {
		t.Errorf("unmapped = %+v", raw)
	}
}

func TestIOSResolverFallbacks(t *testing.T) {
	const appUUID = "11111111-2222-3333-4444-555555555555"
	images := []ingest.ProfileImage{
		{UUID: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE", Name: "libsystem", Load: "0x180000000", App: false},
		{UUID: appUUID, Name: "MyApp", Load: "0x100000000", App: true},
	}
	r := NewIOSResolver(images, map[string]*symbolication.IOSSymbolicator{appUUID: nil})
	if !r.MissingSymbols() {
		t.Error("app image with nil symbolicator: MissingSymbols() = false, want true")
	}

	sys := r.Resolve(ingest.ProfileFrame{A: "0x180001000", I: intp(0), Sym: "dyld4::start"}, false)
	if len(sys) != 1 || sys[0].Function != "dyld4::start" || sys[0].IsApp || !sys[0].Resolved {
		t.Errorf("system frame = %+v", sys)
	}
	app := r.Resolve(ingest.ProfileFrame{A: "0x100001a20", I: intp(1)}, false)
	if len(app) != 1 || app[0].Function != "MyApp+0x1a20" || !app[0].IsApp || app[0].Resolved {
		t.Errorf("app frame without dSYM = %+v", app)
	}
	// The fallback label keeps the raw offset even for caller frames.
	caller := r.Resolve(ingest.ProfileFrame{A: "0x100001a20", I: intp(1)}, true)
	if caller[0].Function != "MyApp+0x1a20" {
		t.Errorf("caller fallback = %q, want raw offset", caller[0].Function)
	}
	// Frames that name no usable image still resolve to something.
	if lone := r.Resolve(ingest.ProfileFrame{A: "0x10", Sym: "lone"}, false); len(lone) != 1 || lone[0].Function != "lone" {
		t.Errorf("imageless frame = %+v", lone)
	}
}

func TestIOSResolverMissingSymbolsFalseWhenAppSymbolicated(t *testing.T) {
	if darwinBinary == nil {
		t.Skip(darwinFixtureSkip)
	}
	sym, err := symbolication.NewIOSSymbolicator(darwinBinary)
	if err != nil {
		t.Fatal(err)
	}
	defer sym.Close()
	const u = "11111111-2222-3333-4444-555555555555"
	images := []ingest.ProfileImage{{UUID: strings.ToLower(u), Name: "hello", Load: "0x100000000", App: true}}
	r := NewIOSResolver(images, map[string]*symbolication.IOSSymbolicator{u: sym})
	if r.MissingSymbols() {
		t.Error("MissingSymbols() = true with a symbolicator for the app image")
	}
}

// callReturnAddress finds a bl instruction in the fixture's __text whose return address (the next
// instruction) is in the same main.* function but on a different source line than the bl itself,
// and returns that return address.
func callReturnAddress(t *testing.T, sym *symbolication.IOSSymbolicator) uint64 {
	t.Helper()
	f, err := macho.NewFile(bytes.NewReader(darwinBinary))
	if err != nil {
		t.Fatal(err)
	}
	sec := f.Section("__text")
	if sec == nil {
		t.Fatal("fixture has no __text")
	}
	text, err := sec.Data()
	if err != nil {
		t.Fatal(err)
	}
	for off := 0; off+8 <= len(text); off += 4 {
		insn := binary.LittleEndian.Uint32(text[off:])
		if insn&0xFC000000 != 0x94000000 { // bl
			continue
		}
		call := sec.Addr + uint64(off)
		ret := call + 4
		a, b := sym.SymbolicateAddress(call), sym.SymbolicateAddress(ret)
		if a.Found && b.Found && strings.HasPrefix(a.Function, "main.") && a.Function == b.Function && a.Line != b.Line {
			return ret
		}
	}
	t.Skip("fixture has no bl in main.* whose return address falls on a later line")
	return 0
}

func TestIOSResolverCallerSubtractsOne(t *testing.T) {
	if darwinBinary == nil {
		t.Skip(darwinFixtureSkip)
	}
	sym, err := symbolication.NewIOSSymbolicator(darwinBinary)
	if err != nil {
		t.Fatal(err)
	}
	defer sym.Close()

	pc := callReturnAddress(t, sym)
	load := sym.TextVMAddr()
	const u = "11111111-2222-3333-4444-555555555555"
	images := []ingest.ProfileImage{{UUID: u, Name: "hello", Load: fmt.Sprintf("0x%x", load), App: true}}
	r := NewIOSResolver(images, map[string]*symbolication.IOSSymbolicator{u: sym})
	frame := ingest.ProfileFrame{A: fmt.Sprintf("0x%x", pc), I: intp(0)}

	wantCall := sym.SymbolicateAddress(pc - 1)
	wantNext := sym.SymbolicateAddress(pc)

	asCaller := r.Resolve(frame, true)
	asLeaf := r.Resolve(frame, false)
	if len(asCaller) != 1 || len(asLeaf) != 1 {
		t.Fatalf("caller=%+v leaf=%+v", asCaller, asLeaf)
	}
	if asCaller[0].Line != wantCall.Line || !asCaller[0].Resolved || !asCaller[0].IsApp {
		t.Errorf("caller = %+v, want call line %d", asCaller[0], wantCall.Line)
	}
	if asLeaf[0].Line != wantNext.Line {
		t.Errorf("leaf = %+v, want next line %d", asLeaf[0], wantNext.Line)
	}
	if !strings.HasPrefix(asCaller[0].Function, "main.") {
		t.Errorf("function = %q, want a main.* DWARF function", asCaller[0].Function)
	}
}
