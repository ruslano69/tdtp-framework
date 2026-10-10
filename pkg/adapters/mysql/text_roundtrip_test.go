package mysql

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// A text field's length decides its MySQL column. VARCHAR only up to the
// utf8mb4 limit; MySQL's own TEXT/MEDIUMTEXT lengths come back as those
// types; any other length past VARCHAR gets a type that holds n four-byte
// characters.
func TestTDTPToMySQL_TextLengths(t *testing.T) {
	for _, tc := range []struct {
		typ    string
		length int
		want   string
	}{
		{"TEXT", 0, "TEXT"},
		{"TEXT", 100, "VARCHAR(100)"},
		{"TEXT", 255, "VARCHAR(255)"}, // TINYTEXT exports as TEXT/255
		{"TEXT", 16383, "VARCHAR(16383)"},
		{"TEXT", 16384, "MEDIUMTEXT"},
		{"TEXT", 65535, "TEXT"}, // MySQL's own TEXT — was VARCHAR(65535), refused
		{"TEXT", 20000, "MEDIUMTEXT"},
		{"TEXT", 16777215, "MEDIUMTEXT"}, // MySQL's own MEDIUMTEXT
		{"TEXT", 2147483647, "LONGTEXT"}, // MySQL's own LONGTEXT
		{"VARCHAR", 0, "VARCHAR(255)"},
		{"VARCHAR", 500, "VARCHAR(500)"},
		{"VARCHAR", 20000, "MEDIUMTEXT"},
		{"STRING", 0, "VARCHAR(255)"},
		{"STRING", 70000, "MEDIUMTEXT"},
		{"CHAR", 0, "CHAR(1)"},
		{"CHAR", 255, "CHAR(255)"},
		{"CHAR", 300, "VARCHAR(300)"},
	} {
		got := TDTPToMySQL(packet.Field{Name: "c", Type: tc.typ, Length: tc.length})
		if got != tc.want {
			t.Errorf("%s(%d) → %s, want %s", tc.typ, tc.length, got, tc.want)
		}
	}
}

// MySQL→MySQL with every text type: export, import into a new table, and
// compare column types and values. The import used to fail on the first TEXT
// column: "Column length too big for column … (max = 16383)".
func TestMySQLTextRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("mysql", testDSN())
	if err != nil {
		t.Skipf("MySQL not available: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("MySQL not available: %v", err)
	}

	const src, dst = "tdtp_text_src", "tdtp_text_dst"
	long := strings.Repeat("Ёлка-🌲 ", 3000) // multi-byte, ~42 KB: needs TEXT, not VARCHAR
	for _, q := range []string{
		"DROP TABLE IF EXISTS " + src, "DROP TABLE IF EXISTS " + dst,
		"CREATE TABLE " + src + ` (id INT PRIMARY KEY, tiny TINYTEXT, txt TEXT, med MEDIUMTEXT,
			lng LONGTEXT, vc VARCHAR(100), ch CHAR(10)) DEFAULT CHARSET=utf8mb4`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + src)
		_, _ = db.Exec("DROP TABLE IF EXISTS " + dst)
	})
	if _, err := db.ExecContext(ctx, "INSERT INTO "+src+" VALUES (1, 'тихо', ?, ?, ?, 'варчар', 'чар'), (2, NULL, '', NULL, '', NULL, NULL)",
		long, long, long); err != nil {
		t.Fatal(err)
	}

	a, err := adapters.New(ctx, adapters.Config{Type: "mysql", DSN: testDSN()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(ctx)
	pkts, err := a.ExportTable(ctx, src)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	for _, p := range pkts {
		p.Header.TableName = dst
		if err := a.ImportPacket(ctx, p, adapters.StrategyReplace); err != nil {
			t.Fatalf("import of MySQL's own export: %v", err)
		}
	}

	colType := func(table, col string) string {
		var typ string
		if err := db.QueryRowContext(ctx, `SELECT COLUMN_TYPE FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, col).Scan(&typ); err != nil {
			t.Fatalf("type of %s.%s: %v", table, col, err)
		}
		return typ
	}
	for _, c := range []struct{ col, want string }{
		{"txt", "text"}, {"med", "mediumtext"}, {"lng", "longtext"}, {"vc", "varchar(100)"},
	} {
		if got := colType(dst, c.col); got != c.want {
			t.Errorf("%s came back as %s, want %s", c.col, got, c.want)
		}
	}

	read := func(table string) [][]sql.NullString {
		rows, err := db.QueryContext(ctx, "SELECT tiny, txt, med, lng, vc, ch FROM "+table+" ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out [][]sql.NullString
		for rows.Next() {
			r := make([]sql.NullString, 6)
			if err := rows.Scan(&r[0], &r[1], &r[2], &r[3], &r[4], &r[5]); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		return out
	}
	want, got := read(src), read(dst)
	if len(got) != len(want) {
		t.Fatalf("%d rows back, want %d", len(got), len(want))
	}
	for i := range want {
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("row %d col %d: got %.40q (valid=%v), want %.40q (valid=%v)",
					i+1, j, got[i][j].String, got[i][j].Valid, want[i][j].String, want[i][j].Valid)
			}
		}
	}
}
