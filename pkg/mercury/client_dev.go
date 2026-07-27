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

// RegisterHash удовлетворяет pipeline.HashRegistrar, ничего никуда не отправляя.
//
// Без этого метода --enc-dev не работал вовсе, хотя в справке флага написано
// "no xZMercury required". Причина в резолвинге: exporter.resolveHashRegistrar
// берёт кастомный биндер только если тот приводится к HashRegistrar, а
// DevClient умел один BindKey — приведение не проходило, и обязательная для
// v1.5 регистрация уходила в mercury.NewClient("") с пустым URL. Получалось
// `Post "/api/hashes/": unsupported protocol scheme ""`: шифрование
// деградировало, выходной файл не создавался.
//
// Пустая реализация здесь — не заглушка ради зелёного теста, а точное
// отражение режима. Реестр хешей существует внутри xZMercury; в dev-режиме
// его нет, регистрировать не у кого. Проверять регистрацию тоже некому:
// ключ живёт только в этом процессе и никуда не сохраняется, так что
// потребителя, который вызвал бы VerifyAndPrepare, в принципе не будет.
// Сами хеши при этом остаются на месте — ComputeIntegrity штампует пакет
// до этого вызова, так что формат v1.5 не нарушается.
//
// Проверка xxh3 сохранена такой же, как у настоящего клиента: забытый
// ComputeIntegrity — это ошибка вызывающего кода, и она должна быть видна
// в dev-режиме ровно так же, как в бою.
//
// Файл собирается только без -tags production, поэтому обойти этим
// настоящую регистрацию в продакшен-сборке невозможно.
func (d *DevClient) RegisterHash(_ context.Context, _ string, _ int, xxh3, _, _, _ string) error {
	if xxh3 == "" {
		return fmt.Errorf("dev: RegisterHash: xxh3 is empty (call ComputeIntegrity first)")
	}
	return nil
}

// VerifyHMACDev всегда возвращает true в dev-режиме.
func VerifyHMACDev(_, _, _ string) bool {
	return true
}
