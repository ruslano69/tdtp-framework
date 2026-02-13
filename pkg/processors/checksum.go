// File: pkg/processors/checksum.go

package processors

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/zeebo/xxh3"
)

// ChecksumProcessor вычисляет и проверяет контрольные суммы данных.
// Использует xxh3 (64-bit) для быстрой проверки целостности.
//
// Режимы работы:
//  1. Генерация (expectedHash == ""): вычисляет хеш и передает через callback
//  2. Валидация (expectedHash != ""): проверяет соответствие вычисленного хеша ожидаемому
type ChecksumProcessor struct {
	validate bool   // true если нужна валидация
	expected string // ожидаемый хеш (hex-encoded)
	callback func(string) // callback для передачи вычисленного хеша
}

// NewChecksumProcessor создает новый процессор контрольных сумм.
//
// Параметры:
//   expectedHash - ожидаемый хеш для валидации (пустая строка для режима генерации)
//   callback - функция для получения вычисленного хеша (опционально, для генерации)
func NewChecksumProcessor(expectedHash string, callback func(string)) *ChecksumProcessor {
	return &ChecksumProcessor{
		validate: expectedHash != "",
		expected: expectedHash,
		callback: callback,
	}
}

// ProcessBlock вычисляет xxh3 хеш блока данных.
// В режиме валидации проверяет соответствие ожидаемому значению.
// В режиме генерации вызывает callback с вычисленным хешом.
func (p *ChecksumProcessor) ProcessBlock(ctx context.Context, input []byte) ([]byte, error) {
	if len(input) == 0 {
		return input, nil
	}

	// Вычисляем XXH3 (64-bit) - очень быстрый, использует SIMD (AVX2/SSE)
	h := xxh3.Hash(input)
	actual := hex.EncodeToString(uint64ToBytes(h))

	if p.validate {
		// Режим валидации: проверяем что хеш совпадает
		if actual != p.expected {
			return nil, fmt.Errorf(
				"checksum mismatch: expected %s, got %s (data corruption detected)",
				p.expected, actual,
			)
		}
	}

	// Передаем вычисленный хеш через callback (для генерации)
	if p.callback != nil {
		p.callback(actual)
	}

	return input, nil
}

// uint64ToBytes конвертирует uint64 в байтовый массив (big-endian).
func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return b
}

// bytesToUint64 конвертирует байтовый массив в uint64 (big-endian).
func bytesToUint64(b []byte) uint64 {
	if len(b) != 8 {
		return 0
	}
	var v uint64
	for i := 0; i < 8; i++ {
		v = (v << 8) | uint64(b[i])
	}
	return v
}

// --- Helper Functions ---

// ComputeChecksum вычисляет xxh3 хеш данных и возвращает hex-encoded строку.
func ComputeChecksum(data []byte) string {
	h := xxh3.Hash(data)
	return hex.EncodeToString(uint64ToBytes(h))
}

// ValidateChecksum проверяет соответствие данных ожидаемому хешу.
// Возвращает ошибку если хеш не совпадает.
func ValidateChecksum(data []byte, expectedHash string) error {
	actual := ComputeChecksum(data)
	if actual != expectedHash {
		return fmt.Errorf(
			"checksum validation failed: expected %s, got %s",
			expectedHash, actual,
		)
	}
	return nil
}
