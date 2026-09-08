package engine

import (
	"github.com/bartkleypas/please/internal/storage"
)

// Forwarded cryptographic vault helpers
var (
	// EncryptField encrypts a plaintext string using AES-GCM with the provided key
	EncryptField = storage.EncryptField

	// DecryptField decrypts a ciphertext string using AES-GCM with the provided key
	DecryptField = storage.DecryptField
)
