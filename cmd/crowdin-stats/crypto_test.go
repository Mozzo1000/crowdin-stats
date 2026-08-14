package main

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func randomKey(t *testing.T) [32]byte {
	t.Helper()
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return key
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := randomKey(t)

	ciphertext, nonce, err := encryptToken(key, "my-crowdin-pat")
	if err != nil {
		t.Fatalf("encryptToken: %v", err)
	}

	got, err := decryptToken(key, ciphertext, nonce)
	if err != nil {
		t.Fatalf("decryptToken: %v", err)
	}
	if got != "my-crowdin-pat" {
		t.Fatalf("got %q, want %q", got, "my-crowdin-pat")
	}
}

func TestEncryptTokenUsesDistinctNonces(t *testing.T) {
	key := randomKey(t)

	_, nonce1, err := encryptToken(key, "token")
	if err != nil {
		t.Fatalf("encryptToken: %v", err)
	}
	_, nonce2, err := encryptToken(key, "token")
	if err != nil {
		t.Fatalf("encryptToken: %v", err)
	}

	if string(nonce1) == string(nonce2) {
		t.Fatalf("expected distinct nonces across calls, got the same nonce twice")
	}
}

func TestDecryptTokenWrongKeyFails(t *testing.T) {
	key := randomKey(t)
	wrongKey := randomKey(t)

	ciphertext, nonce, err := encryptToken(key, "my-crowdin-pat")
	if err != nil {
		t.Fatalf("encryptToken: %v", err)
	}

	if _, err := decryptToken(wrongKey, ciphertext, nonce); err == nil {
		t.Fatal("expected decryption with the wrong key to fail, got nil error")
	}
}

func TestDecryptTokenTamperedCiphertextFails(t *testing.T) {
	key := randomKey(t)

	ciphertext, nonce, err := encryptToken(key, "my-crowdin-pat")
	if err != nil {
		t.Fatalf("encryptToken: %v", err)
	}
	ciphertext[0] ^= 0xFF

	if _, err := decryptToken(key, ciphertext, nonce); err == nil {
		t.Fatal("expected decryption of tampered ciphertext to fail, got nil error")
	}
}

func TestDecryptTokenShortNonceFails(t *testing.T) {
	key := randomKey(t)

	ciphertext, _, err := encryptToken(key, "my-crowdin-pat")
	if err != nil {
		t.Fatalf("encryptToken: %v", err)
	}

	if _, err := decryptToken(key, ciphertext, []byte("too-short")); err == nil {
		t.Fatal("expected decryption with a short nonce to fail, got nil error")
	}
}

func TestLoadMasterKey(t *testing.T) {
	validKey := make([]byte, 32)
	if _, err := rand.Read(validKey); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	validB64 := base64.StdEncoding.EncodeToString(validKey)

	cases := []struct {
		name    string
		value   string
		unset   bool
		wantErr bool
	}{
		{name: "valid 32-byte base64 key", value: validB64},
		{name: "unset", unset: true, wantErr: true},
		{name: "empty string", value: "", wantErr: true},
		{name: "not base64", value: "not-valid-base64!!!", wantErr: true},
		{name: "wrong length (16 bytes)", value: base64.StdEncoding.EncodeToString(make([]byte, 16)), wantErr: true},
		{name: "wrong length (64 bytes)", value: base64.StdEncoding.EncodeToString(make([]byte, 64)), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.unset {
				t.Setenv("MASTER_KEY", "")
			} else {
				t.Setenv("MASTER_KEY", tc.value)
			}

			key, err := loadMasterKey()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("loadMasterKey: %v", err)
			}
			if base64.StdEncoding.EncodeToString(key[:]) != validB64 {
				t.Fatal("decoded key does not match the input")
			}
		})
	}
}
