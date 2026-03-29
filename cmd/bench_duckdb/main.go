// bench_duckdb: сравнение скорости чтения modernc SQLite vs DuckDB in-memory.
// Build: go build -o /tmp/bench_duckdb ./cmd/bench_duckdb/
// Run:   /tmp/bench_duckdb /path/to/db.sqlite
package main

import (
	"bufio"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"strconv"
	"time"

	duckdb "github.com/marcboeker/go-duckdb"
	_ "modernc.org/sqlite"
)

const bufferSize = 4 * 1024 * 1024

func main() {
	dbPath := "/home/user/tdtp-framework/benchmark_100k.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	fmt.Println("=== modernc SQLite ===")
	rows := readSQLite(dbPath)
	fmt.Printf("loaded %d rows from SQLite\n\n", len(rows))

	fmt.Println("=== DuckDB in-memory (bulk insert via Appender) ===")
	benchDuckDB(rows)
}

// readSQLite читает все строки из SQLite в [][]string
func readSQLite(path string) [][]string {
	db, _ := sql.Open("sqlite", path)
	defer db.Close()

	t0 := time.Now()
	sqlRows, _ := db.Query("SELECT * FROM users")
	defer sqlRows.Close()

	cols, _ := sqlRows.Columns()
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}

	var result [][]string
	for sqlRows.Next() {
		sqlRows.Scan(ptrs...)
		row := make([]string, len(cols))
		for j, v := range vals {
			if v != nil {
				switch val := v.(type) {
				case int64:
					row[j] = strconv.FormatInt(val, 10)
				case float64:
					row[j] = strconv.FormatFloat(val, 'g', -1, 64)
				case string:
					row[j] = val
				case time.Time:
					row[j] = val.UTC().Format(time.RFC3339)
				default:
					row[j] = fmt.Sprintf("%v", val)
				}
			}
		}
		result = append(result, row)
	}
	fmt.Printf("sqlite read:  %v\n", time.Since(t0))
	return result
}

// benchDuckDB загружает данные в DuckDB in-memory через Appender и замеряет чтение
func benchDuckDB(data [][]string) {
	if len(data) == 0 {
		fmt.Println("no data")
		return
	}
	cols := len(data[0])

	connector, err := duckdb.NewConnector("", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connector: %v\n", err)
		return
	}
	db := sql.OpenDB(connector)
	defer db.Close()

	// Создаём таблицу (все колонки как VARCHAR — имитируем ETL workspace)
	createSQL := "CREATE TABLE users (c0 VARCHAR"
	for i := 1; i < cols; i++ {
		createSQL += fmt.Sprintf(", c%d VARCHAR", i)
	}
	createSQL += ")"
	if _, err := db.Exec(createSQL); err != nil {
		fmt.Fprintf(os.Stderr, "create: %v\n", err)
		return
	}

	// Bulk insert через Appender — один CGO-вызов на батч, не на строку
	tLoad := time.Now()
	conn, _ := connector.Connect(nil)
	appender, err := duckdb.NewAppenderFromConn(conn, "", "users")
	if err != nil {
		fmt.Fprintf(os.Stderr, "appender: %v\n", err)
		return
	}
	for _, row := range data {
		irow := make([]driver.Value, len(row))
		for i, v := range row {
			irow[i] = v
		}
		appender.AppendRow(irow...)
	}
	appender.Close()
	fmt.Printf("duckdb load:  %v (%d rows)\n", time.Since(tLoad), len(data))

	// Читаем из DuckDB
	tRead := time.Now()
	rows, err := db.Query("SELECT * FROM users")
	if err != nil {
		fmt.Fprintf(os.Stderr, "query: %v\n", err)
		return
	}
	defer rows.Close()

	scanVals := make([]any, cols)
	scanPtrs := make([]any, cols)
	for i := range scanVals {
		scanPtrs[i] = &scanVals[i]
	}

	out, _ := os.Create(os.DevNull)
	w := bufio.NewWriterSize(out, bufferSize)
	defer out.Close()

	var count int64
	for rows.Next() {
		rows.Scan(scanPtrs...)
		w.WriteString("<R>")
		for j, v := range scanVals {
			if j > 0 {
				w.WriteByte('|')
			}
			if v != nil {
				if s, ok := v.(string); ok {
					w.WriteString(s)
				} else {
					fmt.Fprintf(w, "%v", v)
				}
			}
		}
		w.WriteString("</R>\n")
		count++
	}
	w.Flush()

	elapsed := time.Since(tRead)
	fmt.Printf("duckdb read:  %v\n", elapsed)
	fmt.Printf("fields/sec:   %.2fM\n", float64(count)*float64(cols)/elapsed.Seconds()/1e6)
}
