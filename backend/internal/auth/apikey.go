package auth

import (
	"fmt"
	"strings"
)

type KeyType string

const (
	KeyPublicClient KeyType = "public_client"
	KeySecretAdmin  KeyType = "secret_admin"
	KeyMCP          KeyType = "mcp"
)

var keyPrefixes = map[KeyType]string{
	KeyPublicClient: "art_live_",
	KeySecretAdmin:  "art_sec_",
	KeyMCP:          "art_mcp_",
}

// GenerateKey creates a key: type prefix + 32 random bytes (base64url).
// prefix is the non-secret display form (type prefix + first 8 random chars).
func GenerateKey(t KeyType) (plain, hash, prefix string, err error) {
	tp, ok := keyPrefixes[t]
	if !ok {
		return "", "", "", fmt.Errorf("unknown key type %q", t)
	}
	body, err := randomString(32)
	if err != nil {
		return "", "", "", err
	}
	plain = tp + body
	return plain, HashKey(plain), tp + body[:8], nil
}

// HashKey is deterministic: keys are high-entropy so plain SHA-256 suffices.
func HashKey(plain string) string { return HashToken(plain) }

func KeyTypeOf(plain string) (KeyType, bool) {
	for t, p := range keyPrefixes {
		if strings.HasPrefix(plain, p) {
			return t, true
		}
	}
	return "", false
}
