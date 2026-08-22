package api

import "strconv"

// Зеркало packet.NeedsRowCountCheck из tdtp-framework.
//
// Дублируется намеренно: xzmercury — самостоятельный модуль, и его go.mod
// пришпилен к tdtp-framework v1.9.6, то есть к версии заметно старше той, где
// живёт оригинал. Тянуть зависимость ради одного предиката дороже, чем держать
// копию в двадцать строк, — но копия обязана отвечать так же, иначе фреймворк и
// реестр разойдутся в том, какой пакет требует зарегистрированного хеша.
// Согласие закреплено таблицей в version_test.go по обе стороны.
//
// Сравнение числовое, а не строковое. Раньше здесь стояло
// `req.PacketVersion <= "1.3.1"`, и на выпущенных версиях это давало верный
// ответ по совпадению порядка символов; на двузначном втором компоненте
// совпадение кончается — "1.10" строкой меньше "1.3.1", потому что '1' < '3'.
// Реестр начал бы отклонять регистрацию хеша для версии 1.10 с формулировкой
// "requires packet_version >= 1.4", хотя она как раз новее.

// isLegacyPacketVersion сообщает, что версия относится к до-1.4 протоколу:
// такие пакеты живут на счёте строк и хеши в реестр не регистрируют.
//
// Неразбираемая версия считается НОВЕЕ любой известной и легаси НЕ является —
// так мусор в поле версии не открывает обход регистрации. Пустая строка
// трактуется как легаси, повторяя прежнее поведение.
func isLegacyPacketVersion(version string) bool {
	if version == "" {
		return true
	}
	v, ok := parseProtocolVersion(version)
	if !ok {
		return false
	}
	legacyMax, _ := parseProtocolVersion("1.3.1")
	return compareProtocolVersion(v, legacyMax) <= 0
}

func parseProtocolVersion(s string) ([]int, bool) {
	if s == "" {
		return nil, false
	}
	var (
		parts []int
		cur   string
	)
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '.' {
			if cur == "" {
				return nil, false
			}
			n, err := strconv.Atoi(cur)
			if err != nil || n < 0 {
				return nil, false
			}
			parts = append(parts, n)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	return parts, true
}

// compareProtocolVersion возвращает -1, 0 или +1. Недостающие компоненты
// считаются нулями, поэтому "1.4" равно "1.4.0", а "1.3" меньше "1.3.1".
func compareProtocolVersion(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
