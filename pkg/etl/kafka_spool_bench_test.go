//go:build !nokafka

package etl

import (
	"context"
	"fmt"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// makeBenchPackets генерирует N DataPacket с синтетическими HR-данными.
// sizeKB — целевой размер одного пакета в КБ.
func makeBenchPackets(n, rowsPer, sizeKB int) []*packet.DataPacket {
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "ID", Type: "INTEGER", Key: true},
			{Name: "FullName", Type: "TEXT"},
			{Name: "Department", Type: "TEXT"},
			{Name: "Salary", Type: "DECIMAL", Precision: 12, Scale: 2},
			{Name: "HireDate", Type: "DATE"},
			{Name: "Notes", Type: "TEXT"},
		},
	}

	depts := []string{"Engineering", "HR", "Finance", "Legal", "Operations"}
	notes := []string{
		"Annual review completed; promotion approved for Q1 2026",
		"Remote work contract signed; equipment shipped 2025-11-03",
		"Pending background check; start date deferred to next quarter",
		"Medical leave 2025-10-01–2025-11-15; return confirmed",
		"Project lead ERP migration; access to prod granted 2026-01-01",
	}

	noteLen := sizeKB*1024/rowsPer - 120
	if noteLen < 20 {
		noteLen = 20
	}

	pkts := make([]*packet.DataPacket, n)
	for i := 0; i < n; i++ {
		rows := make([][]string, rowsPer)
		for j := 0; j < rowsPer; j++ {
			id := i*100000 + j
			note := fmt.Sprintf("%s | id=%07d | %0*d",
				notes[j%len(notes)], id, noteLen, id*7+j*13)
			rows[j] = []string{
				fmt.Sprintf("%d", id),
				fmt.Sprintf("Employee%07d", id),
				depts[j%len(depts)],
				fmt.Sprintf("%.2f", 45000.0+float64(id%80000)),
				fmt.Sprintf("202%d-0%d-15", j%5+0, j%9+1),
				note,
			}
		}
		pkt := packet.NewDataPacket(packet.TypeReference, "employees")
		pkt.Schema = schema
		pkt.Data = packet.RowsToData(rows)
		pkt.Header.MessageID = fmt.Sprintf("BENCH-%06d", i+1)
		pkts[i] = pkt
	}
	return pkts
}

// BenchmarkKafkaSpoolExport — новый pipeline:
// DataPacket → XML → zstd → spool file → SendBatch → delete
func BenchmarkKafkaSpoolExport(b *testing.B) {
	cases := []struct {
		name    string
		packets int
		rowsPer int
		sizeKB  int
	}{
		{"small/5pkt×500KB", 5, 200, 500},
		{"typical/10pkt×750KB", 10, 300, 750},
		{"large/20pkt×750KB", 20, 300, 750},
	}

	for _, tc := range cases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			cfg := &KafkaOutputConfig{
				Brokers:       []string{"localhost:9092"},
				Topic:         "tdtp-bench-spool",
				PacketKB:      tc.sizeKB,
				BatchSend:     10,
				CompressAlgo:  "zstd",
				CompressLevel: 3,
			}

			// Проверяем доступность Kafka
			exp, err := NewKafkaSpoolExporter(cfg, "bench-preflight")
			if err != nil {
				b.Skipf("Kafka unavailable: %v", err)
			}
			_ = exp.Cleanup()
			exp.Close()

			packets := makeBenchPackets(tc.packets, tc.rowsPer, tc.sizeKB)

			// Считаем примерный общий объём для b.SetBytes
			totalBytes := int64(tc.packets * tc.sizeKB * 1024)
			b.SetBytes(totalBytes)
			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				// jobID уникален на итерацию — spool-директории не конфликтуют
				jobCfg := *cfg
				exp, err := NewKafkaSpoolExporter(&jobCfg, fmt.Sprintf("bench-%04d", n))
				if err != nil {
					b.Fatalf("NewKafkaSpoolExporter: %v", err)
				}
				if err := exp.ExportPackets(context.Background(), packets); err != nil {
					_ = exp.Cleanup()
					exp.Close()
					b.Fatalf("ExportPackets: %v", err)
				}
				_ = exp.Cleanup()
				exp.Close()
			}

			b.ReportMetric(float64(tc.packets), "packets/op")
		})
	}
}
