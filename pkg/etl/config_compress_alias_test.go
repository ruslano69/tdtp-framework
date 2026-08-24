package etl

import (
	"os"
	"path/filepath"
	"testing"
)

// `compress:` и `compression:` — два ключа для одного факта. После загрузки в
// коде обязано существовать одно значение, иначе каждый новый потребитель может
// прочитать не то поле — что и произошло с tdtp-xray.
func TestSetDefaults_CollapsesCompressAlias(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want bool
	}{
		{"только compress", "compress: true", true},
		{"только compression", "compression: true", true},
		{"оба", "compress: true\n    compression: true", true},
		{"ни одного", "format: xml", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `name: "t"
sources:
  - name: s
    type: sqlite
    dsn: x.db
    query: "SELECT 1"
workspace:
  type: sqlite
  mode: ":memory:"
transform:
  result_table: "r"
  sql: "SELECT 1"
output:
  type: tdtp
  tdtp:
    destination: out.xml
    ` + tc.yaml + "\n"

			path := filepath.Join(t.TempDir(), "p.yaml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if cfg.Output.TDTP == nil {
				t.Fatal("output.tdtp отсутствует")
			}
			if got := cfg.Output.TDTP.Compression; got != tc.want {
				t.Errorf("Compression=%v, ожидалось %v", got, tc.want)
			}
			// Алиас обязан быть погашен: иначе он остаётся вторым источником правды.
			if cfg.Output.TDTP.Compress {
				t.Error("Compress не обнулён — снова два представления одного факта")
			}
		})
	}
}
