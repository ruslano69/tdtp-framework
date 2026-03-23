//go:build production

package commands

import "github.com/ruslano69/tdtp-framework/pkg/etl"

// applyDevBinder — no-op в production-сборке.
// В production --enc-dev флаг физически отсутствует, EncDev всегда false.
func applyDevBinder(_ *etl.Processor) {}
