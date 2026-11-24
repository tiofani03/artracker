package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndCheck(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if h == "correct horse battery" || h == "" {
		t.Fatalf("hash must not be the plaintext: %q", h)
	}
	if !CheckPassword(h, "correct horse battery") {
		t.Fatal("right password must verify")
	}
	if CheckPassword(h, "correct horse batterY") {
		t.Fatal("wrong password must not verify")
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name string
		pw   string
		ok   bool
	}{
		{"nine chars", strings.Repeat("a", 9), false},
		{"ten chars", strings.Repeat("a", 10), true},
		{"72 bytes", strings.Repeat("a", 72), true},
		{"73 bytes", strings.Repeat("a", 73), false},
		// 37 two-byte runes = 74 bytes: short in runes, over the limit in bytes.
		{"multibyte over 72 bytes", strings.Repeat("é", 37), false},
		// 10 two-byte runes = 20 bytes: fine.
		{"multibyte ok", strings.Repeat("é", 10), true},
	}
	for _, c := range cases {
		err := ValidatePassword(c.pw)
		if c.ok && err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if !c.ok && !errors.Is(err, ErrPasswordPolicy) {
			t.Errorf("%s: want ErrPasswordPolicy, got %v", c.name, err)
		}
	}
}

func TestCheckPasswordEmptyHash(t *testing.T) {
	if CheckPassword("", "x") {
		t.Fatal("empty hash must never verify")
	}
	if CheckPassword("not-a-bcrypt-hash", "x") {
		t.Fatal("garbage hash must never verify")
	}
}

func TestDummyCheckDoesNotPanic(t *testing.T) {
	DummyCheck("whatever")
}
