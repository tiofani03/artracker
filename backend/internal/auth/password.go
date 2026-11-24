package auth

import (
	"errors"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

const (
	MinPasswordLen   = 10
	MaxPasswordBytes = 72 // bcrypt silently ignores anything past 72 bytes, so reject instead of truncating.

	bcryptCost = 12
)

var ErrPasswordPolicy = errors.New("password must be 10 to 72 bytes long")

// ValidatePassword enforces the length policy. Length is measured in bytes, the
// unit bcrypt truncates by.
func ValidatePassword(plain string) error {
	if len(plain) < MinPasswordLen || len(plain) > MaxPasswordBytes {
		return ErrPasswordPolicy
	}
	return nil
}

func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// CheckPassword reports whether plain matches hash. An empty or malformed hash
// never matches.
func CheckPassword(hash, plain string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

var (
	dummyOnce sync.Once
	dummyHash string
)

// DummyCheck burns one bcrypt compare so a login for an unknown email takes as
// long as one for a known email.
func DummyCheck(plain string) {
	dummyOnce.Do(func() { dummyHash, _ = HashPassword("dummy-password-for-timing") })
	_ = CheckPassword(dummyHash, plain)
}
