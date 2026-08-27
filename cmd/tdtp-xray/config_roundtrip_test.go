package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Открыть боевой пайплайн в xray и сохранить его не должно означать лишиться
// его настроек.
//
// Здесь стояла своя копия TDTPOutputConfig на три поля против тринадцати у
// pkg/etl, и потеря была двойной тишиной: yaml.Unmarshal молча пропускает
// ключи, которых нет в структуре, а сохранение так же молча пишет обратно
// только то, что осталось. YAML на выходе оставался валидным — просто другим.
//
// Проверяется весь путь целиком: YAML → состояние приложения → YAML.
// Проверять только структуру бессмысленно, потому что терялось не в ней, а в
// переходах.
func TestPipelineYAML_RoundTripKeepsEveryOutputField(t *testing.T) {
	const src = `
name: "production export"
version: "1.0"
sources:
  - name: src
    type: postgres
    dsn: "postgres://user:pass@host:5432/db"
    query: "SELECT * FROM t"
workspace:
  type: sqlite
  mode: ":memory:"
transform:
  result_table: "t"
  sql: "SELECT * FROM src"
output:
  type: tdtp
  tdtp:
    destination: out.tdtp.xml
    format: xml
    compression: true
    compress_algo: kanzi
    compress_level: 6
    compact: true
    compact_tail: true
    fixed_fields:
      - Region
      - Currency
    encryption: true
    encryption_v13: true
    fast: true
`

	var loaded TDTPConfig
	if err := yaml.Unmarshal([]byte(src), &loaded); err != nil {
		t.Fatalf("parse the source pipeline: %v", err)
	}
	if loaded.Output.TDTP == nil {
		t.Fatal("the source pipeline has no tdtp output")
	}
	// Если этот блок падает, значит структура снова потеряла поля, и остальная
	// часть теста проверяла бы уже испорченный вход.
	if !loaded.Output.TDTP.Encryption || loaded.Output.TDTP.CompressAlgo != "kanzi" {
		t.Fatalf("the config struct itself drops fields on parse: %+v", loaded.Output.TDTP)
	}

	app := &App{}
	app.loadConfigFromYAML(&loaded)

	out := app.buildOutputConfig()
	if out.TDTP == nil {
		t.Fatal("saving produced no tdtp output")
	}

	got := out.TDTP
	for _, c := range []struct {
		field string
		ok    bool
		saw   any
	}{
		{"destination", got.Destination == "out.tdtp.xml", got.Destination},
		{"format", got.Format == "xml", got.Format},
		{"compression", got.Compression, got.Compression},
		{"compress_algo", got.CompressAlgo == "kanzi", got.CompressAlgo},
		{"compress_level", got.CompressLevel == 6, got.CompressLevel},
		{"compact", got.Compact, got.Compact},
		{"compact_tail", got.CompactTail, got.CompactTail},
		{"fixed_fields", strings.Join(got.FixedFields, ",") == "Region,Currency", got.FixedFields},
		{"encryption", got.Encryption, got.Encryption},
		{"encryption_v13", got.EncryptionV13, got.EncryptionV13},
		{"fast", got.Fast, got.Fast},
	} {
		if !c.ok {
			t.Errorf("%s lost on the round trip: got %v", c.field, c.saw)
		}
	}
}

// compress и compression — два YAML-ключа для одного факта; pkg/etl складывает
// первый во второй и обнуляет. Без того же приёма здесь GUI показал бы «сжатия
// нет» у файла, где написано compress: true, а сохранение записало бы оба
// ключа сразу — из одного факта получилось бы два, и они могли бы разойтись.
func TestPipelineYAML_CompressFoldsIntoCompression(t *testing.T) {
	const src = `
name: "legacy key"
output:
  type: tdtp
  tdtp:
    destination: out.xml
    compress: true
`
	var loaded TDTPConfig
	if err := yaml.Unmarshal([]byte(src), &loaded); err != nil {
		t.Fatalf("parse: %v", err)
	}

	app := &App{}
	app.loadConfigFromYAML(&loaded)

	if app.output == nil || app.output.File == nil {
		t.Fatal("the tdtp output did not reach the GUI model")
	}
	if !app.output.File.Compression {
		t.Error("the GUI shows no compression for a pipeline written with compress: true")
	}

	out := app.buildOutputConfig()
	if !out.TDTP.Compression {
		t.Error("compression lost on save")
	}
	if out.TDTP.Compress {
		t.Error("both compress and compression were written; one fact, one key")
	}
}

// Настройки одного пайплайна не должны перетекать в другой. Это хуже потери:
// потеря хотя бы заметна, а чужое шифрование в чужом файле — нет.
func TestPipelineYAML_KeptSettingsDoNotLeakBetweenConfigs(t *testing.T) {
	const withExtras = `
name: "first"
output:
  type: tdtp
  tdtp:
    destination: a.xml
    encryption: true
    compress_algo: kanzi
`
	const plain = `
name: "second"
output:
  type: tdtp
  tdtp:
    destination: b.xml
`
	app := &App{}

	var first TDTPConfig
	if err := yaml.Unmarshal([]byte(withExtras), &first); err != nil {
		t.Fatalf("parse first: %v", err)
	}
	app.loadConfigFromYAML(&first)
	if !app.buildOutputConfig().TDTP.Encryption {
		t.Fatal("the first config lost its encryption before the check could run")
	}

	var second TDTPConfig
	if err := yaml.Unmarshal([]byte(plain), &second); err != nil {
		t.Fatalf("parse second: %v", err)
	}
	app.loadConfigFromYAML(&second)

	got := app.buildOutputConfig().TDTP
	if got.Encryption {
		t.Error("encryption from the previous pipeline leaked into this one")
	}
	if got.CompressAlgo != "" {
		t.Errorf("compress_algo from the previous pipeline leaked in as %q", got.CompressAlgo)
	}
	if got.Destination != "b.xml" {
		t.Errorf("destination = %q, want b.xml", got.Destination)
	}
}
