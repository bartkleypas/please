package engine

import (
	"strings"
	"testing"
)

func TestEncryptDecryptField(t *testing.T) {
	key := "super-secret-key"
	plaintext := "hello world! this is a secret thought."

	// Test successful encryption
	ciphertext, err := EncryptField(plaintext, key)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	if ciphertext == plaintext {
		t.Errorf("Ciphertext should not equal plaintext")
	}

	if !strings.HasPrefix(ciphertext, "enc:v1:") {
		t.Errorf("Ciphertext missing prefix, got: %s", ciphertext)
	}

	// Test successful decryption
	decrypted, err := DecryptField(ciphertext, key)
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Expected decrypted %q, got %q", plaintext, decrypted)
	}
}

func TestEncryptDecryptEmptyKey(t *testing.T) {
	plaintext := "some text"
	
	ciphertext, err := EncryptField(plaintext, "")
	if err != nil {
		t.Fatalf("Failed to encrypt with empty key: %v", err)
	}

	if ciphertext != plaintext {
		t.Errorf("With empty key, expected ciphertext to equal plaintext")
	}

	decrypted, err := DecryptField(ciphertext, "")
	if err != nil {
		t.Fatalf("Failed to decrypt with empty key: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("With empty key, expected decrypted to equal plaintext")
	}
}

func TestDecryptUnencrypted(t *testing.T) {
	unencrypted := "just some normal unencrypted json"
	
	decrypted, err := DecryptField(unencrypted, "some-key")
	if err != nil {
		t.Fatalf("Failed to decrypt unencrypted text: %v", err)
	}

	if decrypted != unencrypted {
		t.Errorf("Expected unencrypted text to be returned unchanged")
	}
}
