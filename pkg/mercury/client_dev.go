//go:build !production

package mercury

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// DevClient генерирует ключ локально без обращения к xZMercury.
// Доступен только в не-продакшен сборках (go build без -tags production).
// Использование: tdtpcli --enc-dev --pipeline config.yaml
//
// ВНИМАНИЕ: ключ не сохраняется нигде — расшифровать результат
// можно только в рамках одной сессии. Для отладки шифрования, не для продакшена.
type DevClient struct{}

// NewDevClient создаёт dev-клиент (доступен только без -tags production).
func NewDevClient() *DevClient {
	return &DevClient{}
}

// BindKey генерирует случайный AES-256 ключ локально.
// HMAC возвращается как фиктивный — верификация в dev-режиме всегда проходит.
func (d *DevClient) BindKey(_ context.Context, packageUUID, _ string) (*KeyBinding, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("dev: generate key: %w", err)
	}

	return &KeyBinding{
		KeyB64: base64.StdEncoding.EncodeToString(key),
		HMAC:   "dev-mode-no-hmac-verification",
	}, nil
}

// VerifyHMACDev всегда возвращает true в dev-режиме.
func VerifyHMACDev(_, _, _ string) bool {
	return true
}
