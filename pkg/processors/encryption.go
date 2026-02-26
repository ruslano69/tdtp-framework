// Package processors — FileEncryptor выполняет шифрование готового TDTP-файла.
// Встаёт в пайплайне после CompressionProcessor (уровень файла, не строк).
//
// Использование в exporter.go:
//
//	enc := processors.NewFileEncryptor(mercuryClient, serverSecret, packageUUID)
//	if err := enc.EncryptFile(ctx, xmlData); err != nil { ... }
package processors

import (
	"context"
	"fmt"
	"os"

	tdtpcrypto "github.com/ruslano69/tdtp-framework/pkg/crypto"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
)

// EncryptionResult — результат шифрования файла.
type EncryptionResult struct {
	PackageUUID string
	KeyB64      string // только для dev-режима; в prod всегда ""
	Encrypted   []byte // зашифрованный блоб
}

// FileEncryptor шифрует TDTP XML блоб алгоритмом AES-256-GCM,
// получая ключ от xZMercury через UUID-binding флоу.
type FileEncryptor struct {
	client       MercuryBinder
	serverSecret string
	packageUUID  string
	pipelineName string
}

// MercuryBinder — интерфейс для BindKey, позволяет подменять в тестах и dev-режиме.
type MercuryBinder interface {
	BindKey(ctx context.Context, packageUUID, pipelineName string) (*mercury.KeyBinding, error)
}

// NewFileEncryptor создаёт FileEncryptor для продакшен-режима.
// serverSecret — значение переменной MERCURY_SERVER_SECRET для верификации HMAC.
func NewFileEncryptor(client MercuryBinder, serverSecret, packageUUID, pipelineName string) *FileEncryptor {
	return &FileEncryptor{
		client:       client,
		serverSecret: serverSecret,
		packageUUID:  packageUUID,
		pipelineName: pipelineName,
	}
}

// Encrypt получает ключ от Mercury, верифицирует HMAC, шифрует plaintext.
// Возвращает (result, errCode, error):
//   - result: зашифрованный блоб + метаданные
//   - errCode: mercury.ErrCode* — код для error-пакета при сбое
//   - error: детальная ошибка
func (f *FileEncryptor) Encrypt(ctx context.Context, plaintext []byte) (*EncryptionResult, string, error) {
	// 1. Получаем ключ от xZMercury
	binding, err := f.client.BindKey(ctx, f.packageUUID, f.pipelineName)
	if err != nil {
		return nil, mercury.ErrorCode(err), fmt.Errorf("bind key: %w", err)
	}

	// 2. Верифицируем HMAC (в dev-режиме DevClient возвращает фиктивный HMAC — он не верифицируется)
	if f.serverSecret != "" && f.serverSecret != "dev-mode" {
		if !mercury.VerifyHMAC(f.packageUUID, binding.HMAC, f.serverSecret) {
			return nil, mercury.ErrCodeHMACVerificationFailed,
				fmt.Errorf("%w: uuid=%s", mercury.ErrHMACVerificationFailed, f.packageUUID)
		}
	}

	// 3. Декодируем ключ
	key, err := mercury.DecodeKey(binding.KeyB64)
	if err != nil {
		return nil, mercury.ErrCodeMercuryError, fmt.Errorf("decode key: %w", err)
	}

	// 4. Шифруем AES-256-GCM
	encrypted, err := tdtpcrypto.Encrypt(key, plaintext, f.packageUUID)
	if err != nil {
		return nil, mercury.ErrCodeMercuryError, fmt.Errorf("encrypt: %w", err)
	}

	return &EncryptionResult{
		PackageUUID: f.packageUUID,
		Encrypted:   encrypted,
	}, "", nil
}

// WriteEncrypted записывает зашифрованный блоб в файл.
// Расширение .tdtp.enc помогает downstream-компонентам отличить зашифрованные файлы.
func WriteEncrypted(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write encrypted file: %w", err)
	}
	return nil
}
