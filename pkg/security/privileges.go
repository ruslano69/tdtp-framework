// Package security provides functionality for the TDTP framework.
package security

import (
	"os"
	"os/user"
	"runtime"
)

// IsAdmin проверяет, запущена ли программа с административными правами.
//
// Для Unix/Linux систем проверяет effective UID (euid == 0 означает root).
// Для Windows пытается открыть защищенный системный ресурс.
//
// Возвращает:
//   - true если программа запущена от root/Administrator
//   - false в противном случае
func IsAdmin() bool {
	if runtime.GOOS == "windows" {
		return isWindowsAdmin()
	}
	// Unix/Linux/macOS: проверяем effective UID
	return os.Geteuid() == 0
}

// isWindowsAdmin проверяет административные права в Windows.
//
// Использует трюк с попыткой открытия защищенного системного ресурса
// (PHYSICALDRIVE0). Обычные пользователи не могут открыть этот ресурс,
// только администраторы.
//
// Возвращает:
//   - true если программа запущена от имени администратора
//   - false в противном случае
func isWindowsAdmin() bool {
	// Попытка открыть защищенный системный ресурс
	// Только администраторы имеют доступ к \\.\PHYSICALDRIVE0
	file, err := os.Open("\\\\.\\PHYSICALDRIVE0")
	if err != nil {
		// Не удалось открыть - нет прав администратора
		return false
	}
	_ = file.Close()
	return true
}

// GetCurrentUser возвращает имя текущего пользователя.
// Использует os/user.Current() — читает реальный UID процесса, не env-переменную.
// Env-переменная USER/USERNAME тривиально подменяется, что позволяло спуфить аудит.
func GetCurrentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	// Fallback на env только если os/user недоступен (редкие минимальные окружения).
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return "unknown"
}
