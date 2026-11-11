package alerts

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func testMasterKey(fill byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = fill + byte(i)
	}
	return k
}

func newTestSealer(t *testing.T) *LocalSealer {
	t.Helper()
	s, err := NewLocalSealer(testMasterKey(1), "k1")
	if err != nil {
		t.Fatalf("NewLocalSealer: %v", err)
	}
	return s
}

func sealOrFatal(t *testing.T, s Sealer, orgID int64, pt []byte) ([]byte, string) {
	t.Helper()
	blob, keyID, err := s.Seal(orgID, pt)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return blob, keyID
}

func assertOpenFails(t *testing.T, s Sealer, orgID int64, keyID string, blob []byte, what string) {
	t.Helper()
	pt, err := s.Open(orgID, keyID, blob)
	if err != ErrSealOpen || !errors.Is(err, ErrSealOpen) {
		t.Fatalf("%s: err = %v, want exactly ErrSealOpen", what, err)
	}
	if pt != nil {
		t.Fatalf("%s: plaintext returned on failure", what)
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	s := newTestSealer(t)
	for _, n := range []int{0, 1, 15, 16, 17, 46, 255, 4096} {
		pt := bytes.Repeat([]byte{0xA5}, n)
		for i := range pt {
			pt[i] = byte(i * 7)
		}
		blob, keyID := sealOrFatal(t, s, 42, pt)
		if keyID != "k1" {
			t.Fatalf("keyID = %q, want k1", keyID)
		}
		if want := sealMinLen + n; len(blob) != want {
			t.Fatalf("size %d: blob len = %d, want %d", n, len(blob), want)
		}
		if blob[0] != 0x01 {
			t.Fatalf("version byte = %#x, want 0x01", blob[0])
		}
		got, err := s.Open(42, keyID, blob)
		if err != nil {
			t.Fatalf("size %d: Open: %v", n, err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("size %d: round trip mismatch", n)
		}
	}
}

func TestSealRandomised(t *testing.T) {
	s := newTestSealer(t)
	pt := []byte("123456:same-token")
	a, _ := sealOrFatal(t, s, 1, pt)
	b, _ := sealOrFatal(t, s, 1, pt)
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same plaintext produced identical blobs")
	}
	if bytes.Equal(a[1:13], b[1:13]) || bytes.Equal(a[61:73], b[61:73]) {
		t.Fatal("nonce reused across seals")
	}
	if bytes.Equal(a[13:61], b[13:61]) {
		t.Fatal("wrapped data key identical across seals")
	}
}

func TestOpenRejectsOtherOrg(t *testing.T) {
	s := newTestSealer(t)
	blob, keyID := sealOrFatal(t, s, 1, []byte("token"))
	assertOpenFails(t, s, 2, keyID, blob, "other org")
	assertOpenFails(t, s, -1, keyID, blob, "negative org")
	assertOpenFails(t, s, 11, keyID, blob, "org 11")
}

func TestOpenRejectsTamper(t *testing.T) {
	s := newTestSealer(t)
	blob, keyID := sealOrFatal(t, s, 7, []byte("123456789:AAbbCCddEEff"))
	regions := []struct {
		name string
		off  int
	}{
		{"version", 0},
		{"wrapNonce", 1},
		{"wrappedDEK", 13},
		{"dataNonce", 61},
		{"ciphertext", 73},
		{"tag", len(blob) - 1},
	}
	for _, r := range regions {
		bad := append([]byte(nil), blob...)
		bad[r.off] ^= 0x01
		assertOpenFails(t, s, 7, keyID, bad, "flip in "+r.name)
	}
	for i := range blob {
		for _, mask := range []byte{0x01, 0x80, 0xFF} {
			bad := append([]byte(nil), blob...)
			bad[i] ^= mask
			assertOpenFails(t, s, 7, keyID, bad, fmt.Sprintf("flip byte %d mask %#x", i, mask))
		}
	}
	// Appending bytes is also rejected.
	assertOpenFails(t, s, 7, keyID, append(append([]byte(nil), blob...), 0), "appended byte")
	// The original still opens.
	if _, err := s.Open(7, keyID, blob); err != nil {
		t.Fatalf("original blob no longer opens: %v", err)
	}
}

func TestOpenRejectsWrongKeyID(t *testing.T) {
	s := newTestSealer(t)
	blob, _ := sealOrFatal(t, s, 1, []byte("token"))
	assertOpenFails(t, s, 1, "k2", blob, "wrong keyID")
	assertOpenFails(t, s, 1, "", blob, "empty keyID")
	assertOpenFails(t, s, 1, "k1 ", blob, "keyID with suffix")
}

func TestOpenRejectsWrongMasterKey(t *testing.T) {
	s := newTestSealer(t)
	blob, keyID := sealOrFatal(t, s, 1, []byte("token"))
	other, err := NewLocalSealer(testMasterKey(99), "k1")
	if err != nil {
		t.Fatal(err)
	}
	assertOpenFails(t, other, 1, keyID, blob, "wrong master key")
}

func TestOpenRejectsTruncated(t *testing.T) {
	s := newTestSealer(t)
	blob, keyID := sealOrFatal(t, s, 1, []byte("123456789:token-value"))
	assertOpenFails(t, s, 1, keyID, nil, "nil blob")
	for _, n := range []int{0, 1, 12, 13, 61, 73, sealMinLen - 1, len(blob) - 1} {
		assertOpenFails(t, s, 1, keyID, blob[:n], fmt.Sprintf("truncated to %d", n))
	}
	// Minimum-length blob with an empty plaintext opens; shorter does not.
	empty, _ := sealOrFatal(t, s, 1, nil)
	if len(empty) != 89 {
		t.Fatalf("empty-plaintext blob len = %d, want 89", len(empty))
	}
	if _, err := s.Open(1, keyID, empty); err != nil {
		t.Fatalf("empty plaintext: %v", err)
	}
	assertOpenFails(t, s, 1, keyID, empty[:88], "88 bytes")
}

func TestBlobDoesNotContainPlaintext(t *testing.T) {
	s := newTestSealer(t)
	token := "8123456789:AAH-recognisable-SECRET-token-value"
	blob, _ := sealOrFatal(t, s, 1, []byte(token))
	if bytes.Contains(blob, []byte(token)) || bytes.Contains(blob, []byte("SECRET")) {
		t.Fatal("blob contains the plaintext")
	}
	if bytes.Contains(blob, testMasterKey(1)) {
		t.Fatal("blob contains the master key")
	}
}

func TestNewLocalSealerRejectsBadKey(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33, 64} {
		k := bytes.Repeat([]byte{0x5A}, n)
		s, err := NewLocalSealer(k, "k1")
		if err == nil || s != nil {
			t.Fatalf("key len %d: accepted", n)
		}
		if strings.Contains(err.Error(), "ZZZZ") {
			t.Fatalf("key len %d: error leaks key material: %v", n, err)
		}
	}
	if _, err := NewLocalSealer(nil, "k1"); err == nil {
		t.Fatal("nil key accepted")
	}
	if s, err := NewLocalSealer(testMasterKey(1), ""); err == nil || s != nil {
		t.Fatal("empty keyID accepted")
	}
}

func TestSealerDoesNotAliasMasterKey(t *testing.T) {
	key := testMasterKey(1)
	s, err := NewLocalSealer(key, "k1")
	if err != nil {
		t.Fatal(err)
	}
	blob, keyID := sealOrFatal(t, s, 1, []byte("token"))
	for i := range key {
		key[i] = 0
	}
	got, err := s.Open(1, keyID, blob)
	if err != nil || string(got) != "token" {
		t.Fatalf("Open after caller mutated key: %q, %v", got, err)
	}
	blob2, _ := sealOrFatal(t, s, 1, []byte("token2"))
	ref, _ := NewLocalSealer(testMasterKey(1), "k1")
	if got, err := ref.Open(1, "k1", blob2); err != nil || string(got) != "token2" {
		t.Fatalf("seal after caller mutated key not under the original key: %q, %v", got, err)
	}
}

func TestSealerConcurrentUse(t *testing.T) {
	s := newTestSealer(t)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				pt := []byte(fmt.Sprintf("goroutine-%d-iter-%d", g, i))
				org := int64(g + 1)
				blob, keyID, err := s.Seal(org, pt)
				if err != nil {
					errs <- err
					return
				}
				got, err := s.Open(org, keyID, blob)
				if err != nil {
					errs <- err
					return
				}
				if !bytes.Equal(got, pt) {
					errs <- fmt.Errorf("goroutine %d: mismatch", g)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestOpenErrorsAreOpaque(t *testing.T) {
	s := newTestSealer(t)
	blob, keyID := sealOrFatal(t, s, 1, []byte("token"))
	other, _ := NewLocalSealer(testMasterKey(99), "k1")
	badVersion := append([]byte(nil), blob...)
	badVersion[0] = 0x02
	badTag := append([]byte(nil), blob...)
	badTag[len(badTag)-1] ^= 0xFF
	badWrap := append([]byte(nil), blob...)
	badWrap[20] ^= 0xFF

	cases := []struct {
		name string
		err  error
	}{}
	add := func(name string, _ []byte, err error) {
		cases = append(cases, struct {
			name string
			err  error
		}{name, err})
	}
	pt, err := s.Open(2, keyID, blob)
	add("other org", pt, err)
	pt, err = s.Open(1, "k2", blob)
	add("wrong keyID", pt, err)
	pt, err = other.Open(1, keyID, blob)
	add("wrong master key", pt, err)
	pt, err = s.Open(1, keyID, badVersion)
	add("bad version", pt, err)
	pt, err = s.Open(1, keyID, badTag)
	add("bad tag", pt, err)
	pt, err = s.Open(1, keyID, badWrap)
	add("bad wrapped key", pt, err)
	pt, err = s.Open(1, keyID, nil)
	add("nil", pt, err)
	pt, err = s.Open(1, keyID, blob[:50])
	add("truncated", pt, err)

	for _, c := range cases {
		if c.err != ErrSealOpen {
			t.Fatalf("%s: err = %#v, want the ErrSealOpen value", c.name, c.err)
		}
		if c.err.Error() != "alerts: cannot open sealed value" {
			t.Fatalf("%s: err text = %q", c.name, c.err.Error())
		}
	}
}

var _ Sealer = (*LocalSealer)(nil)
