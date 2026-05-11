package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Sealer encrypts/decrypts small secrets (e.g. user-supplied LLM API keys)
// with AES-256-GCM, using a master key configured via AETHER_MASTER_KEY.
//
// Output format (base64): nonce || ciphertext.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer constructs a sealer from a base64-encoded 32-byte key.
func NewSealer(b64Key string) (*Sealer, error) {
	key, err := base64.StdEncoding.DecodeString(b64Key)
	if err != nil {
		return nil, fmt.Errorf("master key not valid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes (got %d)", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal returns a base64 ciphertext.
func (s *Sealer) Seal(plaintext string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := s.aead.Seal(nil, nonce, []byte(plaintext), nil)
	combined := append(nonce, ct...)
	return base64.StdEncoding.EncodeToString(combined), nil
}

// Open reverses Seal.
func (s *Sealer) Open(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	if len(raw) < s.aead.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():]
	pt, err := s.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
