//go:build !nokafka

package brokers

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

const benchTopic = "tdtp-bench"

func newBenchBroker(b *testing.B) *Kafka {
	b.Helper()
	cfg := Config{
		Type:          "kafka",
		Brokers:       []string{"localhost:9092"},
		Topic:         benchTopic,
		ConsumerGroup: "tdtp-bench-group",
	}
	k, err := NewKafka(cfg)
	if err != nil {
		b.Fatalf("NewKafka: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := k.Connect(ctx); err != nil {
		b.Skipf("Kafka not available: %v", err)
	}
	return k
}

// departments и notes — реалистичные значения для имитации HR-данных.
// Разнообразный контент хуже сжимается (ближе к реальным данным).
var (
	deptNames = []string{
		"Engineering", "Human Resources", "Finance", "Marketing", "Operations",
		"Legal", "IT Support", "Product Management", "Sales", "Customer Success",
	}
	noteTemplates = []string{
		"Transferred from regional office on probation review 2024-Q3",
		"Annual performance rating: exceeds expectations; bonus approved",
		"Remote work arrangement approved until end of fiscal year 2026",
		"Pending background check completion; start date adjusted to next quarter",
		"Project lead for ERP migration; access to production systems granted",
		"Medical leave 2025-11-01 to 2025-12-15; return confirmed by HR director",
		"Promotion from Junior to Senior level effective 2026-01-01",
		"Visa sponsorship in progress; work authorization valid through 2027",
		"Second disciplinary notice issued; follow-up meeting scheduled",
		"Exit interview completed; knowledge transfer documentation in progress",
	}
)

// makeTDTPPacket генерирует синтетический TDTP XML-пакет размером ~sizeKB КБ.
// Структура приближена к реальному пакету: Header + Schema + Data rows.
// Контент разнообразный (HR-данные) — компрессия близка к реальным данным.
func makeTDTPPacket(partNum, totalParts, numRows, sizeKB int) []byte {
	var buf bytes.Buffer
	buf.Grow(sizeKB * 1024)

	fmt.Fprintf(&buf, `<?xml version="1.0" encoding="UTF-8"?>
<TDTPPacket version="1.0">
  <Header>
    <Type>reference</Type>
    <TableName>employees</TableName>
    <MessageID>BENCH-2026-%08d</MessageID>
    <PartNumber>%d</PartNumber>
    <TotalParts>%d</TotalParts>
    <RecordsInPart>%d</RecordsInPart>
    <Timestamp>2026-04-08T12:00:00Z</Timestamp>
  </Header>
  <Schema version="1">
    <Field name="ID" type="INTEGER" key="true"/>
    <Field name="FullName" type="TEXT"/>
    <Field name="Email" type="TEXT"/>
    <Field name="Department" type="TEXT"/>
    <Field name="Position" type="TEXT"/>
    <Field name="Salary" type="DECIMAL" precision="12" scale="2"/>
    <Field name="HireDate" type="DATE"/>
    <Field name="TermDate" type="DATE"/>
    <Field name="IsActive" type="BOOLEAN"/>
    <Field name="Notes" type="TEXT"/>
  </Schema>
  <Data>`, partNum, partNum, totalParts, numRows)

	// Целевой размер строки с учётом XML-overhead (~80 байт тегов)
	rowBudget := sizeKB*1024/numRows - 80
	if rowBudget < 120 {
		rowBudget = 120
	}

	// Базовая длина строки данных (без доп. паддинга): ~160 байт
	baseRowLen := 160
	extraPerRow := rowBudget - baseRowLen
	if extraPerRow < 0 {
		extraPerRow = 0
	}

	for i := 0; i < numRows; i++ {
		id := partNum*100000 + i
		dept := deptNames[i%len(deptNames)]
		note := noteTemplates[i%len(noteTemplates)]

		// Дополняем notes до нужного размера строки разнообразным суффиксом
		if extraPerRow > 0 {
			note = fmt.Sprintf("%s | ref#%07d | payroll-id: PAY-%06d-%04d",
				note, id, id%999999, i%9999)
			// Добрасываем остаток уникальными данными
			if remaining := extraPerRow - len(note) + len(noteTemplates[i%len(noteTemplates)]); remaining > 0 {
				note += fmt.Sprintf(" | %0*d", remaining, id*7+i*13)
			}
		}

		salary := 45000.00 + float64(id%80000)
		hireYear := 2010 + (i % 15)
		hireMonth := 1 + (i % 12)
		termDate := "NULL"
		isActive := "1"
		if i%11 == 0 {
			termDate = fmt.Sprintf("%d-%02d-01", hireYear+5, hireMonth)
			isActive = "0"
		}

		fmt.Fprintf(&buf,
			"\n    <Row><f>%d</f><f>%s %s</f><f>%s.%s@company.example.com</f>"+
				"<f>%s</f><f>Senior Specialist</f><f>%.2f</f>"+
				"<f>%d-%02d-15</f><f>%s</f><f>%s</f><f>%s</f></Row>",
			id,
			firstNames[i%len(firstNames)], lastNames[i%len(lastNames)],
			firstNames[i%len(firstNames)], lastNames[i%len(lastNames)],
			dept, salary,
			hireYear, hireMonth,
			termDate, isActive, note,
		)
	}

	buf.WriteString("\n  </Data>\n</TDTPPacket>")
	return buf.Bytes()
}

var firstNames = []string{
	"Alexander", "Natalia", "Dmitry", "Elena", "Sergey", "Olga", "Mikhail", "Anna",
	"Vladimir", "Irina", "Andrey", "Maria", "Pavel", "Tatiana", "Nikolay", "Yulia",
}
var lastNames = []string{
	"Ivanov", "Smirnova", "Kuznetsov", "Popova", "Sokolov", "Lebedeva", "Kozlov",
	"Novikova", "Morozov", "Petrova", "Volkov", "Sokolova", "Alekseev", "Fedorova",
}

// ─── Benchmark scenarios ──────────────────────────────────────────────────────

// BenchmarkSendSequential — текущее поведение: N пакетов, каждый Send() отдельно.
// Это baseline: N RTT * (serialize + network).
func BenchmarkSendSequential(b *testing.B) {
	for _, tc := range packetCases() {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			k := newBenchBroker(b)
			defer k.Close()

			payloads := buildPayloads(tc)
			totalBytes := totalSize(payloads)

			ctx := context.Background()
			b.ResetTimer()
			b.SetBytes(int64(totalBytes))

			for n := 0; n < b.N; n++ {
				for _, p := range payloads {
					if err := k.Send(ctx, p); err != nil {
						b.Fatalf("Send: %v", err)
					}
				}
			}

			b.ReportMetric(float64(len(payloads)), "packets/op")
			b.ReportMetric(float64(totalBytes)/1024/1024, "MB/op")
		})
	}
}

// BenchmarkSendBatch — единый SendBatch(): все пакеты одним WriteMessages.
// Ожидаем: 1 RTT вместо N.
func BenchmarkSendBatch(b *testing.B) {
	for _, tc := range packetCases() {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			k := newBenchBroker(b)
			defer k.Close()

			payloads := buildPayloads(tc)
			totalBytes := totalSize(payloads)

			ctx := context.Background()
			b.ResetTimer()
			b.SetBytes(int64(totalBytes))

			for n := 0; n < b.N; n++ {
				if err := k.SendBatch(ctx, payloads); err != nil {
					b.Fatalf("SendBatch: %v", err)
				}
			}

			b.ReportMetric(float64(len(payloads)), "packets/op")
			b.ReportMetric(float64(totalBytes)/1024/1024, "MB/op")
		})
	}
}

// BenchmarkSendParallel — worker pool: W горутин, каждая отправляет свою часть пакетов.
// Моделирует параллельную сериализацию + отправку.
func BenchmarkSendParallel(b *testing.B) {
	workers := runtime.NumCPU()

	for _, tc := range packetCases() {
		tc := tc
		b.Run(fmt.Sprintf("%s/workers=%d", tc.name, workers), func(b *testing.B) {
			// Каждый worker получает свой writer (отдельный Kafka соединение)
			brokers := make([]*Kafka, workers)
			for i := 0; i < workers; i++ {
				brokers[i] = newBenchBroker(b)
			}
			defer func() {
				for _, br := range brokers {
					br.Close()
				}
			}()

			payloads := buildPayloads(tc)
			totalBytes := totalSize(payloads)

			ctx := context.Background()
			b.ResetTimer()
			b.SetBytes(int64(totalBytes))

			for n := 0; n < b.N; n++ {
				var wg sync.WaitGroup
				chunkSize := (len(payloads) + workers - 1) / workers

				for w := 0; w < workers; w++ {
					wg.Add(1)
					w := w
					go func() {
						defer wg.Done()
						start := w * chunkSize
						end := start + chunkSize
						if end > len(payloads) {
							end = len(payloads)
						}
						if start >= end {
							return
						}
						chunk := payloads[start:end]
						if err := brokers[w].SendBatch(ctx, chunk); err != nil {
							b.Errorf("worker %d SendBatch: %v", w, err)
						}
					}()
				}
				wg.Wait()
			}

			b.ReportMetric(float64(len(payloads)), "packets/op")
			b.ReportMetric(float64(totalBytes)/1024/1024, "MB/op")
			b.ReportMetric(float64(workers), "workers")
		})
	}
}

// ─── Test cases ───────────────────────────────────────────────────────────────

type benchCase struct {
	name     string
	packets  int // число пакетов (частей одной таблицы)
	rowsPer  int // строк в пакете
	sizeKB   int // целевой размер пакета в КБ
}

func packetCases() []benchCase {
	return []benchCase{
		// Маленькая таблица: 5 пакетов × 500 КБ = 2.5 МБ
		{name: "small/5pkt×500KB", packets: 5, rowsPer: 200, sizeKB: 500},
		// Типичный экспорт: 10 пакетов × 1.9 МБ = 19 МБ
		{name: "typical/10pkt×1900KB", packets: 10, rowsPer: 500, sizeKB: 1900},
		// Большая таблица: 20 пакетов × 1.9 МБ = 38 МБ
		{name: "large/20pkt×1900KB", packets: 20, rowsPer: 500, sizeKB: 1900},
	}
}

func buildPayloads(tc benchCase) [][]byte {
	payloads := make([][]byte, tc.packets)
	for i := 0; i < tc.packets; i++ {
		payloads[i] = makeTDTPPacket(i+1, tc.packets, tc.rowsPer, tc.sizeKB)
	}
	return payloads
}

func totalSize(payloads [][]byte) int {
	total := 0
	for _, p := range payloads {
		total += len(p)
	}
	return total
}
