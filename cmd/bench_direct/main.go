package main

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"strconv"
	"time"

	sqlite "modernc.org/sqlite"
	_ "modernc.org/sqlite"
)

const DBFile = "benchmark_100k.db"

// ── Вариант A: через database/sql (текущий baseline) ─────────────────────
func benchViaSQL(db *sql.DB) ([]byte, int) {
	rows, _ := db.Query("SELECT * FROM Users")
	defer rows.Close()

	cols, _ := rows.Columns()
	colCount := len(cols)
	values := make([]interface{}, colCount)
	scanArgs := make([]interface{}, colCount)
	for i := range values {
		scanArgs[i] = &values[i]
	}

	buf := make([]byte, 0, 32*1024*1024)
	n := 0
	for rows.Next() {
		rows.Scan(scanArgs...)
		buf = append(buf, '<', 'R', '>')
		for i, val := range values {
			if i > 0 {
				buf = append(buf, '|')
			}
			if val == nil {
				continue
			}
			switch v := val.(type) {
			case int64:
				buf = strconv.AppendInt(buf, v, 10)
			case float64:
				buf = strconv.AppendFloat(buf, v, 'f', -1, 64)
			case string:
				buf = append(buf, v...)
			case []byte:
				buf = append(buf, v...)
			}
		}
		buf = append(buf, '<', '/', 'R', '>', '\n')
		n++
	}
	return buf, n
}

// ── Вариант B: напрямую через driver.Conn, без database/sql mutex ─────────
func benchViaDirect() ([]byte, int) {
	// Открываем через Driver напрямую — никакого connection pool, никакого mutex
	var drv sqlite.Driver
	driverConn, err := drv.Open(DBFile)
	if err != nil {
		panic(err)
	}
	defer driverConn.Close()

	// Prepare через driver.Conn
	driverStmt, err := driverConn.Prepare("SELECT * FROM Users")
	if err != nil {
		panic(err)
	}
	defer driverStmt.Close()

	// Query через driver.Stmt
	driverRows, err := driverStmt.Query(nil)
	if err != nil {
		panic(err)
	}
	defer driverRows.Close()

	cols := driverRows.Columns()
	colCount := len(cols)

	// dest — слайс driver.Value ([]interface{})
	// Next() заполняет его НАПРЯМУЮ без mutex
	dest := make([]driver.Value, colCount)

	buf := make([]byte, 0, 32*1024*1024)
	n := 0
	for driverRows.Next(dest) == nil {
		buf = append(buf, '<', 'R', '>')
		for i, val := range dest {
			if i > 0 {
				buf = append(buf, '|')
			}
			if val == nil {
				continue
			}
			switch v := val.(type) {
			case int64:
				buf = strconv.AppendInt(buf, v, 10)
			case float64:
				buf = strconv.AppendFloat(buf, v, 'f', -1, 64)
			case string:
				buf = append(buf, v...)
			case []byte:
				buf = append(buf, v...)
			case time.Time:
				buf = append(buf, v.Format("2006-01-02")...)
			}
		}
		buf = append(buf, '<', '/', 'R', '>', '\n')
		n++
	}
	return buf, n
}

func run(name string, runs int, f func() ([]byte, int)) {
	// прогрев
	f()
	times := make([]float64, runs)
	for i := 0; i < runs; i++ {
		t0 := time.Now()
		buf, rows := f()
		ms := float64(time.Since(t0).Milliseconds())
		times[i] = ms
		_ = buf
		_ = rows
	}
	minT := times[0]
	var sum float64
	for _, t := range times {
		sum += t
		if t < minT {
			minT = t
		}
	}
	mf := float64(100000*16) / (minT / 1000) / 1e6
	fmt.Printf("%-40s min=%4.0fms  avg=%4.0fms  %.2f Mfields/s\n",
		name, minT, sum/float64(runs), mf)
}

func main() {
	// database/sql pool
	db, _ := sql.Open("sqlite", DBFile)
	defer db.Close()

	// Убеждаемся что файл существует
	if _, err := os.Stat(DBFile); err != nil {
		fmt.Println("Нет файла", DBFile)
		return
	}

	const runs = 10
	fmt.Printf("modernc.org/sqlite  |  %d прогонов  |  100k × 16\n\n", runs)

	run("A. database/sql (mutex per row)", runs, func() ([]byte, int) {
		return benchViaSQL(db)
	})
	run("B. driver.Conn напрямую (NO mutex)", runs, func() ([]byte, int) {
		return benchViaDirect()
	})

	// Проверяем что результаты одинаковые
	bufSQL, nSQL := benchViaSQL(db)
	bufDirect, nDirect := benchViaDirect()
	fmt.Printf("\nПроверка: SQL=%d строк (%d bytes), Direct=%d строк (%d bytes)\n",
		nSQL, len(bufSQL), nDirect, len(bufDirect))
	if len(bufSQL) == len(bufDirect) {
		fmt.Println("✓ Размер буфера совпадает")
	} else {
		fmt.Println("! Размер отличается — проверь конвертацию типов")
	}

	fmt.Println()
	fmt.Println("Для сравнения:")
	fmt.Println("  Python fetchall() → память: 287ms  5.57 Mf/s")
	fmt.Println("  DuckDB in-memory read:       293ms  5.45 Mf/s")
}
