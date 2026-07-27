package crypto

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// --- Encrypt / Decrypt ---

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"
	plaintext := []byte("salary report: top secret data 12345")

	blob, err := Encrypt(key, plaintext, uuid)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	gotUUID, got, err := Decrypt(key, blob)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if gotUUID != uuid {
		t.Errorf("Decrypt() uuid = %q, want %q", gotUUID, uuid)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("Decrypt() plaintext mismatch")
	}
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	key := make([]byte, 32)
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"

	blob, err := Encrypt(key, []byte{}, uuid)
	if err != nil {
		t.Fatalf("Encrypt() empty plaintext error = %v", err)
	}
	_, got, err := Decrypt(key, blob)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Decrypt() expected empty plaintext, got %d bytes", len(got))
	}
}

// --- Encrypt: неверные аргументы ---

func TestEncrypt_InvalidKeyLength(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{"empty key", []byte{}},
		{"16-byte key (AES-128)", make([]byte, 16)},
		{"31-byte key", make([]byte, 31)},
		{"33-byte key", make([]byte, 33)},
	}
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Encrypt(tt.key, []byte("data"), uuid)
			if err == nil {
				t.Error("Encrypt() expected error for invalid key length")
			}
		})
	}
}

func TestEncrypt_InvalidUUID(t *testing.T) {
	key := make([]byte, 32)
	tests := []struct {
		name string
		uuid string
	}{
		{"empty UUID", ""},
		{"too short", "e6de8dd5-4e9a"},
		{"invalid hex in UUID", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Encrypt(key, []byte("data"), tt.uuid)
			if err == nil {
				t.Errorf("Encrypt() expected error for invalid UUID %q", tt.uuid)
			}
		})
	}
}

// --- Decrypt: атаки и повреждения ---

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := bytes.Repeat([]byte{0xAA}, 32)
	key2 := bytes.Repeat([]byte{0xBB}, 32)
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"

	blob, err := Encrypt(key1, []byte("secret"), uuid)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	_, _, err = Decrypt(key2, blob)
	if err == nil {
		t.Error("Decrypt() with wrong key should return error (GCM auth failed)")
	}
}

func TestDecrypt_CorruptedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"

	blob, err := Encrypt(key, []byte("confidential data"), uuid)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	// Портим последний байт — часть GCM-тега
	blob[len(blob)-1] ^= 0xFF

	_, _, err = Decrypt(key, blob)
	if err == nil {
		t.Error("Decrypt() on corrupted ciphertext should return error")
	}
}

func TestDecrypt_BlobTooShort(t *testing.T) {
	key := make([]byte, 32)
	tests := []struct {
		name string
		blob []byte
	}{
		{"nil blob", nil},
		{"empty blob", []byte{}},
		{"1 byte", []byte{0x01}},
		{"header - 1 byte", make([]byte, headerSize-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Decrypt(key, tt.blob)
			if err == nil {
				t.Error("Decrypt() expected error for short blob")
			}
		})
	}
}

func TestDecrypt_InvalidKeyLength(t *testing.T) {
	blob := make([]byte, headerSize+16) // минимальный валидный размер
	blob[0] = headerVersion
	blob[2] = algoAES256GCM

	tests := []struct {
		name string
		key  []byte
	}{
		{"16-byte key", make([]byte, 16)},
		{"empty key", []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Decrypt(tt.key, blob)
			if err == nil {
				t.Error("Decrypt() expected error for invalid key length")
			}
		})
	}
}

func TestDecrypt_UnsupportedVersion(t *testing.T) {
	key := make([]byte, 32)
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"

	blob, err := Encrypt(key, []byte("data"), uuid)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	blob[0] = 0x99 // подменяем версию

	_, _, err = Decrypt(key, blob)
	if err == nil {
		t.Error("Decrypt() expected error for unsupported version")
	}
}

func TestDecrypt_UnsupportedAlgorithm(t *testing.T) {
	key := make([]byte, 32)
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"

	blob, err := Encrypt(key, []byte("data"), uuid)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	blob[2] = 0x99 // подменяем алгоритм

	_, _, err = Decrypt(key, blob)
	if err == nil {
		t.Error("Decrypt() expected error for unsupported algorithm")
	}
}

// --- ExtractUUID ---

func TestExtractUUID_MatchesEncrypted(t *testing.T) {
	key := make([]byte, 32)
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"

	blob, err := Encrypt(key, []byte("payload"), uuid)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	got, err := ExtractUUID(blob)
	if err != nil {
		t.Fatalf("ExtractUUID() error = %v", err)
	}
	if got != uuid {
		t.Errorf("ExtractUUID() = %q, want %q", got, uuid)
	}
}

func TestExtractUUID_NoKeyRequired(t *testing.T) {
	// Получатель узнаёт UUID без расшифровки — чтобы запросить ключ у xZMercury
	key := bytes.Repeat([]byte{0xFF}, 32)
	uuid := "aabbccdd-1122-3344-5566-778899aabbcc"

	blob, err := Encrypt(key, []byte("top secret report"), uuid)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Ключа нет — только blob
	got, err := ExtractUUID(blob)
	if err != nil {
		t.Fatalf("ExtractUUID() error = %v", err)
	}
	if got != uuid {
		t.Errorf("ExtractUUID() = %q, want %q", got, uuid)
	}
}

func TestExtractUUID_BlobTooShort(t *testing.T) {
	tests := []struct {
		name string
		blob []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"header - 1 byte", make([]byte, headerSize-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ExtractUUID(tt.blob)
			if err == nil {
				t.Error("ExtractUUID() expected error for short blob")
			}
		})
	}
}

// --- Свойства безопасности ---

func TestEncrypt_UniqueNonce(t *testing.T) {
	// Один и тот же plaintext → разный ciphertext (nonce из crypto/rand)
	key := make([]byte, 32)
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"
	plaintext := []byte("same payload every time")

	blob1, err := Encrypt(key, plaintext, uuid)
	if err != nil {
		t.Fatalf("Encrypt() first call error = %v", err)
	}
	blob2, err := Encrypt(key, plaintext, uuid)
	if err != nil {
		t.Fatalf("Encrypt() second call error = %v", err)
	}
	if bytes.Equal(blob1, blob2) {
		t.Error("Encrypt() produced identical blobs — nonce not random (replay attack risk)")
	}
}

func TestEncrypt_DifferentUUIDs_DifferentBlobs(t *testing.T) {
	// Разные UUID → разные заголовки → разные блобы (изоляция пакетов)
	key := make([]byte, 32)
	plaintext := []byte("same data")
	uuid1 := "aaaaaaaa-0000-0000-0000-000000000001"
	uuid2 := "bbbbbbbb-0000-0000-0000-000000000002"

	blob1, _ := Encrypt(key, plaintext, uuid1)
	blob2, _ := Encrypt(key, plaintext, uuid2)

	// UUID-секции (байты 3..18) должны отличаться
	if bytes.Equal(blob1[3:3+uuidSize], blob2[3:3+uuidSize]) {
		t.Error("Encrypt() UUID section identical for different UUIDs")
	}
}

// --- EncryptSection / DecryptSection (v1.5) ---

func TestEncryptDecryptSection_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	plaintext := []byte(`<Field name="id" type="INTEGER" key="true"/>`)

	encoded, err := EncryptSection(key, plaintext)
	if err != nil {
		t.Fatalf("EncryptSection() error = %v", err)
	}

	got, err := DecryptSection(key, encoded)
	if err != nil {
		t.Fatalf("DecryptSection() error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("DecryptSection() plaintext mismatch: got %q, want %q", got, plaintext)
	}
}

func TestEncryptDecryptSection_EmptyPlaintext(t *testing.T) {
	key := make([]byte, 32)

	encoded, err := EncryptSection(key, []byte{})
	if err != nil {
		t.Fatalf("EncryptSection() empty plaintext error = %v", err)
	}
	got, err := DecryptSection(key, encoded)
	if err != nil {
		t.Fatalf("DecryptSection() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("DecryptSection() expected empty plaintext, got %d bytes", len(got))
	}
}

func TestEncryptSection_NoHeaderOverhead(t *testing.T) {
	// Section format has no version/algo/uuid header — just base64(nonce||ciphertext).
	// Decoded length must be exactly nonceSize + len(plaintext) + GCM tag(16).
	key := make([]byte, 32)
	plaintext := []byte("twelve bytes")

	encoded, err := EncryptSection(key, plaintext)
	if err != nil {
		t.Fatalf("EncryptSection() error = %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode error = %v", err)
	}
	wantLen := nonceSize + len(plaintext) + 16 // GCM tag
	if len(raw) != wantLen {
		t.Errorf("decoded length = %d, want %d (no whole-blob header expected)", len(raw), wantLen)
	}
}

func TestEncryptSection_InvalidKeyLength(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{"empty key", []byte{}},
		{"16-byte key (AES-128)", make([]byte, 16)},
		{"31-byte key", make([]byte, 31)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EncryptSection(tt.key, []byte("data"))
			if err == nil {
				t.Error("EncryptSection() expected error for invalid key length")
			}
		})
	}
}

func TestDecryptSection_WrongKey(t *testing.T) {
	key1 := bytes.Repeat([]byte{0xAA}, 32)
	key2 := bytes.Repeat([]byte{0xBB}, 32)

	encoded, err := EncryptSection(key1, []byte("secret schema"))
	if err != nil {
		t.Fatalf("EncryptSection() error = %v", err)
	}
	_, err = DecryptSection(key2, encoded)
	if err == nil {
		t.Error("DecryptSection() with wrong key should return error (GCM auth failed)")
	}
}

func TestDecryptSection_CorruptedCiphertext(t *testing.T) {
	key := make([]byte, 32)

	encoded, err := EncryptSection(key, []byte("confidential rows"))
	if err != nil {
		t.Fatalf("EncryptSection() error = %v", err)
	}
	raw, _ := base64.StdEncoding.DecodeString(encoded)
	raw[len(raw)-1] ^= 0xFF // corrupt last byte of the GCM tag
	corrupted := base64.StdEncoding.EncodeToString(raw)

	_, err = DecryptSection(key, corrupted)
	if err == nil {
		t.Error("DecryptSection() on corrupted ciphertext should return error")
	}
}

func TestDecryptSection_InvalidBase64(t *testing.T) {
	key := make([]byte, 32)
	_, err := DecryptSection(key, "not-valid-base64!!!")
	if err == nil {
		t.Error("DecryptSection() expected error for invalid base64")
	}
}

func TestDecryptSection_TooShort(t *testing.T) {
	key := make([]byte, 32)
	tests := []struct {
		name    string
		encoded string
	}{
		{"empty string", ""},
		{"shorter than nonce", base64.StdEncoding.EncodeToString(make([]byte, nonceSize-1))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecryptSection(key, tt.encoded)
			if err == nil {
				t.Error("DecryptSection() expected error for too-short data")
			}
		})
	}
}

func TestDecryptSection_InvalidKeyLength(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(make([]byte, nonceSize+16))
	tests := []struct {
		name string
		key  []byte
	}{
		{"16-byte key", make([]byte, 16)},
		{"empty key", []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecryptSection(tt.key, encoded)
			if err == nil {
				t.Error("DecryptSection() expected error for invalid key length")
			}
		})
	}
}

func TestEncryptSection_UniqueNoncePerCall(t *testing.T) {
	// Same key, same plaintext, called multiple times (mirrors encrypting
	// QueryContext/Schema/Data of one packet with one key) — ciphertexts
	// must differ, proving nonces never repeat under the same key.
	key := make([]byte, 32)
	plaintext := []byte("same section content every time")

	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		encoded, err := EncryptSection(key, plaintext)
		if err != nil {
			t.Fatalf("EncryptSection() call %d error = %v", i, err)
		}
		if seen[encoded] {
			t.Fatalf("EncryptSection() produced duplicate ciphertext on call %d — nonce reuse", i)
		}
		seen[encoded] = true
	}
}

func TestEncryptSection_OneKeyMultipleSections(t *testing.T) {
	// Simulates encrypting QueryContext + Schema + Data of one packet with
	// one BindKey-issued key — all three must round-trip independently.
	key := make([]byte, 32)
	sections := map[string][]byte{
		"QueryContext": []byte(`<OriginalQuery>balance >= 1000</OriginalQuery>`),
		"Schema":       []byte(`<Field name="id" type="INTEGER"/>`),
		"Data":         []byte("1|John Doe|1000.50\n2|Jane Roe|2500.00\n"),
	}

	encrypted := make(map[string]string, len(sections))
	for name, plaintext := range sections {
		encoded, err := EncryptSection(key, plaintext)
		if err != nil {
			t.Fatalf("EncryptSection(%s) error = %v", name, err)
		}
		encrypted[name] = encoded
	}

	for name, plaintext := range sections {
		got, err := DecryptSection(key, encrypted[name])
		if err != nil {
			t.Fatalf("DecryptSection(%s) error = %v", name, err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Errorf("DecryptSection(%s) mismatch: got %q, want %q", name, got, plaintext)
		}
	}
}

// --- Benchmarks ---

func BenchmarkEncrypt_4KB(b *testing.B) {
	key := make([]byte, 32)
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"
	data := make([]byte, 4096)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Encrypt(key, data, uuid)
	}
}

func BenchmarkDecrypt_4KB(b *testing.B) {
	key := make([]byte, 32)
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"
	blob, _ := Encrypt(key, make([]byte, 4096), uuid)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = Decrypt(key, blob)
	}
}

func BenchmarkExtractUUID(b *testing.B) {
	key := make([]byte, 32)
	blob, _ := Encrypt(key, []byte("data"), "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractUUID(blob)
	}
}
