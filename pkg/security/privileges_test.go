package security

import (
	"os"
	"runtime"
	"testing"
)

func TestIsAdmin(t *testing.T) {
	// Этот тест проверяет только что функция работает без паники
	// Фактический результат зависит от прав запуска теста
	result := IsAdmin()

	// Проверяем, что возвращается bool
	if result != true && result != false {
		t.Error("IsAdmin should return bool")
	}

	// Для CI/CD обычно запускается не от root
	// Поэтому просто проверяем, что функция не падает
	t.Logf("IsAdmin() = %v (OS: %s)", result, runtime.GOOS)
}

func TestIsAdminUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping Unix test on Windows")
	}

	// Проверяем логику для Unix
	euid := os.Geteuid()
	expectedAdmin := (euid == 0)
	actualAdmin := IsAdmin()

	if expectedAdmin != actualAdmin {
		t.Errorf("IsAdmin() = %v, expected %v (euid=%d)", actualAdmin, expectedAdmin, euid)
	}
}

func TestIsWindowsAdmin(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows test on non-Windows OS")
	}

	// На Windows просто проверяем, что функция работает
	result := isWindowsAdmin()
	t.Logf("isWindowsAdmin() = %v", result)

	// Проверяем, что возвращается bool
	if result != true && result != false {
		t.Error("isWindowsAdmin should return bool")
	}
}

func TestGetCurrentUser(t *testing.T) {
	user := GetCurrentUser()

	if user == "" {
		t.Error("GetCurrentUser should not return empty string")
	}

	// Проверяем, что вернулось что-то осмысленное
	if user != "unknown" {
		t.Logf("Current user: %s", user)
	} else {
		t.Log("Could not determine current user")
	}
}

// Benchmark для проверки производительности
func BenchmarkIsAdmin(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = IsAdmin()
	}
}

func BenchmarkGetCurrentUser(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = GetCurrentUser()
	}
}
