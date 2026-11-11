package alerts

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Sealer encrypts secrets (bot tokens) at rest, bound to an organisation.
// It is an interface so a cloud KMS can replace the local master key later.
type Sealer interface {
	// Seal encrypts plaintext for orgID and returns the blob plus the id of
	// the master key that wrapped it.
	Seal(orgID int64, plaintext []byte) (blob []byte, keyID string, err error)
	// Open reverses Seal. Every failure is ErrSealOpen.
	Open(orgID int64, keyID string, blob []byte) ([]byte, error)
}

// ErrSealOpen is the only error Open returns. It deliberately carries no
// cause, so it cannot leak material or serve as an oracle.
var ErrSealOpen = errors.New("alerts: cannot open sealed value")

// Blob layout:
//
//	version(1) | wrapNonce(12) | wrappedDEK(32+16) | dataNonce(12) | ciphertext+tag(16)
const (
	sealVersion    byte = 0x01
	sealKeyLen          = 32
	sealNonceLen        = 12
	sealTagLen          = 16
	sealWrappedLen      = sealKeyLen + sealTagLen

	sealWrapNonceOff = 1
	sealWrappedOff   = sealWrapNonceOff + sealNonceLen
	sealDataNonceOff = sealWrappedOff + sealWrappedLen
	sealCipherOff    = sealDataNonceOff + sealNonceLen
	sealMinLen       = sealCipherOff + sealTagLen // 89: empty plaintext
)

// LocalSealer is envelope encryption under a master key held in memory: a
// fresh random data key per Seal, wrapped by the master key with AES-256-GCM,
// and the plaintext sealed by the data key with AES-256-GCM. Both layers bind
// the organisation id and key id as associated data. Safe for concurrent use.
type LocalSealer struct {
	master cipher.AEAD
	keyID  string
}

var _ Sealer = (*LocalSealer)(nil)

// NewLocalSealer builds a sealer from a 32-byte master key. The key is copied;
// the caller may wipe its slice afterwards.
func NewLocalSealer(masterKey []byte, keyID string) (*LocalSealer, error) {
	if len(masterKey) != sealKeyLen {
		return nil, errors.New("alerts: invalid master key: must be 32 bytes")
	}
	if keyID == "" {
		return nil, errors.New("alerts: master key id is empty")
	}
	k := make([]byte, sealKeyLen)
	copy(k, masterKey)
	aead, err := newGCM(k)
	for i := range k {
		k[i] = 0
	}
	if err != nil {
		return nil, errors.New("alerts: cannot initialise master key cipher")
	}
	return &LocalSealer{master: aead, keyID: keyID}, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealAAD(orgID int64, keyID string) []byte {
	return []byte("org:" + strconv.FormatInt(orgID, 10) + "|key:" + keyID)
}

func (s *LocalSealer) Seal(orgID int64, plaintext []byte) ([]byte, string, error) {
	dek := make([]byte, sealKeyLen)
	defer func() {
		for i := range dek {
			dek[i] = 0
		}
	}()
	blob := make([]byte, sealCipherOff, sealMinLen+len(plaintext))
	blob[0] = sealVersion
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, "", fmt.Errorf("alerts: seal: random data key: %w", err)
	}
	if _, err := io.ReadFull(rand.Reader, blob[sealWrapNonceOff:sealWrappedOff]); err != nil {
		return nil, "", fmt.Errorf("alerts: seal: random nonce: %w", err)
	}
	if _, err := io.ReadFull(rand.Reader, blob[sealDataNonceOff:sealCipherOff]); err != nil {
		return nil, "", fmt.Errorf("alerts: seal: random nonce: %w", err)
	}
	aad := sealAAD(orgID, s.keyID)
	s.master.Seal(blob[sealWrappedOff:sealWrappedOff], blob[sealWrapNonceOff:sealWrappedOff], dek, aad)
	data, err := newGCM(dek)
	if err != nil {
		return nil, "", errors.New("alerts: seal: cannot initialise data key cipher")
	}
	blob = data.Seal(blob, blob[sealDataNonceOff:sealCipherOff], plaintext, aad)
	return blob, s.keyID, nil
}

func (s *LocalSealer) Open(orgID int64, keyID string, blob []byte) ([]byte, error) {
	if keyID != s.keyID || len(blob) < sealMinLen || blob[0] != sealVersion {
		return nil, ErrSealOpen
	}
	aad := sealAAD(orgID, keyID)
	dek, err := s.master.Open(make([]byte, 0, sealKeyLen),
		blob[sealWrapNonceOff:sealWrappedOff], blob[sealWrappedOff:sealDataNonceOff], aad)
	if err != nil {
		return nil, ErrSealOpen
	}
	defer func() {
		for i := range dek {
			dek[i] = 0
		}
	}()
	data, err := newGCM(dek)
	if err != nil {
		return nil, ErrSealOpen
	}
	pt, err := data.Open(nil, blob[sealDataNonceOff:sealCipherOff], blob[sealCipherOff:], aad)
	if err != nil {
		return nil, ErrSealOpen
	}
	if pt == nil {
		pt = []byte{}
	}
	return pt, nil
}
