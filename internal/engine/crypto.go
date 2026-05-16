package engine

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const encryptionPrefix = "enc:v1:"

// deriveKey creates a 32-byte key from the provided string using SHA-256
func deriveKey(key string) []byte {
	hash := sha256.Sum256([]byte(key))
	return hash[:]
}

// EncryptField encrypts a plaintext string using AES-GCM with the provided key.
// If the key is empty or the plaintext is empty, it returns the plaintext unchanged.
// The resulting ciphertext is base64 encoded and prefixed with "enc:v1:".
func EncryptField(plaintext string, key string) (string, error) {
	if key == "" || plaintext == "" {
		return plaintext, nil
	}

	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create gcm: %w", err)
	}

	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := aesgcm.Seal(nonce, nonce, []byte(plaintext), nil)
	encoded := base64.StdEncoding.EncodeToString(ciphertext)

	return encryptionPrefix + encoded, nil
}

// DecryptField decrypts a ciphertext string using AES-GCM with the provided key.
// If the ciphertext does not start with the encryption prefix, it is returned unchanged.
// If the key is empty, it will fail to decrypt unless the string is plaintext.
func DecryptField(ciphertext string, key string) (string, error) {
	if !strings.HasPrefix(ciphertext, encryptionPrefix) {
		return ciphertext, nil
	}

	if key == "" {
		return "", fmt.Errorf("cannot decrypt field: no encryption key provided")
	}

	encoded := strings.TrimPrefix(ciphertext, encryptionPrefix)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create gcm: %w", err)
	}

	nonceSize := aesgcm.NonceSize()
	if len(decoded) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := decoded[:nonceSize], decoded[nonceSize:]
	plaintext, err := aesgcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}
