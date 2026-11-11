package alerts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSourcesCallOnlySendOnlyMethods is a cheap source-level guard for the
// product rule that the platform receives nothing from Telegram in tenant
// mode: production code calls only sendMessage and getMe there.
//
// It scans the non-test Go files of this package and of the handler package
// and fails when:
//   - any of them mentions getChat (it returns chat details such as pinned
//     messages and descriptions, so it must never come back), or
//   - getUpdates appears outside link.go and telegram.go, the shared-mode
//     files where PollUpdates (gated by Mode() == ModeShared at its one call
//     site) reads the link codes.
//
// It is a string match, not a proof: the runtime half of the guarantee is the
// strict Bot API fakes and the handler test that records every method called.
func TestSourcesCallOnlySendOnlyMethods(t *testing.T) {
	sharedModeFiles := map[string]bool{"link.go": true, "telegram.go": true}
	for _, dir := range []string{".", filepath.Join("..", "handler")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("no Go files found in %s", dir)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(src), "getChat") {
				t.Errorf("%s mentions getChat; the platform must not read chat details from Telegram", f)
			}
			if strings.Contains(string(src), "getUpdates") && !(dir == "." && sharedModeFiles[filepath.Base(f)]) {
				t.Errorf("%s mentions getUpdates; only the shared-mode link flow may poll Telegram", f)
			}
		}
	}
}
