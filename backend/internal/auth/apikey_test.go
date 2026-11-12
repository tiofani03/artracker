package auth

import (
	"strings"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	cases := []struct {
		kt     KeyType
		prefix string
	}{
		{KeyPublicClient, "art_live_"},
		{KeySecretAdmin, "art_sec_"},
	}
	for _, c := range cases {
		plain, hash, prefix, err := GenerateKey(c.kt)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(plain, c.prefix) {
			t.Errorf("%s key %q lacks prefix %q", c.kt, plain, c.prefix)
		}
		if hash != HashKey(plain) || hash == plain || strings.Contains(hash, plain) {
			t.Error("hash must be HashKey(plain) and not expose plain")
		}
		if !strings.HasPrefix(plain, prefix) || prefix == plain || len(prefix) != len(c.prefix)+8 {
			t.Errorf("bad display prefix %q for %q", prefix, plain)
		}
		if got, ok := KeyTypeOf(plain); !ok || got != c.kt {
			t.Errorf("KeyTypeOf = %v, %v", got, ok)
		}
	}
	a, _, _, _ := GenerateKey(KeyPublicClient)
	b, _, _, _ := GenerateKey(KeyPublicClient)
	if a == b {
		t.Error("keys must be random")
	}
	if _, ok := KeyTypeOf("foo_abc"); ok {
		t.Error("KeyTypeOf(foo_abc) should be false")
	}
	if _, _, _, err := GenerateKey(KeyType("nope")); err == nil {
		t.Error("unknown key type should error")
	}
}

func TestGenerateKeyMCPPrefix(t *testing.T) {
	plain, _, _, err := GenerateKey(KeyMCP)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, "art_mcp_") {
		t.Fatalf("mcp key %q lacks art_mcp_ prefix", plain)
	}
	if got, ok := KeyTypeOf(plain); !ok || got != KeyMCP {
		t.Fatalf("KeyTypeOf = %q,%v want mcp", got, ok)
	}
}
