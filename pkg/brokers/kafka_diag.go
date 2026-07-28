package brokers

// kafka_diag.go — сказать про размер до отправки и разобрать отказ после неё.
//
// Раньше превышение выяснялось только по факту: брокер возвращал
// "Message Size Too Large", а данные к тому моменту уже прочитаны из БД,
// сериализованы и сжаты. Здесь два дополнения к этому пути — предупреждение
// заранее и разбор отказа поимённо.

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
)

// warnOnce не даёт превратить предупреждение в шум: на партиционированном
// экспорте оно иначе повторится на каждой части.
var warnedOversize atomic.Bool

// splitByBudget режет партию на порции, каждая из которых укладывается в
// limit по несжатому размеру.
//
// Сообщение, которое само по себе больше предела, уходит отдельной порцией:
// дробление ему не поможет, зато так оно не утянет за собой соседей, и отказ
// в classifyWriteError укажет именно на него.
func splitByBudget(messages [][]byte, limit int) [][][]byte {
	if limit <= 0 {
		return [][][]byte{messages}
	}

	var chunks [][][]byte
	var current [][]byte
	size := 0

	for _, m := range messages {
		if len(m) > limit {
			if len(current) > 0 {
				chunks = append(chunks, current)
				current, size = nil, 0
			}
			chunks = append(chunks, [][]byte{m})
			continue
		}

		if len(current) > 0 && size+len(m) > limit {
			chunks = append(chunks, current)
			current, size = nil, 0
		}

		current = append(current, m)
		size += len(m)
	}

	if len(current) > 0 {
		chunks = append(chunks, current)
	}

	return chunks
}

// warnIfSingleOversized предупреждает про сообщение, которое не спасёт никакое
// дробление.
//
// Раньше предупреждение выдавалось на любой батч сверх предела — но батч
// теперь режется автоматически, так что говорить об этом стало не о чем.
// Осталось то, чего автоматика не решает: одна часть крупнее, чем брокер
// готов принять. Лечится только уменьшением packet_kb или настройкой брокера.
func (k *Kafka) warnIfSingleOversized(messages [][]byte, limit int) {
	if limit <= 0 {
		return
	}

	for _, m := range messages {
		if len(m) <= limit {
			continue
		}
		if warnedOversize.Swap(true) {
			return
		}

		log.Printf("WARNING: a single Kafka message is %d bytes against a %d byte limit. "+
			"Splitting the batch cannot help — one packet does not fit. The broker "+
			"measures the compressed batch, so this may still get through if the data "+
			"compresses; high-entropy data (UUID, base64, ciphertext) will not. Lower "+
			"kafka.packet_kb, or raise the broker's message.max.bytes — %d bytes is the "+
			"recommended setting for TDTP.",
			len(m), limit, RecommendedMessageMaxBytes)
		return
	}
}

// classifyWriteError превращает отказ записи в отчёт по каждому пакету.
//
// kafka-go возвращает на батч одну ошибку, внутри которой перечислены ошибки
// отдельных записей — а вызывающий код выше по стеку сворачивал это в счётчик
// ("streaming export completed with 2 errors"), так что причина не доезжала
// до пользователя вовсе. Здесь она называется, и при отказе по размеру
// добавляется то, что с ней делать.
func classifyWriteError(err error, messages [][]byte, limit int) error {
	if err == nil {
		return nil
	}

	if !isMessageTooLarge(err) {
		return fmt.Errorf("failed to write batch to Kafka (%d messages): %w", len(messages), err)
	}

	total := 0
	for _, m := range messages {
		total += len(m)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Kafka rejected the batch: message size over the broker limit")
	if limit > 0 {
		fmt.Fprintf(&b, " (%d bytes)", limit)
	}
	fmt.Fprintf(&b, ". Batch: %d message(s), %d bytes uncompressed", len(messages), total)

	for i, m := range messages {
		over := ""
		if limit > 0 && len(m) > limit {
			over = " — over the limit on its own"
		}
		fmt.Fprintf(&b, "\n  packet %d/%d: %d bytes%s", i+1, len(messages), len(m), over)
	}

	fmt.Fprintf(&b, "\nThe limit applies to the whole batch, not to one record: "+
		"lower kafka.batch_send to send fewer per request, lower kafka.packet_kb to make "+
		"each packet smaller, or set the broker's message.max.bytes to %d "+
		"(recommended for TDTP).", RecommendedMessageMaxBytes)
	fmt.Fprintf(&b, "\nunderlying error: %v", err)

	return fmt.Errorf("%s", b.String())
}

// isMessageTooLarge распознаёт отказ по размеру.
//
// По тексту, а не по типу: kafka-go отдаёт на батч составную ошибку, в
// которой ошибки отдельных записей уже склеены в строку, так что errors.Is
// до kafka.MessageSizeTooLarge не достаёт.
func isMessageTooLarge(err error) bool {
	return strings.Contains(err.Error(), "Message Size Too Large") ||
		strings.Contains(err.Error(), "MESSAGE_TOO_LARGE")
}
