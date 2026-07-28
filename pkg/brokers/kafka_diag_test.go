//go:build !nokafka

package brokers

import "testing"

func sized(sizes ...int) [][]byte {
	out := make([][]byte, 0, len(sizes))
	for _, n := range sizes {
		out = append(out, make([]byte, n))
	}
	return out
}

func chunkSizes(chunks [][][]byte) []int {
	out := make([]int, 0, len(chunks))
	for _, c := range chunks {
		total := 0
		for _, m := range c {
			total += len(m)
		}
		out = append(out, total)
	}
	return out
}

// Ни одна порция не должна превышать бюджет — ради этого дробление и написано.
// batch_send из конфига считает сообщения, не байты, поэтому десять пакетов по
// 270 КБ уходили одним запросом на 2.7 МБ и отвергались вместе.
func TestSplitByBudgetKeepsChunksUnderLimit(t *testing.T) {
	const limit = 1000

	for _, tc := range []struct {
		name  string
		input [][]byte
	}{
		{"all fit in one", sized(100, 200, 300)},
		{"exactly at the limit", sized(500, 500)},
		{"just over", sized(500, 501)},
		{"many small", sized(100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100)},
		{"one large among small", sized(100, 900, 100)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, total := range chunkSizes(splitByBudget(tc.input, limit)) {
				if total > limit {
					t.Errorf("chunk %d is %d bytes, over the %d budget", i, total, limit)
				}
			}
		})
	}
}

// Ни одно сообщение не должно потеряться или задвоиться.
func TestSplitByBudgetPreservesEverything(t *testing.T) {
	input := sized(100, 2000, 300, 400, 50)

	got := 0
	for _, chunk := range splitByBudget(input, 1000) {
		got += len(chunk)
	}

	if got != len(input) {
		t.Errorf("got %d messages across chunks, want %d", got, len(input))
	}
}

// Сообщение крупнее предела уезжает отдельно: дробление ему не поможет, но так
// оно не утянет соседей, и отказ укажет именно на него.
func TestSplitByBudgetIsolatesOversized(t *testing.T) {
	chunks := splitByBudget(sized(100, 5000, 100), 1000)

	var lone bool
	for _, c := range chunks {
		if len(c) == 1 && len(c[0]) == 5000 {
			lone = true
		}
		if len(c) > 1 {
			for _, m := range c {
				if len(m) == 5000 {
					t.Error("oversized message was batched with others")
				}
			}
		}
	}
	if !lone {
		t.Error("oversized message did not get a chunk of its own")
	}
}

// Неизвестный предел (брокер не ответил) не должен приводить к дроблению на
// произвольное число: лучше отправить как есть и дать брокеру решить.
func TestSplitByBudgetUnknownLimitDoesNotSplit(t *testing.T) {
	input := sized(100, 200, 300)

	if chunks := splitByBudget(input, 0); len(chunks) != 1 {
		t.Errorf("got %d chunks with an unknown limit, want 1", len(chunks))
	}
}

func TestSplitByBudgetEmpty(t *testing.T) {
	if chunks := splitByBudget(nil, 1000); len(chunks) != 0 {
		t.Errorf("got %d chunks for no messages, want 0", len(chunks))
	}
}

// Отказ по размеру распознаётся по тексту: kafka-go склеивает ошибки записей
// батча в строку, и errors.Is до kafka.MessageSizeTooLarge не достаёт.
func TestIsMessageTooLarge(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"kafka-go wording", errString("Kafka write errors (10/10), errors: [[10] Message Size Too Large: ...]"), true},
		{"protocol name", errString("MESSAGE_TOO_LARGE"), true},
		{"unrelated", errString("dial tcp: connection refused"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMessageTooLarge(tc.err); got != tc.want {
				t.Errorf("isMessageTooLarge = %v, want %v", got, tc.want)
			}
		})
	}
}

// Отчёт об отказе обязан называть каждый пакет: раньше наружу уходил только
// счётчик, и причина до пользователя не доезжала.
func TestClassifyWriteErrorNamesEachPacket(t *testing.T) {
	err := classifyWriteError(errString("Message Size Too Large"), sized(300, 400, 5000), 1000)
	if err == nil {
		t.Fatal("expected an error")
	}

	msg := err.Error()
	for _, want := range []string{"packet 1/3", "packet 2/3", "packet 3/3", "300 bytes", "5000 bytes"} {
		if !contains(msg, want) {
			t.Errorf("message does not mention %q:\n%s", want, msg)
		}
	}
	if !contains(msg, "over the limit on its own") {
		t.Error("the packet that exceeds the limit alone is not marked as such")
	}
}

// Не связанная с размером ошибка не должна маскироваться под превышение.
func TestClassifyWriteErrorPassesOtherFailuresThrough(t *testing.T) {
	err := classifyWriteError(errString("connection refused"), sized(10), 1000)
	if err == nil {
		t.Fatal("expected an error")
	}
	if contains(err.Error(), "over the broker limit") {
		t.Errorf("a connection failure was reported as a size problem:\n%s", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}
