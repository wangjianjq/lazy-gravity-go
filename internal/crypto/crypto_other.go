//go:build !windows

package crypto

import (
	"encoding/base64"
	"fmt"
)

var (
	// UseMock enables a simple base64-only mock encryption for tests.
	UseMock bool
)

// Encrypt is a stub for non-Windows platforms (or mock mode).
func Encrypt(plaintext string) (string, error) {
	if UseMock {
		return base64.StdEncoding.EncodeToString([]byte(plaintext)), nil
	}
	return "", fmt.Errorf("DPAPI encryption is only supported on Windows")
}

// Decrypt is a stub for non-Windows platforms (or mock mode).
func Decrypt(ciphertext string) (string, error) {
	if UseMock {
		data, err := base64.StdEncoding.DecodeString(ciphertext)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return "", fmt.Errorf("DPAPI decryption is only supported on Windows")
}
