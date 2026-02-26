// Package crypto предоставляет AES-256-GCM шифрование для TDTP-пакетов.
//
// Формат зашифрованного файла:
//
//	[2B version][1B algorithm][16B package_uuid][12B nonce][...ciphertext]
//
// version:      0x01 0x00 (v1.0)
// algorithm:    0x01 = AES-256-GCM
// package_uuid: UUID пакета в бинарном виде (16 байт без дефисов)
// nonce:        12 байт из crypto/rand (уникален для каждого шифрования)
// ciphertext:   AES-256-GCM ciphertext + 16-байтный GCM-тег
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const (
	headerVersion   = byte(0x01)
	headerVersionLo = byte(0x00)
	algoAES256GCM   = byte(0x01)

	headerSize = 2 + 1 + 16 + 12 // version(2) + algo(1) + uuid(16) + nonce(12)
	nonceSize  = 12
	uuidSize   = 16
)

// Encrypt шифрует plaintext алгоритмом AES-256-GCM.
// key — 32 байта (AES-256), packageUUID — UUID пакета в стандартном формате "xxxxxxxx-xxxx-...".
// Возвращает бинарный блоб: заголовок + ciphertext.
func Encrypt(key []byte, plaintext []byte, packageUUID string) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encrypt: key must be 32 bytes, got %d", len(key))
	}

	uuidBytes, err := uuidToBytes(packageUUID)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("encrypt: create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encrypt: create GCM: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("encrypt: generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Собираем: [2B version][1B algo][16B uuid][12B nonce][ciphertext]
	out := make([]byte, 0, headerSize+len(ciphertext))
	out = append(out, headerVersion, headerVersionLo)
	out = append(out, algoAES256GCM)
	out = append(out, uuidBytes...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)

	return out, nil
}

// Decrypt расшифровывает блоб, созданный Encrypt.
// Возвращает UUID пакета из заголовка и plaintext.
func Decrypt(key []byte, blob []byte) (packageUUID string, plaintext []byte, err error) {
	if len(key) != 32 {
		return "", nil, fmt.Errorf("decrypt: key must be 32 bytes, got %d", len(key))
	}
	if len(blob) < headerSize {
		return "", nil, fmt.Errorf("decrypt: blob too short: %d bytes", len(blob))
	}

	// Разбираем заголовок
	// blob[0:2] — version (проверяем)
	if blob[0] != headerVersion {
		return "", nil, fmt.Errorf("decrypt: unsupported version: 0x%02x", blob[0])
	}
	// blob[2] — algo
	if blob[2] != algoAES256GCM {
		return "", nil, fmt.Errorf("decrypt: unsupported algorithm: 0x%02x", blob[2])
	}

	uuidBytes := blob[3 : 3+uuidSize]
	packageUUID = bytesToUUID(uuidBytes)
	nonce := blob[3+uuidSize : 3+uuidSize+nonceSize]
	ciphertext := blob[headerSize:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", nil, fmt.Errorf("decrypt: create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", nil, fmt.Errorf("decrypt: create GCM: %w", err)
	}

	plaintext, err = gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", nil, fmt.Errorf("decrypt: authentication failed (wrong key or corrupted data): %w", err)
	}

	return packageUUID, plaintext, nil
}

// uuidToBytes конвертирует UUID-строку "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" в 16 байт.
func uuidToBytes(uuid string) ([]byte, error) {
	clean := strings.ReplaceAll(uuid, "-", "")
	if len(clean) != 32 {
		return nil, fmt.Errorf("invalid UUID: %q", uuid)
	}
	b, err := hex.DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("invalid UUID hex: %w", err)
	}
	return b, nil
}

// bytesToUUID конвертирует 16 байт в UUID-строку.
func bytesToUUID(b []byte) string {
	h := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}
