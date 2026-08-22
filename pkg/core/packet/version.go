package packet

import "strconv"

// ProtocolVersion — разобранная версия протокола TDTP ("1.0", "1.3.1", "1.4").
//
// Компоненты сравниваются как числа, а не как символы. До появления этого типа
// версии сравнивались строкой (`version <= "1.3.1"`), и на нынешнем наборе
// значений это давало верный ответ по совпадению: '0', '2', '3' идут в таблице
// символов до '4'. Совпадение кончается на двузначном компоненте —
// "1.10" строкой меньше "1.3.1", потому что '1' < '3', то есть версия 1.10
// молча считалась бы ДОревней 1.3.1 и переставала требовать зарегистрированный
// хеш. Отказ тихий: пакет импортируется, просто без проверки, которую его
// версия обязана требовать.
type ProtocolVersion struct {
	parts []int
}

// ParseProtocolVersion разбирает строку версии.
//
// ok=false означает, что строка не является версией: пустая, с нечисловым
// компонентом, с пустым компонентом ("1..2") или с отрицательным числом.
// Решение, что делать с таким значением, принимает вызывающий — см.
// NeedsRowCountCheck.
func ParseProtocolVersion(s string) (ProtocolVersion, bool) {
	if s == "" {
		return ProtocolVersion{}, false
	}
	var (
		parts []int
		cur   string
	)
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '.' {
			if cur == "" {
				return ProtocolVersion{}, false
			}
			n, err := strconv.Atoi(cur)
			if err != nil || n < 0 {
				return ProtocolVersion{}, false
			}
			parts = append(parts, n)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	return ProtocolVersion{parts: parts}, true
}

// Compare возвращает -1, 0 или +1.
//
// Отсутствующие компоненты считаются нулями, поэтому "1.4" и "1.4.0" равны, а
// "1.3" меньше "1.3.1".
func (v ProtocolVersion) Compare(other ProtocolVersion) int {
	n := len(v.parts)
	if len(other.parts) > n {
		n = len(other.parts)
	}
	for i := 0; i < n; i++ {
		a, b := 0, 0
		if i < len(v.parts) {
			a = v.parts[i]
		}
		if i < len(other.parts) {
			b = other.parts[i]
		}
		if a != b {
			if a < b {
				return -1
			}
			return 1
		}
	}
	return 0
}

// CompareProtocolVersions сравнивает две строки версий.
//
// ok=false, если хотя бы одна из них не разбирается; результат в этом случае
// смысла не имеет и использовать его нельзя.
func CompareProtocolVersions(a, b string) (int, bool) {
	va, okA := ParseProtocolVersion(a)
	vb, okB := ParseProtocolVersion(b)
	if !okA || !okB {
		return 0, false
	}
	return va.Compare(vb), true
}

// versionLegacyMax — последняя версия, живущая на счёте строк вместо хешей.
const versionLegacyMax = "1.3.1"
