// Package security holds credential hashing and at-rest encryption.
//
// Two different primitives are used on purpose:
//
//   - Provider credentials must be replayable by the gateway, so they are
//     encrypted with AES-256-GCM under APP_SECRET.
//   - Client API keys must never be recoverable, so only an HMAC-SHA256 of the
//     key is stored, alongside a short non-secret prefix used for lookup.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ErrDecrypt reports a ciphertext that does not authenticate under the current
// APP_SECRET (wrong secret, or tampered row).
var ErrDecrypt = errors.New("credential could not be decrypted with the configured APP_SECRET")

// HashKey returns the hex HMAC-SHA256 of a client key.
func HashKey(secret []byte, key string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(key))
	return hex.EncodeToString(mac.Sum(nil))
}

// EqualKey compares two hex digests without leaking a timing signal.
func EqualKey(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// NewClientKey mints a key and its lookup prefix. The returned plaintext is the
// only copy that will ever exist.
func NewClientKey() (plaintext, prefix string, err error) {
	buf := make([]byte, 24)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	// "atk" makes keys greppable in logs and secret scanners.
	plaintext = "atk_" + strings.TrimRight(base64.RawURLEncoding.EncodeToString(buf), "=")
	prefix = plaintext[:12]
	return plaintext, prefix, nil
}

// Encrypt seals plaintext with AES-256-GCM. The nonce is prefixed to the
// ciphertext so a single column is enough to store the value.
func Encrypt(secret []byte, plaintext string) (string, error) {
	gcm, err := aead(secret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a value produced by Encrypt.
func Decrypt(secret []byte, encoded string) (string, error) {
	gcm, err := aead(secret)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrDecrypt
	}
	if len(raw) < gcm.NonceSize() {
		return "", ErrDecrypt
	}
	nonce, body := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	opened, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(opened), nil
}

// Fingerprint is a short, non-reversible label used to show that a credential
// exists (and changed) without revealing it.
func Fingerprint(secret []byte, value string) string {
	sum := sha256.Sum256(append(append([]byte{}, secret...), value...))
	return hex.EncodeToString(sum[:])[:10]
}

func aead(secret []byte) (cipher.AEAD, error) {
	// Derive a fixed-length key so operators may supply a passphrase.
	material := sha256.Sum256(secret)
	block, err := aes.NewCipher(material[:])
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	return cipher.NewGCM(block)
}

// RandomToken returns a URL-safe token for sessions and bootstrap secrets.
func RandomToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(buf), "="), nil
}
