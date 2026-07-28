package brokers

// kafka_diag.go — сказать про размер до отправки и разобрать отказ после неё.
//
// Раньше превышение выяснялось только по факту: брокер возвращал
// "Message Size Too Large", а данные к тому моменту уже прочитаны из БД,
// сериализованы и сжаты. Здесь два дополнения к этому пути — предупреждение
// заранее и разбор отказа поимённо.

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
)

// warnOnce не даёт превратить предупреждение в шум: на партиционированном
// экспорте оно иначе повторится на каждой части.
var warnedOversize atomic.Bool

// warnIfOversized предупреждает, если несжатый размер уже перерос предел
// брокера, но отправку не отменяет.
//
// Отправлять всё равно правильно: брокер меряет сжатый record batch, а
// kafka.Writer сжимает (Snappy). Обычные табличные данные ужимаются втрое и
// проходят — именно поэтому проблему годами не замечали. Но коэффициент
// определяется данными: UUID, base64 и шифротекст не сжимаются вовсе, и там
// тот же экспорт отвергается. Предсказать, чем кончится, здесь нельзя —
// поэтому предупреждение, а не отказ.
func (k *Kafka) warnIfOversized(ctx context.Context, messages [][]byte) {
	limit, known := k.MessageMaxBytes(ctx)

	total := 0
	largest := 0
	for _, m := range messages {
		total += len(m)
		if len(m) > largest {
			largest = len(m)
		}
	}

	// Предел применяется к record batch целиком, а не к отдельной записи:
	// десять пакетов по 200 КБ в одном вызове дают 2 МБ и отвергаются
	// вместе, хотя каждый по отдельности прошёл бы.
	if total <= limit {
		return
	}
	if warnedOversize.Swap(true) {
		return
	}

	source := "broker"
	if !known {
		source = "Kafka default (broker did not answer DescribeConfigs)"
	}

	log.Printf("WARNING: Kafka batch is %d bytes uncompressed against a %d byte limit (%s). "+
		"Sending anyway — the broker measures the compressed batch, and ordinary table data "+
		"usually fits. High-entropy data (UUID, base64, ciphertext) will not: it does not "+
		"compress, and the send is rejected. If that happens, lower kafka.packet_kb or "+
		"kafka.batch_send, or raise the broker's message.max.bytes — %d bytes is the "+
		"recommended setting for TDTP.",
		total, limit, source, RecommendedMessageMaxBytes)

	if largest > limit {
		log.Printf("WARNING: one packet alone is %d bytes, over the %d byte limit — "+
			"batching cannot help; kafka.packet_kb has to come down.", largest, limit)
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
