//go:build !production

package main

import "flag"

// registerDevFlags регистрирует флаги доступные только в dev-сборке.
// В продакшен-сборке (go build -tags production) этот файл не компилируется —
// флаг физически отсутствует в бинаре.
func init() {
	// EncryptDev активирует шифрование через DevClient (локальная генерация ключа, без xZMercury).
	// Использование: tdtpcli --enc-dev --pipeline config.yaml
	//
	// ПРЕДУПРЕЖДЕНИЕ: результат можно расшифровать только в рамках одной сессии.
	// Ключ нигде не сохраняется. Только для отладки шифрования.
	flag.Bool("enc-dev", false, "[DEV ONLY] Encrypt output using locally generated key (no xZMercury required). Key is session-scoped and not stored.")
}
