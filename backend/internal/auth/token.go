package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Issuer signs and verifies short-lived access tokens: compact JWS, HS256,
// implemented on the standard library (no network-fetched dependency).
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

func NewIssuer(secret string, accessTTL time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), ttl: accessTTL}
}

var b64 = base64.RawURLEncoding

const jwtHeader = `{"alg":"HS256","typ":"JWT"}`

type claims struct {
	Sub string `json:"sub"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

func (i *Issuer) sign(signingInput string) []byte {
	m := hmac.New(sha256.New, i.secret)
	m.Write([]byte(signingInput))
	return m.Sum(nil)
}

func (i *Issuer) IssueAccess(userID int64) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(i.ttl)
	payload, err := json.Marshal(claims{Sub: strconv.FormatInt(userID, 10), Iat: now.Unix(), Exp: exp.Unix()})
	if err != nil {
		return "", time.Time{}, err
	}
	input := b64.EncodeToString([]byte(jwtHeader)) + "." + b64.EncodeToString(payload)
	return input + "." + b64.EncodeToString(i.sign(input)), exp, nil
}

var errInvalidToken = errors.New("invalid token")

func (i *Issuer) VerifyAccess(token string) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, errInvalidToken
	}
	hdrRaw, err := b64.DecodeString(parts[0])
	if err != nil {
		return 0, errInvalidToken
	}
	var hdr struct {
		Alg string `json:"alg"`
	}
	if json.Unmarshal(hdrRaw, &hdr) != nil || hdr.Alg != "HS256" {
		return 0, errInvalidToken // rejects alg=none and algorithm confusion
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, i.sign(parts[0]+"."+parts[1])) {
		return 0, errInvalidToken
	}
	payload, err := b64.DecodeString(parts[1])
	if err != nil {
		return 0, errInvalidToken
	}
	var c claims
	if json.Unmarshal(payload, &c) != nil || c.Exp == 0 || time.Now().Unix() >= c.Exp {
		return 0, errors.New("token expired or malformed")
	}
	id, err := strconv.ParseInt(c.Sub, 10, 64)
	if err != nil {
		return 0, errInvalidToken
	}
	return id, nil
}

// NewRefreshToken returns an opaque random token and its SHA-256 hash; only
// the hash is stored.
func NewRefreshToken() (plain, hash string, err error) {
	plain, err = randomString(32)
	if err != nil {
		return "", "", err
	}
	return plain, HashToken(plain), nil
}

func HashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func randomString(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
