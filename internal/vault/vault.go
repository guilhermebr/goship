// Package vault provides AES-256-GCM encryption for secret environment variables.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const encryptedPrefix = "encrypted:"

// ErrNotEncrypted is returned when trying to decrypt a value that is not encrypted.
var ErrNotEncrypted = errors.New("value is not encrypted")

// Vault provides AES-256-GCM encryption and decryption.
type Vault struct {
	aead cipher.AEAD
}

// New creates a new Vault with the given master key.
// The master key is hashed with SHA-256 to produce a 32-byte AES key.
func New(masterKey []byte) (*Vault, error) {
	if len(masterKey) == 0 {
		return nil, errors.New("master key must not be empty")
	}

	key := sha256.Sum256(masterKey)

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &Vault{aead: aead}, nil
}

// Encrypt encrypts plaintext and returns "encrypted:" + base64(nonce+ciphertext).
func (v *Vault) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := v.aead.Seal(nonce, nonce, []byte(plaintext), nil)

	return encryptedPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a value produced by Encrypt.
// Returns ErrNotEncrypted if the value does not have the "encrypted:" prefix.
func (v *Vault) Decrypt(value string) (string, error) {
	if !IsEncrypted(value) {
		return "", ErrNotEncrypted
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encryptedPrefix))
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	nonceSize := v.aead.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]

	plaintext, err := v.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted returns true if the value has the "encrypted:" prefix.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, encryptedPrefix)
}
