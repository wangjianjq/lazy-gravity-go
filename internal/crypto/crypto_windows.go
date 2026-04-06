//go:build windows

package crypto

import (
	"encoding/base64"
	"fmt"
	"syscall"
	"unsafe"
)

// dataBlob mirrors the Windows CRYPT_INTEGER_BLOB (DATA_BLOB) structure.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	// UseMock enables a simple base64-only mock encryption for tests.
	UseMock bool
)

var (
	modCrypt32             = syscall.NewLazyDLL("Crypt32.dll")
	modKernel32            = syscall.NewLazyDLL("Kernel32.dll")
	procCryptProtectData   = modCrypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = modCrypt32.NewProc("CryptUnprotectData")
	procLocalFree          = modKernel32.NewProc("LocalFree")
)

func blobFromBytes(data []byte) *dataBlob {
	if len(data) == 0 {
		var b byte = 0
		return &dataBlob{cbData: 0, pbData: &b}
	}
	return &dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
}

// Encrypt encrypts plaintext using Windows DPAPI (current-user scope).
// Returns a Base64-encoded ciphertext that can only be decrypted on the
// same machine by the same Windows user account.
func Encrypt(plaintext string) (string, error) {
	if UseMock {
		return base64.StdEncoding.EncodeToString([]byte(plaintext)), nil
	}
	data := []byte(plaintext)
	input := blobFromBytes(data)
	var output dataBlob

	ret, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(input)), // pDataIn
		0,                              // szDataDescr
		0,                              // pOptionalEntropy
		0,                              // pvReserved
		0,                              // pPromptStruct
		0,                              // dwFlags (CRYPTPROTECT_LOCAL_MACHINE=0 → user-scope)
		uintptr(unsafe.Pointer(&output)),
	)
	if ret == 0 {
		return "", fmt.Errorf("CryptProtectData: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(output.pbData)))

	cipherBytes := make([]byte, output.cbData)
	copy(cipherBytes, unsafe.Slice(output.pbData, output.cbData))
	return base64.StdEncoding.EncodeToString(cipherBytes), nil
}

// Decrypt decrypts a DPAPI-encrypted Base64 string produced by Encrypt.
func Decrypt(ciphertext string) (string, error) {
	if UseMock {
		data, err := base64.StdEncoding.DecodeString(ciphertext)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	input := blobFromBytes(data)
	var output dataBlob

	ret, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(input)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&output)),
	)
	if ret == 0 {
		return "", fmt.Errorf("CryptUnprotectData: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(output.pbData)))

	plainBytes := make([]byte, output.cbData)
	copy(plainBytes, unsafe.Slice(output.pbData, output.cbData))
	return string(plainBytes), nil
}
