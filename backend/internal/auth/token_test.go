package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestIssueVerifyAccess(t *testing.T) {
	i := NewIssuer("secret", time.Minute)
	tok, exp, err := i.IssueAccess(42)
	if err != nil {
		t.Fatal(err)
	}
	if !exp.After(time.Now()) {
		t.Error("expiry should be in the future")
	}
	id, err := i.VerifyAccess(tok)
	if err != nil || id != 42 {
		t.Fatalf("VerifyAccess = %d, %v", id, err)
	}
}

func TestVerifyAccessRejectsExpired(t *testing.T) {
	i := NewIssuer("secret", -time.Minute)
	tok, _, _ := i.IssueAccess(1)
	if _, err := i.VerifyAccess(tok); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestVerifyAccessRejectsWrongSecret(t *testing.T) {
	tok, _, _ := NewIssuer("a", time.Minute).IssueAccess(1)
	if _, err := NewIssuer("b", time.Minute).VerifyAccess(tok); err == nil {
		t.Fatal("token signed with another secret accepted")
	}
}

func TestVerifyAccessRejectsAlgNone(t *testing.T) {
	enc := base64.RawURLEncoding.EncodeToString
	tok := enc([]byte(`{"alg":"none","typ":"JWT"}`)) + "." +
		enc([]byte(`{"sub":"1","exp":9999999999}`)) + "."
	if _, err := NewIssuer("secret", time.Minute).VerifyAccess(tok); err == nil {
		t.Fatal("alg=none token accepted")
	}
}

func TestNewRefreshTokenHashMatches(t *testing.T) {
	plain, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if hash != HashToken(plain) || hash == plain {
		t.Error("hash must equal HashToken(plain) and differ from plain")
	}
	p2, _, _ := NewRefreshToken()
	if p2 == plain {
		t.Error("tokens must be random")
	}
	if strings.ContainsAny(plain, "+/=") {
		t.Error("token must be base64url without padding")
	}
}
