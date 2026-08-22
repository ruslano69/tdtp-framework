package api

import "testing"

// legacyStringPredicate — прежняя проверка из hashes.go, дословно.
func legacyStringPredicate(version string) bool {
	return version <= "1.3.1"
}

// versionAgreement — таблица, по которой обязаны совпадать ДВЕ реализации:
// эта и packet.NeedsRowCountCheck в tdtp-framework. Один и тот же список лежит
// в pkg/core/packet/version_test.go. Если они разойдутся, фреймворк и реестр
// перестанут одинаково отвечать на вопрос "нужен ли этому пакету
// зарегистрированный хеш", и расхождение вылезет уже в бою: экспортёр хеш
// зарегистрирует, а импортёр решит, что регистрация не требовалась, — или
// наоборот, реестр откажет в регистрации пакету, который без неё не пройдёт.
var versionAgreement = map[string]bool{
	// выпущенные версии
	"1.0":   true,
	"1.2":   true,
	"1.3":   true,
	"1.3.1": true,
	"1.4":   false,
	"1.5":   false,
	// граница и эквивалентность недостающих компонентов
	"1.3.2": false,
	"1.4.0": false,
	"2.0":   false,
	// двузначный второй компонент — здесь строковое сравнение врало
	"1.10":  false,
	"1.11":  false,
	"1.25":  false,
	"1.100": false,
	// уже верные до починки
	"10.0":   false,
	"1.3.10": false,
	// мусор — закрываемся, а не открываемся
	"abc":      false,
	"1.x":      false,
	"1..2":     false,
	"-1.0":     false,
	"v1.4":     false,
	"1.4-beta": false,
	// задокументированное исключение
	"": true,
}

func TestIsLegacyPacketVersion_MatchesFramework(t *testing.T) {
	for v, want := range versionAgreement {
		if got := isLegacyPacketVersion(v); got != want {
			t.Errorf("isLegacyPacketVersion(%q) = %v, want %v "+
				"(must match packet.NeedsRowCountCheck in tdtp-framework)", v, got, want)
		}
	}
}

// На выпущенных версиях ответ обязан остаться прежним — иначе это не
// исправление сравнения, а смена того, какие пакеты реестр принимает.
func TestIsLegacyPacketVersion_UnchangedForShippedVersions(t *testing.T) {
	for _, v := range []string{"1.0", "1.2", "1.3", "1.3.1", "1.4", "1.5"} {
		if got, want := isLegacyPacketVersion(v), legacyStringPredicate(v); got != want {
			t.Errorf("version %q: now %v, previously %v", v, got, want)
		}
	}
}

// Починка со стороны реестра: регистрация хеша для версии 1.10 отклонялась с
// формулировкой "requires packet_version >= 1.4", хотя 1.10 как раз новее.
func TestIsLegacyPacketVersion_MultiDigitComponents(t *testing.T) {
	for _, v := range []string{"1.10", "1.11", "1.25", "1.100"} {
		if isLegacyPacketVersion(v) {
			t.Errorf("version %q must be accepted for hash registration", v)
		}
		if !legacyStringPredicate(v) {
			t.Errorf("version %q: the string comparison already handled this correctly — "+
				"the test is pinning the wrong case", v)
		}
	}
}

func TestCompareProtocolVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0},
		{"1.4", "1.4.0", 0},
		{"1.3", "1.3.1", -1},
		{"1.3.1", "1.4", -1},
		{"1.10", "1.3.1", 1},
		{"1.100", "1.99", 1},
		{"2.0", "1.100", 1},
	}
	for _, c := range cases {
		va, okA := parseProtocolVersion(c.a)
		vb, okB := parseProtocolVersion(c.b)
		if !okA || !okB {
			t.Fatalf("parse failed for %q or %q", c.a, c.b)
		}
		if got := compareProtocolVersion(va, vb); got != c.want {
			t.Errorf("compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
