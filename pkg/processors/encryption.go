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

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
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

// bindAndDecodeKey получает ключ от Mercury и верифицирует HMAC — общий для
// v1.3 (Encrypt, whole-blob) и v1.5 (EncryptSectionsV15) первый шаг; разница
// между форматами начинается только после того как raw key на руках.
// Возвращает (key, errCode, error) — errCode как в Encrypt, для error-пакета.
func (f *FileEncryptor) bindAndDecodeKey(ctx context.Context) ([]byte, string, error) {
	// 1. Получаем ключ от xZMercury
	binding, err := f.client.BindKey(ctx, f.packageUUID, f.pipelineName)
	if err != nil {
		return nil, mercury.ErrorCode(err), fmt.Errorf("bind key: %w", err)
	}

	// 2. Верифицируем HMAC.
	// serverSecret пустой — ошибка конфигурации: молчаливый bypass недопустим в проде.
	// Для dev-режима нужно явно передать "dev-mode" (DevClient / тестовый сервер).
	if f.serverSecret == "" {
		return nil, mercury.ErrCodeHMACVerificationFailed,
			fmt.Errorf("%w: MERCURY_SERVER_SECRET not set — "+
				"HMAC verification is mandatory; use serverSecret=\"dev-mode\" to opt out explicitly",
				mercury.ErrHMACVerificationFailed)
	}
	if f.serverSecret != "dev-mode" {
		// mode берём из ответа сервера — он включён в HMAC, поэтому подделать нельзя.
		// dev-binding (mode="dev") не пройдёт верификацию с prod-secret даже при утечке:
		// разные секреты + разная строка в подписи.
		if !mercury.VerifyHMAC(f.packageUUID, binding.HMAC, f.serverSecret, binding.Mode) {
			return nil, mercury.ErrCodeHMACVerificationFailed,
				fmt.Errorf("%w: uuid=%s mode=%s", mercury.ErrHMACVerificationFailed,
					f.packageUUID, binding.Mode)
		}
	}

	// 3. Декодируем ключ
	key, err := mercury.DecodeKey(binding.KeyB64)
	if err != nil {
		return nil, mercury.ErrCodeMercuryError, fmt.Errorf("decode key: %w", err)
	}
	return key, "", nil
}

// Encrypt получает ключ от Mercury, верифицирует HMAC, шифрует plaintext.
// Возвращает (result, errCode, error):
//   - result: зашифрованный блоб + метаданные
//   - errCode: mercury.ErrCode* — код для error-пакета при сбое
//   - error: детальная ошибка
func (f *FileEncryptor) Encrypt(ctx context.Context, plaintext []byte) (*EncryptionResult, string, error) {
	key, errCode, err := f.bindAndDecodeKey(ctx)
	if err != nil {
		return nil, errCode, err
	}

	// Шифруем AES-256-GCM (legacy whole-blob format)
	encrypted, err := tdtpcrypto.Encrypt(key, plaintext, f.packageUUID)
	if err != nil {
		return nil, mercury.ErrCodeMercuryError, fmt.Errorf("encrypt: %w", err)
	}

	return &EncryptionResult{
		PackageUUID: f.packageUUID,
		Encrypted:   encrypted,
	}, "", nil
}

// EncryptSectionsV15 получает ключ от Mercury (тем же BindKey+HMAC флоу что
// и Encrypt) и шифрует QueryContext/Schema/Data пакета "на месте" через
// packet.EncryptSections (TDTP v1.5 — Header остаётся plain XML). pkt
// должен уже пройти ComputeIntegrity и сжатие, если оно включено — порядок
// hash->compress->encrypt фиксирован, эта функция ничего из этого не
// выполняет сама. f.packageUUID должен быть pkt.Header.MessageID — иначе
// консьюмер не сможет узнать, каким uuid делать RetrieveKey, не расшифровав
// то, что как раз требует ключа (см. docs/tdtp-protocol-schema.md → "v1.5").
func (f *FileEncryptor) EncryptSectionsV15(ctx context.Context, pkt *packet.DataPacket) (string, error) {
	key, errCode, err := f.bindAndDecodeKey(ctx)
	if err != nil {
		return errCode, err
	}
	if err := packet.EncryptSections(pkt, key); err != nil {
		return mercury.ErrCodeMercuryError, fmt.Errorf("encrypt sections: %w", err)
	}
	return "", nil
}

// WriteEncrypted записывает зашифрованный блоб в файл.
// Расширение .tdtp.enc помогает downstream-компонентам отличить зашифрованные файлы.
func WriteEncrypted(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write encrypted file: %w", err)
	}
	return nil
}
