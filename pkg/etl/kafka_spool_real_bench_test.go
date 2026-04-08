//go:build !nokafka

package etl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	kafka "github.com/segmentio/kafka-go"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ─── Инфраструктура ──────────────────────────────────────────────────────────

const (
	benchDBName   = "benchmark_100k.db"
	benchTopic    = "tdtp-bench-real"
	benchTopicSpl = "tdtp-bench-real-spool"
)

// findBenchDB ищет benchmark_100k.db от файла теста вверх по дереву.
func findBenchDB(t testing.TB) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, benchDBName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		dir = filepath.Dir(dir)
	}
	t.Skipf("%s not found", benchDBName)
	return ""
}

func kafkaAvailable(t testing.TB) {
	t.Helper()
	conn, err := kafka.DialContext(context.Background(), "tcp", "localhost:9092")
	if err != nil {
		t.Skipf("Kafka not available: %v", err)
	}
	_ = conn.Close()
}

func newWriteOnlyWriter(topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:         kafka.TCP("localhost:9092"),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
		Async:        false,
		Compression:  kafka.Snappy,
		MaxAttempts:  3,
		WriteTimeout: 30 * time.Second,
		BatchBytes:   100 * 1024 * 1024,
		BatchTimeout: 5 * time.Millisecond,
	}
}

// exportFromDB — полный цикл:
// открыть адаптер → ExportTable → получить DataPacket-ы.
// Вызывается внутри тикаемого цикла.
func exportFromDB(ctx context.Context, dbPath string) ([]*packet.DataPacket, error) {
	a := adapters.MustNew(ctx, adapters.Config{Type: "sqlite", DSN: dbPath})
	defer func() { _ = a.Close(ctx) }()
	return a.ExportTable(ctx, "Users")
}

// repartition пере-нарезает пакеты под нужный packetKB.
func repartition(pkts []*packet.DataPacket, packetKB int) ([]*packet.DataPacket, error) {
	gen := packet.NewGenerator()
	gen.SetMaxMessageSize(packetKB * 1024)
	var out []*packet.DataPacket
	for _, p := range pkts {
		p.MaterializeRows()
		rows := packet.ParseRows(p.Data.Rows, packet.NewParser())
		parts, err := gen.GenerateReference(p.Header.TableName, p.Schema, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, parts...)
	}
	return out, nil
}

// ─── Benchmarks — полный E2E ──────────────────────────────────────────────────

// BenchmarkE2E_LegacySend — ТЕКУЩИЙ путь (baseline):
//
//	DB read → ExportTable (packet.DataPacket) → ToXML → Send() по одному
//
// Это полное время "от запроса к БД до ACK от Kafka" на каждый пакет.
func BenchmarkE2E_LegacySend(b *testing.B) {
	kafkaAvailable(b)
	dbPath := findBenchDB(b)

	// Разогрев: определяем общий объём для b.SetBytes
	{
		pkts, _ := exportFromDB(context.Background(), dbPath)
		gen := packet.NewGenerator()
		total := int64(0)
		for _, p := range pkts {
			p.MaterializeRows()
			d, _ := gen.ToXML(p, true)
			total += int64(len(d))
		}
		b.Logf("Warmup: %d packets, %.1f MB XML", len(pkts), float64(total)/1024/1024)
		b.SetBytes(total)
	}

	w := newWriteOnlyWriter(benchTopic)
	defer func() { _ = w.Close() }()
	gen := packet.NewGenerator()
	ctx := context.Background()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		// 1. Читаем БД
		pkts, err := exportFromDB(ctx, dbPath)
		if err != nil {
			b.Fatalf("exportFromDB: %v", err)
		}

		// 2. Сериализуем + шлём по одному
		for i, p := range pkts {
			p.MaterializeRows()
			xmlData, err := gen.ToXML(p, true)
			if err != nil {
				b.Fatalf("ToXML: %v", err)
			}
			msg := kafka.Message{
				Key:   []byte(fmt.Sprintf("tdtp-%d-%d", n, i)),
				Value: xmlData,
				Time:  time.Now(),
			}
			if err := w.WriteMessages(ctx, msg); err != nil {
				b.Fatalf("WriteMessages: %v", err)
			}
		}
	}
	b.ReportMetric(float64(6), "packets/op")
}

// BenchmarkE2E_SendBatch — DB read → ToXML → все пакеты одним WriteMessages.
func BenchmarkE2E_SendBatch(b *testing.B) {
	kafkaAvailable(b)
	dbPath := findBenchDB(b)

	{
		pkts, _ := exportFromDB(context.Background(), dbPath)
		gen := packet.NewGenerator()
		total := int64(0)
		for _, p := range pkts {
			p.MaterializeRows()
			d, _ := gen.ToXML(p, true)
			total += int64(len(d))
		}
		b.Logf("Warmup: %d packets, %.1f MB XML", len(pkts), float64(total)/1024/1024)
		b.SetBytes(total)
	}

	w := newWriteOnlyWriter(benchTopic)
	defer func() { _ = w.Close() }()
	gen := packet.NewGenerator()
	ctx := context.Background()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		pkts, err := exportFromDB(ctx, dbPath)
		if err != nil {
			b.Fatalf("exportFromDB: %v", err)
		}

		msgs := make([]kafka.Message, 0, len(pkts))
		total := int64(0)
		for i, p := range pkts {
			p.MaterializeRows()
			xmlData, err := gen.ToXML(p, true)
			if err != nil {
				b.Fatalf("ToXML: %v", err)
			}
			msgs = append(msgs, kafka.Message{
				Key:   []byte(fmt.Sprintf("tdtp-%d-%d", n, i)),
				Value: xmlData,
				Time:  time.Now(),
			})
			total += int64(len(xmlData))
		}

		if err := w.WriteMessages(ctx, msgs...); err != nil {
			b.Fatalf("WriteMessages: %v", err)
		}
	}
	b.ReportMetric(float64(6), "packets/op")
}

// BenchmarkE2E_SpoolPipeline_zstd — полный E2E через spool:
//
//	DB read → repartition(750KB) → XML+zstd → spool file → SendBatch → delete
func BenchmarkE2E_SpoolPipeline_zstd(b *testing.B) {
	kafkaAvailable(b)
	dbPath := findBenchDB(b)

	cfg := &KafkaOutputConfig{
		Brokers:       []string{"localhost:9092"},
		Topic:         benchTopicSpl,
		PacketKB:      750,
		BatchSend:     10,
		CompressAlgo:  "zstd",
		CompressLevel: 3,
	}

	{
		pkts, _ := exportFromDB(context.Background(), dbPath)
		parts, _ := repartition(pkts, cfg.PacketKB)
		b.Logf("Warmup: %d packets @ %dKB target", len(parts), cfg.PacketKB)
		b.SetBytes(int64(len(parts)) * int64(cfg.PacketKB) * 1024)
	}

	ctx := context.Background()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		// 1. Читаем БД
		pkts, err := exportFromDB(ctx, dbPath)
		if err != nil {
			b.Fatalf("exportFromDB: %v", err)
		}

		// 2. Пере-нарезаем
		parts, err := repartition(pkts, cfg.PacketKB)
		if err != nil {
			b.Fatalf("repartition: %v", err)
		}

		// 3. Spool pipeline: XML → zstd → файл → Kafka
		jobCfg := *cfg
		exp, err := NewKafkaSpoolExporter(&jobCfg, fmt.Sprintf("e2e-%04d", n))
		if err != nil {
			b.Fatalf("NewKafkaSpoolExporter: %v", err)
		}
		if err := exp.ExportPackets(ctx, parts); err != nil {
			_ = exp.Cleanup()
			exp.Close()
			b.Fatalf("ExportPackets: %v", err)
		}
		_ = exp.Cleanup()
		exp.Close()
	}

	b.ReportMetric(float64(27), "packets/op")
}

// BenchmarkE2E_SpoolPipeline_kanzi — то же, но kanzi level 6.
func BenchmarkE2E_SpoolPipeline_kanzi(b *testing.B) {
	kafkaAvailable(b)
	dbPath := findBenchDB(b)

	cfg := &KafkaOutputConfig{
		Brokers:       []string{"localhost:9092"},
		Topic:         benchTopicSpl,
		PacketKB:      750,
		BatchSend:     10,
		CompressAlgo:  "kanzi",
		CompressLevel: 6,
	}

	{
		pkts, _ := exportFromDB(context.Background(), dbPath)
		parts, _ := repartition(pkts, cfg.PacketKB)
		b.Logf("Warmup: %d packets @ %dKB target, kanzi-6", len(parts), cfg.PacketKB)
		b.SetBytes(int64(len(parts)) * int64(cfg.PacketKB) * 1024)
	}

	ctx := context.Background()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		pkts, err := exportFromDB(ctx, dbPath)
		if err != nil {
			b.Fatalf("exportFromDB: %v", err)
		}
		parts, err := repartition(pkts, cfg.PacketKB)
		if err != nil {
			b.Fatalf("repartition: %v", err)
		}
		jobCfg := *cfg
		exp, err := NewKafkaSpoolExporter(&jobCfg, fmt.Sprintf("e2e-k-%04d", n))
		if err != nil {
			b.Skipf("kanzi not available: %v", err)
		}
		if err := exp.ExportPackets(ctx, parts); err != nil {
			_ = exp.Cleanup()
			exp.Close()
			b.Fatalf("ExportPackets: %v", err)
		}
		_ = exp.Cleanup()
		exp.Close()
	}

	b.ReportMetric(float64(27), "packets/op")
}

// BenchmarkE2E_InMemory_100MB — то же что SpoolPipeline_zstd, но без диска.
// Сжатые данные накапливаются в памяти ≤ 100 MB, backpressure через семафор.
func BenchmarkE2E_InMemory_100MB(b *testing.B) {
	kafkaAvailable(b)
	dbPath := findBenchDB(b)

	cfg := &KafkaOutputConfig{
		Brokers:       []string{"localhost:9092"},
		Topic:         benchTopicSpl,
		PacketKB:      750,
		BatchSend:     10,
		CompressAlgo:  "zstd",
		CompressLevel: 3,
		MemLimitMB:    100,
	}

	{
		pkts, _ := exportFromDB(context.Background(), dbPath)
		parts, _ := repartition(pkts, cfg.PacketKB)
		b.Logf("Warmup: %d packets @ %dKB target, mem_limit=%dMB", len(parts), cfg.PacketKB, cfg.MemLimitMB)
		b.SetBytes(int64(len(parts)) * int64(cfg.PacketKB) * 1024)
	}

	ctx := context.Background()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		pkts, err := exportFromDB(ctx, dbPath)
		if err != nil {
			b.Fatalf("exportFromDB: %v", err)
		}
		parts, err := repartition(pkts, cfg.PacketKB)
		if err != nil {
			b.Fatalf("repartition: %v", err)
		}
		jobCfg := *cfg
		exp, err := NewKafkaSpoolExporter(&jobCfg, fmt.Sprintf("e2e-m-%04d", n))
		if err != nil {
			b.Fatalf("NewKafkaSpoolExporter: %v", err)
		}
		if err := exp.ExportPackets(ctx, parts); err != nil {
			exp.Close()
			b.Fatalf("ExportPackets: %v", err)
		}
		exp.Close()
	}

	b.ReportMetric(float64(27), "packets/op")
}
