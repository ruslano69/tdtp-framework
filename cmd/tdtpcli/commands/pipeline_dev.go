//go:build !production

package commands

import (
	"github.com/ruslano69/tdtp-framework/pkg/etl"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
)

// applyDevBinder устанавливает DevClient для локальной генерации ключей шифрования.
// Используется когда --enc-dev флаг активен (только в dev-сборках, не production).
// DevClient генерирует AES-256 ключ локально, не обращаясь к xZMercury.
// ПРЕДУПРЕЖДЕНИЕ: результат можно расшифровать только зная ключ из вывода DevClient.
func applyDevBinder(proc *etl.Processor) {
	proc.WithMercuryBinder(mercury.NewDevClient())
}
