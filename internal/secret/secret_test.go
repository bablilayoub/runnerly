package secret

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func testKey(t *testing.T) *Key {
	t.Helper()
	encoded, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	k, err := ParseKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestRoundTrip(t *testing.T) {
	k := testKey(t)
	const plaintext = "ghp_a_real_looking_token"

	sealed, err := k.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}
	if bytes.Contains(sealed, []byte(plaintext)) {
		t.Error("the plaintext is visible in the ciphertext")
	}

	got, err := k.DecryptString(sealed)
	if err != nil {
		t.Fatalf("DecryptString() error = %v", err)
	}
	if got != plaintext {
		t.Errorf("round trip = %q, want %q", got, plaintext)
	}
}

func TestEncryptingTwiceGivesDifferentBytes(t *testing.T) {
	k := testKey(t)
	first, err := k.EncryptString("same value")
	if err != nil {
		t.Fatal(err)
	}
	second, err := k.EncryptString("same value")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Error("the same plaintext sealed to identical bytes; the nonce is not random")
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	k := testKey(t)
	sealed, err := k.EncryptString("secret")
	if err != nil {
		t.Fatal(err)
	}

	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := k.Decrypt(tampered); err == nil {
		t.Error("Decrypt() accepted tampered ciphertext")
	}

	if _, err := k.Decrypt([]byte("short")); err == nil {
		t.Error("Decrypt() accepted a value too short to be ciphertext")
	}
}

func TestDecryptWithTheWrongKeyIsExplained(t *testing.T) {
	sealed, err := testKey(t).EncryptString("secret")
	if err != nil {
		t.Fatal(err)
	}
	_, err = testKey(t).Decrypt(sealed)
	if err == nil {
		t.Fatal("Decrypt() succeeded with a different key")
	}
	if !strings.Contains(err.Error(), "secret key has probably changed") {
		t.Errorf("the error should explain the likely cause, got: %v", err)
	}
}

func TestParseKeyAcceptsHexAndBase64(t *testing.T) {
	raw := make([]byte, KeySize)
	for i := range raw {
		raw[i] = byte(i)
	}

	encodings := map[string]string{
		"hex":             hex.EncodeToString(raw),
		"base64":          base64.StdEncoding.EncodeToString(raw),
		"raw base64":      base64.RawStdEncoding.EncodeToString(raw),
		"url base64":      base64.URLEncoding.EncodeToString(raw),
		"with whitespace": "  " + hex.EncodeToString(raw) + "\n",
	}
	for name, encoded := range encodings {
		t.Run(name, func(t *testing.T) {
			k, err := ParseKey(encoded)
			if err != nil {
				t.Fatalf("ParseKey() error = %v", err)
			}
			sealed, err := k.EncryptString("x")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := k.Decrypt(sealed); err != nil {
				t.Errorf("the parsed key does not work: %v", err)
			}
		})
	}
}

func TestParseKeyRejectsBadInput(t *testing.T) {
	if _, err := ParseKey(""); !errors.Is(err, ErrNoKey) {
		t.Errorf("ParseKey(\"\") = %v, want ErrNoKey", err)
	}
	// The right encoding but the wrong length.
	short := hex.EncodeToString(make([]byte, 16))
	if _, err := ParseKey(short); err == nil {
		t.Error("ParseKey() accepted a 16-byte key")
	}
	if _, err := ParseKey("not a key at all"); err == nil {
		t.Error("ParseKey() accepted nonsense")
	}
	if _, err := ParseKey(hex.EncodeToString(make([]byte, 16))); err != nil &&
		!strings.Contains(err.Error(), "server keygen") {
		t.Errorf("the error should say how to make one, got: %v", err)
	}
}

func TestNewKeyRequiresTheRightLength(t *testing.T) {
	if _, err := NewKey(make([]byte, 16)); err == nil {
		t.Error("NewKey() accepted a short key")
	}
	if _, err := NewKey(make([]byte, KeySize)); err != nil {
		t.Errorf("NewKey() error = %v", err)
	}
}

func TestGenerateKeyIsRandomAndUsable(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		encoded, err := GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) != KeySize*2 {
			t.Fatalf("key is %d characters, want %d hex characters", len(encoded), KeySize*2)
		}
		if seen[encoded] {
			t.Fatal("GenerateKey() repeated a key")
		}
		seen[encoded] = true
	}
}

func TestNilKeyIsRefusedRatherThanCrashing(t *testing.T) {
	var k *Key
	if _, err := k.Encrypt([]byte("x")); !errors.Is(err, ErrNoKey) {
		t.Errorf("Encrypt() on a nil key = %v, want ErrNoKey", err)
	}
	if _, err := k.Decrypt([]byte("x")); !errors.Is(err, ErrNoKey) {
		t.Errorf("Decrypt() on a nil key = %v, want ErrNoKey", err)
	}
}
