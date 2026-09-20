// Package secret encrypts values the control plane must be able to read back.
//
// Bearer tokens are hashed, not encrypted, because the server only needs to
// recognize them. A user's GitHub credential is different: the server has to
// present it to GitHub, so it must be recoverable. That is what this package
// is for, and it is only appropriate on the server, which has somewhere to
// keep a key. The CLI has no such place, which is why it stores its token in
// a 0600 file and says so.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// KeySize is the required key length: AES-256.
const KeySize = 32

// ErrNoKey means the server has no encryption key configured.
var ErrNoKey = errors.New("no secret key")

// Key encrypts and decrypts stored credentials.
type Key struct {
	aead cipher.AEAD
}

// NewKey builds a Key from 32 raw bytes.
func NewKey(raw []byte) (*Key, error) {
	if len(raw) != KeySize {
		return nil, fmt.Errorf("the secret key must be %d bytes, got %d", KeySize, len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build AES-GCM: %w", err)
	}
	return &Key{aead: aead}, nil
}

// ParseKey reads a key encoded as hex or base64.
//
// Both are accepted because operators paste whichever their password manager
// or secret store produced, and guessing wrong would be a confusing failure.
func ParseKey(encoded string) (*Key, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, ErrNoKey
	}

	if raw, err := hex.DecodeString(encoded); err == nil && len(raw) == KeySize {
		return NewKey(raw)
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if raw, err := enc.DecodeString(encoded); err == nil && len(raw) == KeySize {
			return NewKey(raw)
		}
	}
	return nil, fmt.Errorf("the secret key is not %d bytes of hex or base64.\n"+
		"Generate one with `runnerly server keygen`", KeySize)
}

// GenerateKey returns a new hex-encoded key.
func GenerateKey() (string, error) {
	raw := make([]byte, KeySize)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// Encrypt seals a value. The nonce is random per call and stored in front of
// the ciphertext, so encrypting the same value twice never produces the same
// bytes.
func (k *Key) Encrypt(plaintext []byte) ([]byte, error) {
	if k == nil {
		return nil, ErrNoKey
	}
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return k.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a value sealed by Encrypt.
func (k *Key) Decrypt(sealed []byte) ([]byte, error) {
	if k == nil {
		return nil, ErrNoKey
	}
	size := k.aead.NonceSize()
	if len(sealed) < size {
		return nil, errors.New("the stored value is too short to be encrypted data")
	}
	plaintext, err := k.aead.Open(nil, sealed[:size], sealed[size:], nil)
	if err != nil {
		// Do not repeat the cipher's message: to a caller it is always
		// either a wrong key or tampering, and both need the same action.
		return nil, errors.New("the stored value could not be decrypted.\n" +
			"The secret key has probably changed; credentials sealed with the old one are unreadable")
	}
	return plaintext, nil
}

// EncryptString is Encrypt for text.
func (k *Key) EncryptString(s string) ([]byte, error) { return k.Encrypt([]byte(s)) }

// DecryptString is Decrypt for text.
func (k *Key) DecryptString(sealed []byte) (string, error) {
	out, err := k.Decrypt(sealed)
	return string(out), err
}
