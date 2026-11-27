package etl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
	"github.com/ruslano69/tdtp-framework/pkg/xlsx"
)

const secretName = "Ivanenko"

func xlsxPipeline(t *testing.T) (config *PipelineConfig, destination string) {
	t.Helper()
	dir := t.TempDir()
	csv := filepath.Join(dir, "in.csv")
	if err := os.WriteFile(csv, []byte("id,surname\n1,"+secretName+"\n2,Petrenko\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination = filepath.Join(dir, "reports", "out.xlsx")
	config = &PipelineConfig{
		Name:      "xlsx",
		Sources:   []SourceConfig{{Name: "people", Type: "csv", DSN: csv, CSV: &CSVSourceConfig{Columns: []string{"id", "surname"}}}},
		Workspace: WorkspaceConfig{Type: "sqlite", Mode: ":memory:"},
		Transform: TransformConfig{SQL: "SELECT id, surname FROM people ORDER BY id", ResultTable: "result"},
		Output:    OutputConfig{Type: "xlsx", XLSX: &XLSXOutputConfig{Destination: destination, Sheet: "People"}},
	}
	config.SetDefaults()
	return config, destination
}

// runPipeline повторяет путь CLI: сначала Processor.Validate, потом Execute.
// Именно Validate отклонял xlsx, а сам экспорт в коде был.
func runPipeline(t *testing.T, config *PipelineConfig) {
	t.Helper()
	p := NewProcessor(config)
	if err := p.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := p.Execute(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func readRows(t *testing.T, path string) []string {
	t.Helper()
	pkt, err := xlsx.FromXLSX(path, "")
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]string, len(pkt.Data.Rows))
	for i, row := range pkt.Data.Rows {
		rows[i] = row.Value
	}
	return rows
}

func TestPipelineXLSXOutput(t *testing.T) {
	config, destination := xlsxPipeline(t)
	runPipeline(t, config)

	rows := readRows(t, destination)
	if len(rows) != 2 || !strings.Contains(rows[0], secretName) {
		t.Fatalf("unexpected rows: %q", rows)
	}
}

func TestPipelineXLSXOutput_IsMasked(t *testing.T) {
	config, destination := xlsxPipeline(t)
	config.Processors.PreExport = []processors.Config{
		{Type: "field_masker", Params: map[string]any{"fields": map[string]any{"surname": "stars"}}},
	}
	runPipeline(t, config)

	rows := readRows(t, destination)
	if len(rows) != 2 {
		t.Fatalf("unexpected rows: %q", rows)
	}
	for _, row := range rows {
		if strings.Contains(row, secretName) || strings.Contains(row, "Petrenko") || !strings.Contains(row, "*") {
			t.Fatalf("surname is not masked in the XLSX: %q", row)
		}
	}
}

func TestExporterValidateConfig_XLSX(t *testing.T) {
	for name, tc := range map[string]struct {
		xlsx *XLSXOutputConfig
		ok   bool
	}{
		"complete":       {&XLSXOutputConfig{Destination: "a.xlsx"}, true},
		"no section":     {nil, false},
		"no destination": {&XLSXOutputConfig{}, false},
	} {
		err := NewExporter(OutputConfig{Type: "xlsx", XLSX: tc.xlsx}).ValidateConfig()
		if (err == nil) != tc.ok {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

// onePacket — пакет из одной строки с одним текстовым полем.
func onePacket() *packet.DataPacket {
	pkt := packet.NewDataPacket(packet.TypeReference, "r")
	pkt.Schema = packet.Schema{Fields: []packet.Field{{Name: "v", Type: "TEXT"}}}
	pkt.Data = packet.RowsToData([][]string{{"value"}})
	pkt.Header.RecordsInPart = 1
	return pkt
}

func unreachableOutputs(broken string) map[string]OutputConfig {
	return map[string]OutputConfig{
		"rabbitmq": {Type: "rabbitmq", RabbitMQ: &RabbitMQOutputConfig{Host: "127.0.0.1", Port: 1, Queue: "q"}},
		"kafka":    {Type: "kafka", Kafka: &KafkaOutputConfig{Brokers: []string{"127.0.0.1:1"}, Topic: "t"}},
		// Каталог внутри обычного файла создать нельзя.
		"xlsx": {Type: "xlsx", XLSX: &XLSXOutputConfig{Destination: broken + "/out.xlsx"}},
		"tdtp": {Type: "tdtp", TDTP: &TDTPOutputConfig{Format: "xml", Destination: broken + "/out.tdtp.xml"}},
	}
}

func brokenDir(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Цепочка отрабатывает до обращения к каналу: без fallback доставить некуда, но
// пакет к этому моменту уже обработан. Без этой проверки пропуск цепочки в
// самой отправке не виден — брокеру в тесте взяться неоткуда.
func TestExport_PreExportRunsBeforeEveryChannel(t *testing.T) {
	for name, output := range unreachableOutputs(brokenDir(t)) {
		t.Run(name, func(t *testing.T) {
			pkt := onePacket()
			exporter := NewExporter(output).WithPreExportChain(processors.NewChain(markOnce{}))
			if _, err := exporter.Export(context.Background(), pkt); err == nil {
				t.Fatal("the channel was expected to fail")
			}
			if got := pkt.GetRows(); len(got) != 1 || got[0][0] != "value!" {
				t.Fatalf("the channel got unprocessed rows: %q", got)
			}
		})
	}
}

// markOnce дописывает метку: по числу меток видно, сколько раз применялась
// цепочка pre-export.
type markOnce struct{}

func (markOnce) Name() string { return "mark" }

func (markOnce) Process(_ context.Context, data [][]string, _ packet.Schema) ([][]string, error) {
	out := make([][]string, len(data))
	for i, row := range data {
		out[i] = []string{row[0] + "!"}
	}
	return out, nil
}

// Цепочка применяется один раз и при переключении на fallback: повтор после
// primary замаскировал бы уже замаскированное.
func TestExport_PreExportAppliedExactlyOnce(t *testing.T) {
	for name, primary := range unreachableOutputs(brokenDir(t)) {
		t.Run(name, func(t *testing.T) {
			fallback := filepath.Join(t.TempDir(), "fallback.tdtp.xml")
			primary.Fallback = &OutputConfig{Type: "tdtp", TDTP: &TDTPOutputConfig{Format: "xml", Destination: fallback}}

			pkt := onePacket()

			exporter := NewExporter(primary).WithPreExportChain(processors.NewChain(markOnce{}))
			result, err := exporter.Export(context.Background(), pkt)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.OutputType, "fallback") {
				t.Fatalf("primary was expected to fail, delivered as %q", result.OutputType)
			}
			delivered, err := packet.NewParser().ParseFile(fallback)
			if err != nil {
				t.Fatal(err)
			}
			if got := delivered.GetRows(); len(got) != 1 || got[0][0] != "value!" {
				t.Fatalf("want the chain applied exactly once, got %q", got)
			}
		})
	}
}
