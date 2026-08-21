package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"
)

// Где на самом деле уходит время при чтении строк, и что из этого можно
// забрать сменой API, а что — нет.
//
// Вопрос возникает регулярно: не ускорит ли обход database/sql в пользу
// driver.Rows? Ответ ниже — нет, разницы не видно, а вся стоимость сидит
// внутри драйвера, в разборе дат по объявленному типу колонки.

const driverBenchRows = 50000

// buildDriverBenchDB собирает таблицу с тремя колонками дат и тремя без.
func buildDriverBenchDB(b *testing.B) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "cost.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(`CREATE TABLE bench (
		id INTEGER PRIMARY KEY, name TEXT, amount REAL,
		created DATETIME, updated TIMESTAMP, birth DATE)`); err != nil {
		b.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	stmt, err := tx.Prepare("INSERT INTO bench VALUES (?,?,?,?,?,?)")
	if err != nil {
		b.Fatal(err)
	}
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < driverBenchRows; i++ {
		t := base.Add(time.Duration(i)*37*time.Second + time.Duration(i*7919)*time.Microsecond)
		if _, err := stmt.Exec(i, fmt.Sprintf("name_%d", i), float64(i)*1.5,
			t.Format("2006-01-02 15:04:05.000"),
			t.Format("2006-01-02 15:04:05"),
			base.AddDate(0, 0, i%9000).Format("2006-01-02")); err != nil {
			b.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		b.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	return path
}

func readViaSQL(b *testing.B, path, query string) {
	b.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := db.Query(query)
		if err != nil {
			b.Fatal(err)
		}
		cols, _ := rows.Columns()
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for j := range vals {
			ptrs[j] = &vals[j]
		}
		n := 0
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				b.Fatal(err)
			}
			n++
		}
		if err := rows.Err(); err != nil {
			b.Fatal(err)
		}
		_ = rows.Close()
		if n != driverBenchRows {
			b.Fatalf("got %d rows", n)
		}
	}
}

const allColumns = "SELECT id, name, amount, created, updated, birth FROM bench"

// BenchmarkRead_SQLRows — текущий путь: database/sql + Scan(&any...).
func BenchmarkRead_SQLRows(b *testing.B) {
	readViaSQL(b, buildDriverBenchDB(b), allColumns)
}

// BenchmarkRead_DriverRows — тот же запрос напрямую через driver.Rows, минуя
// database/sql целиком.
//
// Измеряется одинаково с BenchmarkRead_SQLRows, вплоть до числа аллокаций.
// convertAssign при скане в *any попадает в ветку присваивания без всякой
// конвертации, а больше database/sql на строку ничего заметного не делает.
// Обходить его смысла нет.
func BenchmarkRead_DriverRows(b *testing.B) {
	path := buildDriverBenchDB(b)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		b.Fatal(err)
	}
	d := db.Driver()
	_ = db.Close()

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := d.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		qc, ok := conn.(driver.QueryerContext)
		if !ok {
			b.Fatal("driver does not implement QueryerContext")
		}
		rows, err := qc.QueryContext(ctx, allColumns, nil)
		if err != nil {
			b.Fatal(err)
		}
		dest := make([]driver.Value, len(rows.Columns()))
		n := 0
		for {
			err := rows.Next(dest)
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatal(err)
			}
			n++
		}
		_ = rows.Close()
		_ = conn.Close()
		if n != driverBenchRows {
			b.Fatalf("got %d rows", n)
		}
	}
}

// BenchmarkRead_DatesAsText — те же колонки, но CAST'ом в TEXT.
//
// Драйвер решает, разбирать ли ячейку во время, по ОБЪЯВЛЕННОМУ типу колонки;
// CAST меняет его на TEXT, и разбор не запускается вовсе. Разница с
// BenchmarkRead_SQLRows и есть цена этого разбора — она большая, куда больше
// всего, что стоит форматирование на нашей стороне.
func BenchmarkRead_DatesAsText(b *testing.B) {
	readViaSQL(b, buildDriverBenchDB(b), `SELECT id, name, amount,
		CAST(created AS TEXT), CAST(updated AS TEXT), CAST(birth AS TEXT) FROM bench`)
}

// BenchmarkRead_NoDates — те же строки без колонок дат.
func BenchmarkRead_NoDates(b *testing.B) {
	readViaSQL(b, buildDriverBenchDB(b), "SELECT id, name, amount FROM bench")
}

// BenchmarkRead_IDOnly — нижняя граница обхода строк этим драйвером.
func BenchmarkRead_IDOnly(b *testing.B) {
	readViaSQL(b, buildDriverBenchDB(b), "SELECT id FROM bench")
}
