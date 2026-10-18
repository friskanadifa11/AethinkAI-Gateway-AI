package security

import (
	"strings"
	"testing"
)

// The two primitives here carry the whole credential story: provider secrets
// must be recoverable, client keys must not be.

func TestEncryptDecryptRoundTrip(t *testing.T) {
	secret := []byte("a-secret-that-is-definitely-long-enough")
	plaintext := "sk-provider-credential-value"

	sealed, err := Encrypt(secret, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, plaintext) {
		t.Fatal("the ciphertext contains the plaintext")
	}

	opened, err := Decrypt(secret, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if opened != plaintext {
		t.Fatalf("expected %q, got %q", plaintext, opened)
	}
}

func TestDecryptRejectsWrongSecret(t *testing.T) {
	sealed, err := Encrypt([]byte("first-secret-long-enough-value"), "value")
	if err != nil {
		t.Fatal(err)
	}
	// A rotated APP_SECRET must surface as a clear failure, not garbage.
	if _, err := Decrypt([]byte("second-secret-long-enough-value"), sealed); err == nil {
		t.Fatal("decryption with the wrong secret must fail")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	secret := []byte("a-secret-that-is-definitely-long-enough")
	sealed, err := Encrypt(secret, "value")
	if err != nil {
		t.Fatal(err)
	}
	// Flip a character in the middle of the base64 body.
	body := []byte(sealed)
	body[len(body)/2] = body[len(body)/2] ^ 0x01
	if _, err := Decrypt(secret, string(body)); err == nil {
		t.Fatal("a tampered ciphertext must fail to authenticate")
	}
}

func TestEncryptIsNonDeterministic(t *testing.T) {
	secret := []byte("a-secret-that-is-definitely-long-enough")
	first, err := Encrypt(secret, "same-value")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encrypt(secret, "same-value")
	if err != nil {
		t.Fatal(err)
	}
	// A fresh nonce per call is what stops two providers sharing a credential
	// from being identifiable by comparing their stored rows.
	if first == second {
		t.Fatal("encrypting the same value twice produced identical output")
	}
}

func TestNewClientKeyShape(t *testing.T) {
	plaintext, prefix, err := NewClientKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plaintext, "atk_") {
		t.Errorf("keys must be greppable, got %q", plaintext)
	}
	if len(prefix) != 12 {
		t.Errorf("expected a 12-character lookup prefix, got %d", len(prefix))
	}
	if !strings.HasPrefix(plaintext, prefix) {
		t.Error("the prefix must be the leading characters of the key")
	}

	other, _, err := NewClientKey()
	if err != nil {
		t.Fatal(err)
	}
	if other == plaintext {
		t.Fatal("two generated keys were identical")
	}
}

func TestHashKeyIsStableAndKeyed(t *testing.T) {
	a := []byte("secret-number-one-long-enough-value")
	b := []byte("secret-number-two-long-enough-value")
	key := "atk_exampleKeyValue"

	if HashKey(a, key) != HashKey(a, key) {
		t.Fatal("hashing must be deterministic, or no stored key would ever verify")
	}
	if HashKey(a, key) == HashKey(b, key) {
		t.Fatal("the hash must be keyed: otherwise a leaked table is crackable offline")
	}
}

func TestEqualKey(t *testing.T) {
	if !EqualKey("abc", "abc") {
		t.Error("identical digests must compare equal")
	}
	if EqualKey("abc", "abd") {
		t.Error("different digests must not compare equal")
	}
	if EqualKey("abc", "abcd") {
		t.Error("different lengths must not compare equal")
	}
}

func TestFingerprintIsShortAndKeyed(t *testing.T) {
	secret := []byte("a-secret-that-is-definitely-long-enough")
	fp := Fingerprint(secret, "sk-credential")
	if len(fp) != 10 {
		t.Errorf("expected a 10-character fingerprint, got %d", len(fp))
	}
	if fp == Fingerprint([]byte("another-secret-long-enough-value"), "sk-credential") {
		t.Error("the fingerprint must depend on the secret")
	}
}

func TestBcryptRoundTrip(t *testing.T) {
	hash, err := Bcrypt("correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if !BcryptCompare(hash, "correct-horse-battery") {
		t.Fatal("the correct password must verify")
	}
	if BcryptCompare(hash, "wrong-password") {
		t.Fatal("an incorrect password must not verify")
	}
	// A malformed stored hash is a failed comparison, never a panic.
	if BcryptCompare("not-a-hash", "anything") {
		t.Fatal("a malformed hash must not verify")
	}
}

func TestRandomTokenIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		token, err := RandomToken(32)
		if err != nil {
			t.Fatal(err)
		}
		if seen[token] {
			t.Fatal("RandomToken repeated a value")
		}
		seen[token] = true
	}
}
