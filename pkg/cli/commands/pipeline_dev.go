//go:build !production

package commands

import (
	"github.com/ruslano69/tdtp-framework/pkg/etl"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
)

// applyDevBinder устанавливает DevClient для локальной генерации ключей шифрования.
// Используется когда --enc-dev флаг активен (только в dev-сборках, не production).
// DevClient генерирует AES-256 ключ локально, не обращаясь к xZMercury.
//
// Одного этого флага достаточно — ни MERCURY_SERVER_SECRET, ни что-либо ещё
// подстраивать не нужно: освобождение от HMAC-верификации объявляет сам
// DevClient (HMACVerificationExempt). Так было не всегда, и это важно для
// сценария, ради которого режим существует, — аварийного отката при недоступном
// Redis: на боевом сервере секрет выставлен всегда, а прежний обход включался
// только строкой "dev-mode" в нём, так что --enc-dev в одиночку тихо
// деградировал в error-пакет именно во время аварии.
//
// ПРЕДУПРЕЖДЕНИЕ: результат можно расшифровать только зная ключ из вывода DevClient.
func applyDevBinder(proc *etl.Processor) {
	proc.WithMercuryBinder(mercury.NewDevClient())
}
