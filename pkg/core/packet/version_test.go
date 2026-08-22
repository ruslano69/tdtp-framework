package packet

import (
	"fmt"
	"math/rand"
	"testing"
)

// legacyStringPredicate — прежняя реализация NeedsRowCountCheck, дословно.
// Держим её здесь как эталон: правка обязана совпадать с ней на всех версиях,
// которые протокол уже использовал, и расходиться только там, где строковое
// сравнение было неверным.
func legacyStringPredicate(version string) bool {
	return version <= "1.3.1"
}

// Версии, которые протокол реально выпускал. На них правка обязана быть
// незаметной — иначе это не исправление, а смена поведения.
var shippedVersions = []string{"1.0", "1.2", "1.3", "1.3.1", "1.4", "1.5"}

func TestNeedsRowCountCheck_UnchangedForShippedVersions(t *testing.T) {
	for _, v := range shippedVersions {
		got, want := NeedsRowCountCheck(v), legacyStringPredicate(v)
		if got != want {
			t.Errorf("version %q: now %v, previously %v — shipped versions must not change",
				v, got, want)
		}
	}
}

func TestNeedsRowCountCheck_KnownAnswers(t *testing.T) {
	cases := map[string]bool{
		"1.0":   true,  // до 1.4 — счёт строк
		"1.2":   true,  // сжатие появилось здесь, целостности ещё нет
		"1.3":   true,
		"1.3.1": true,  // последняя легаси-версия, граница включительно
		"1.3.2": false, // за границей
		"1.4":   false, // xxh3 появился здесь
		"1.4.0": false, // отсутствующий компонент — ноль, равно "1.4"
		"1.5":   false,
		"2.0":   false,
	}
	for v, want := range cases {
		if got := NeedsRowCountCheck(v); got != want {
			t.Errorf("NeedsRowCountCheck(%q) = %v, want %v", v, got, want)
		}
	}
}

// Собственно починка: второй компонент из двух и более цифр. Строковое
// сравнение отвечало здесь true, потому что '1' < '3', — то есть версия 1.10
// считалась бы ДОревней 1.3.1 и переставала требовать зарегистрированный хеш.
func TestNeedsRowCountCheck_MultiDigitComponents(t *testing.T) {
	for _, v := range []string{"1.10", "1.11", "1.25", "1.100"} {
		if NeedsRowCountCheck(v) {
			t.Errorf("version %q is newer than %s and must require integrity, got legacy handling",
				v, versionLegacyMax)
		}
		// Если это перестанет падать на прежней реализации, значит случай
		// выбран неверно и тест ничего не доказывает.
		if !legacyStringPredicate(v) {
			t.Errorf("version %q: the string comparison already handled this correctly — "+
				"the test is pinning the wrong case", v)
		}
	}
}

// Не всякий многозначный номер был сломан, и преувеличивать масштаб починки не
// нужно: здесь строковое сравнение давало верный ответ само по себе — у "10.0"
// второй символ '0' идёт после '.', а "1.3.10" длиннее своего префикса "1.3.1".
// Новая реализация обязана отвечать так же.
func TestNeedsRowCountCheck_AlreadyCorrectBeforeTheFix(t *testing.T) {
	for _, v := range []string{"10.0", "1.3.10", "2.0", "1.4"} {
		if NeedsRowCountCheck(v) != legacyStringPredicate(v) {
			t.Errorf("version %q: answer changed (%v → %v) although the string comparison was right",
				v, legacyStringPredicate(v), NeedsRowCountCheck(v))
		}
	}
}

// Мусор в поле версии не должен открывать путь в обход проверки целостности.
func TestNeedsRowCountCheck_MalformedFailsClosed(t *testing.T) {
	for _, v := range []string{"abc", "1.x", "1..2", "-1.0", "v1.4", "1.4-beta", " 1.4", "1,4"} {
		if NeedsRowCountCheck(v) {
			t.Errorf("version %q is not parseable and must fail closed (integrity required)", v)
		}
	}
	// Пустая строка — задокументированное исключение, см. NeedsRowCountCheck.
	if !NeedsRowCountCheck("") {
		t.Error(`version "" must keep its legacy handling`)
	}
}

func TestParseProtocolVersion(t *testing.T) {
	ok := []string{"1", "1.0", "1.3.1", "0.0.0", "10.20.30", "1.0.0.0"}
	for _, s := range ok {
		if _, valid := ParseProtocolVersion(s); !valid {
			t.Errorf("ParseProtocolVersion(%q) rejected a valid version", s)
		}
	}
	bad := []string{"", ".", "1.", ".1", "1..2", "abc", "1.a", "-1", "1.-2", "1 .2", "١.٢"}
	for _, s := range bad {
		if _, valid := ParseProtocolVersion(s); valid {
			t.Errorf("ParseProtocolVersion(%q) accepted a malformed version", s)
		}
	}
}

func TestCompareProtocolVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0},
		{"1.4", "1.4.0", 0},   // недостающий компонент — ноль
		{"1.0.0.0", "1", 0},   // и в другую сторону
		{"1.3", "1.3.1", -1},
		{"1.3.1", "1.4", -1},
		{"1.4", "1.3.1", 1},
		{"1.10", "1.3.1", 1},  // здесь строковое сравнение врало
		{"1.25", "1.4", 1},
		{"2.0", "1.100", 1},
		{"1.100", "1.99", 1},
	}
	for _, c := range cases {
		got, ok := CompareProtocolVersions(c.a, c.b)
		if !ok {
			t.Errorf("CompareProtocolVersions(%q, %q) reported malformed input", c.a, c.b)
			continue
		}
		if got != c.want {
			t.Errorf("CompareProtocolVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if _, ok := CompareProtocolVersions("1.0", "abc"); ok {
		t.Error("malformed operand must be reported, not compared")
	}
}

// Сравнение обязано быть согласованным: антисимметричным и транзитивным.
// Проверяется на случайных версиях, включая двузначные компоненты, где прежняя
// строковая реализация как раз и нарушала порядок.
func TestCompareProtocolVersions_IsATotalOrder(t *testing.T) {
	rng := rand.New(rand.NewSource(20260822))
	gen := func() string {
		switch rng.Intn(3) {
		case 0:
			return fmt.Sprintf("%d.%d", rng.Intn(30), rng.Intn(30))
		case 1:
			return fmt.Sprintf("%d.%d.%d", rng.Intn(5), rng.Intn(30), rng.Intn(30))
		default:
			return fmt.Sprintf("%d", rng.Intn(5))
		}
	}
	for i := 0; i < 20000; i++ {
		a, b, c := gen(), gen(), gen()
		ab, _ := CompareProtocolVersions(a, b)
		ba, _ := CompareProtocolVersions(b, a)
		if ab != -ba {
			t.Fatalf("not antisymmetric: cmp(%q,%q)=%d but cmp(%q,%q)=%d", a, b, ab, b, a, ba)
		}
		bc, _ := CompareProtocolVersions(b, c)
		ac, _ := CompareProtocolVersions(a, c)
		if ab <= 0 && bc <= 0 && ac > 0 {
			t.Fatalf("not transitive: %q <= %q <= %q but cmp(%q,%q)=%d", a, b, c, a, c, ac)
		}
	}
}

// versionAgreement — таблица, по которой обязаны совпадать ДВЕ реализации
// предиката: эта и isLegacyPacketVersion в xzmercury/internal/api. Один и тот
// же список лежит там в version_test.go. Копия существует потому, что
// xzmercury — самостоятельный модуль, пришпиленный к старой версии фреймворка;
// разойдись они, экспортёр и реестр перестанут одинаково отвечать на вопрос
// "нужен ли этому пакету зарегистрированный хеш", и расхождение вылезет в бою.
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

func TestNeedsRowCountCheck_AgreesWithMercury(t *testing.T) {
	for v, want := range versionAgreement {
		if got := NeedsRowCountCheck(v); got != want {
			t.Errorf("NeedsRowCountCheck(%q) = %v, want %v "+
				"(must match isLegacyPacketVersion in xzmercury/internal/api)", v, got, want)
		}
	}
}
