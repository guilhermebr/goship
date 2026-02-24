package vault

import (
	"errors"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	v, err := New([]byte("test-master-key"))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	plaintext := "my-secret-password"

	encrypted, err := v.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	if !IsEncrypted(encrypted) {
		t.Error("IsEncrypted() = false, want true")
	}

	decrypted, err := v.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypt() = %q, want %q", decrypted, plaintext)
	}
}

func TestIsEncrypted(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"encrypted:abc123", true},
		{"encrypted:", true},
		{"plaintext", false},
		{"", false},
		{"encrypt:abc", false},
	}

	for _, tt := range tests {
		if got := IsEncrypted(tt.value); got != tt.want {
			t.Errorf("IsEncrypted(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestDecryptWrongKey(t *testing.T) {
	v1, err := New([]byte("key-one"))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	v2, err := New([]byte("key-two"))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	encrypted, err := v1.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	_, err = v2.Decrypt(encrypted)
	if err == nil {
		t.Error("Decrypt() with wrong key should fail")
	}
}

func TestDecryptNotEncrypted(t *testing.T) {
	v, err := New([]byte("key"))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, err = v.Decrypt("plaintext")
	if !errors.Is(err, ErrNotEncrypted) {
		t.Errorf("Decrypt(plaintext) error = %v, want ErrNotEncrypted", err)
	}
}

func TestNewEmptyKey(t *testing.T) {
	_, err := New([]byte{})
	if err == nil {
		t.Error("New(empty) should return error")
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	v, err := New([]byte("key"))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	e1, _ := v.Encrypt("same-plaintext")
	e2, _ := v.Encrypt("same-plaintext")

	if e1 == e2 {
		t.Error("two encryptions of the same plaintext should produce different ciphertexts")
	}
}
