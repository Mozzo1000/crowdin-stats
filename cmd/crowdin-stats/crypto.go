package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/nacl/secretbox"
)

func loadMasterKey() ([32]byte, error) {
	var key [32]byte

	raw := os.Getenv("MASTER_KEY")
	if raw == "" {
		return key, errors.New("MASTER_KEY not set")
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(decoded) != 32 {
		return key, fmt.Errorf("MASTER_KEY must be 32 bytes, base64-encoded")
	}
	copy(key[:], decoded)
	return key, nil
}

func encryptToken(key [32]byte, token string) (ciphertext, nonce []byte, err error) {
	var n [24]byte
	if _, err := rand.Read(n[:]); err != nil {
		return nil, nil, err
	}
	ct := secretbox.Seal(nil, []byte(token), &n, &key)
	return ct, n[:], nil
}

// generateRevokeToken returns a fresh high-entropy revoke token and its
// SHA-256 hash. The token is shown to the user exactly once (embedded in the
// revoke URL); only its hash is ever persisted, so a leaked database can't
// be used to revoke a project.
func generateRevokeToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashRevokeToken(token), nil
}

func hashRevokeToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func decryptToken(key [32]byte, ciphertext, nonce []byte) (string, error) {
	if len(nonce) != 24 {
		return "", errors.New("decryption failed — invalid nonce length")
	}
	var n [24]byte
	copy(n[:], nonce)
	out, ok := secretbox.Open(nil, ciphertext, &n, &key)
	if !ok {
		return "", errors.New("decryption failed — data may be corrupted")
	}
	return string(out), nil
}
