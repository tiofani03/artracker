package symbolication

import (
	"debug/dwarf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// darwinFixture is a Mach-O arm64 binary with DWARF built from testdata/hello.
// It is nil (and darwinFixtureSkip is set) when the build is not possible.
var (
	darwinFixture     *IOSSymbolicator
	darwinFixtureSkip string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "symbolication-hello-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdir temp:", err)
		os.Exit(1)
	}

	darwinFixture, darwinFixtureSkip = buildDarwinFixture(dir)
	code := m.Run()

	if darwinFixture != nil {
		_ = darwinFixture.Close()
	}
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func buildDarwinFixture(dir string) (*IOSSymbolicator, string) {
	out := filepath.Join(dir, "hello")
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", out, "./testdata/hello")
	cmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Sprintf("cannot build darwin fixture: %v\n%s", err, b)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Sprintf("cannot read darwin fixture: %v", err)
	}
	sym, err := NewIOSSymbolicator(data)
	if err != nil {
		return nil, fmt.Sprintf("cannot load darwin fixture: %v", err)
	}
	return sym, ""
}

// requireDarwinFixture returns the shared fixture symbolicator, skipping the
// test when it could not be built in this environment.
func requireDarwinFixture(t *testing.T) *IOSSymbolicator {
	t.Helper()
	if darwinFixture == nil {
		t.Skip(darwinFixtureSkip)
	}
	return darwinFixture
}

// functionAddr returns the low PC of the named subprogram in the fixture.
func functionAddr(t *testing.T, s *IOSSymbolicator, name string) uint64 {
	t.Helper()
	r := s.dwarfData.Reader()
	for {
		e, err := r.Next()
		if err != nil || e == nil {
			break
		}
		if e.Tag != dwarf.TagSubprogram {
			continue
		}
		if n, _ := e.Val(dwarf.AttrName).(string); n == name {
			if low, ok := e.Val(dwarf.AttrLowpc).(uint64); ok {
				return low
			}
		}
	}
	t.Fatalf("function %q not found in fixture DWARF", name)
	return 0
}

func TestIndexMatchesLinearScan(t *testing.T) {
	s := requireDarwinFixture(t)

	var text *struct{ addr, size uint64 }
	for _, sec := range s.machoFile.Sections {
		if sec.Seg == "__TEXT" && sec.Name == "__text" {
			text = &struct{ addr, size uint64 }{sec.Addr, sec.Size}
		}
	}
	if text == nil {
		t.Fatal("fixture has no __TEXT,__text section")
	}

	const samples = 500
	step := text.size / samples
	if step == 0 {
		step = 1
	}
	foundCount := 0
	for i := uint64(0); i < samples; i++ {
		pc := text.addr + i*step
		got := s.SymbolicateAddress(pc)
		want := s.symbolicateAddressLinear(pc)
		if got != want {
			t.Fatalf("pc %#x: index = %+v, linear = %+v", pc, got, want)
		}
		if got.Found {
			foundCount++
		}
	}
	if foundCount < samples/2 {
		t.Fatalf("only %d/%d sample addresses resolved; fixture DWARF looks unusable", foundCount, samples)
	}
}

func TestSymbolicateImageAddressSlide(t *testing.T) {
	s := requireDarwinFixture(t)

	fn := functionAddr(t, s, "main.second")
	const load = uint64(0x104000000)
	pc := load + (fn - s.TextVMAddr())

	got := s.SymbolicateImageAddress(pc, load)
	if !got.Found {
		t.Fatalf("SymbolicateImageAddress(%#x, %#x) not found", pc, load)
	}
	if !strings.Contains(got.Function, "main.second") {
		t.Fatalf("function = %q, want it to contain main.second", got.Function)
	}
	if got != s.SymbolicateAddress(fn) {
		t.Fatalf("slide result %+v differs from unslid lookup %+v", got, s.SymbolicateAddress(fn))
	}
}

func TestTextVMAddr(t *testing.T) {
	s := requireDarwinFixture(t)
	if s.TextVMAddr() == 0 {
		t.Fatal("TextVMAddr() = 0, want __TEXT segment vmaddr")
	}
}

func TestIndexBuiltOnce(t *testing.T) {
	s := requireDarwinFixture(t)
	// Fresh symbolicator so the shared fixture's index state does not matter.
	fresh, err := NewIOSSymbolicator(s.data)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()

	fn := functionAddr(t, fresh, "main.first")
	fresh.SymbolicateAddress(fn)
	fresh.SymbolicateAddress(fn + 4)
	if fresh.indexBuilds != 1 {
		t.Fatalf("index built %d times, want 1", fresh.indexBuilds)
	}
}

func TestSymbolicateImageAddressBelowLoadAddr(t *testing.T) {
	s := requireDarwinFixture(t)
	const load = uint64(0x104000000)
	for _, pc := range []uint64{load - 1, load - 0x1000, 0} {
		if got := s.SymbolicateImageAddress(pc, load); got.Found {
			t.Errorf("SymbolicateImageAddress(%#x, %#x) = %+v, want not found", pc, load, got)
		}
	}
}

func TestIndexBuildPanicYieldsEmptyIndex(t *testing.T) {
	s := requireDarwinFixture(t)
	fresh, err := NewIOSSymbolicator(s.data)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	fresh.buildIndexFn = func() *addressIndex { panic("malformed dwarf") }

	fn := functionAddr(t, fresh, "main.first")
	for i := 0; i < 2; i++ {
		if got := fresh.SymbolicateAddress(fn); got.Found {
			t.Fatalf("call %d: got %+v, want not found after failed index build", i, got)
		}
	}
	if fresh.indexBuilds != 1 {
		t.Fatalf("index built %d times, want 1", fresh.indexBuilds)
	}
}

func TestIndexConcurrent(t *testing.T) {
	s := requireDarwinFixture(t)
	fresh, err := NewIOSSymbolicator(s.data)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()

	fn := functionAddr(t, fresh, "main.second")
	want := s.SymbolicateAddress(fn)

	const workers = 16
	var wg sync.WaitGroup
	results := make([]ResolvedLocation, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = fresh.SymbolicateAddress(fn)
		}()
	}
	wg.Wait()

	for i, got := range results {
		if got != want {
			t.Errorf("worker %d: got %+v, want %+v", i, got, want)
		}
	}
	if fresh.indexBuilds != 1 {
		t.Fatalf("index built %d times, want 1", fresh.indexBuilds)
	}
}
