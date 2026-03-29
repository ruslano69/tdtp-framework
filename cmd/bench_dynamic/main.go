package main

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

const DBFile = "benchmark_100k.db"

// ── Вариант 1: оригинальный dynamic (interface{}) → файл ──────────────────
func benchDynamicToFile(db *sql.DB) (int64, int) {
	file, _ := os.Create("/tmp/bench_dynamic.xml")
	defer file.Close()

	buf := make([]byte, 0, 8*1024*1024)
	buf = append(buf, "<DATA>\n"...)

	rows, _ := db.Query("SELECT * FROM Users")
	defer rows.Close()

	cols, _ := rows.Columns()
	colCount := len(cols)
	values := make([]interface{}, colCount)
	scanArgs := make([]interface{}, colCount)
	for i := range values {
		scanArgs[i] = &values[i]
	}

	n := 0
	for rows.Next() {
		rows.Scan(scanArgs...)
		buf = append(buf, "<R>"...)
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
				buf = strconv.AppendFloat(buf, v, 'f', 2, 64)
			case string:
				buf = append(buf, v...)
			case []byte:
				buf = append(buf, v...)
			case bool:
				if v {
					buf = append(buf, '1')
				} else {
					buf = append(buf, '0')
				}
			case time.Time:
				buf = append(buf, v.Format("2006-01-02 15:04:05")...)
			default:
				buf = append(buf, fmt.Sprint(v)...)
			}
		}
		buf = append(buf, "</R>\n"...)
		n++
		if len(buf) > 4*1024*1024 {
			file.Write(buf)
			buf = buf[:0]
		}
	}
	buf = append(buf, "</DATA>\n"...)
	file.Write(buf)
	return 0, n
}

// ── Вариант 2: dynamic только в память (без записи на диск) ──────────────
func benchDynamicMemOnly(db *sql.DB) ([]byte, int) {
	buf := make([]byte, 0, 32*1024*1024)
	buf = append(buf, "<DATA>\n"...)

	rows, _ := db.Query("SELECT * FROM Users")
	defer rows.Close()

	cols, _ := rows.Columns()
	colCount := len(cols)
	values := make([]interface{}, colCount)
	scanArgs := make([]interface{}, colCount)
	for i := range values {
		scanArgs[i] = &values[i]
	}

	n := 0
	for rows.Next() {
		rows.Scan(scanArgs...)
		buf = append(buf, "<R>"...)
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
				buf = strconv.AppendFloat(buf, v, 'f', 2, 64)
			case string:
				buf = append(buf, v...)
			case []byte:
				buf = append(buf, v...)
			case bool:
				if v {
					buf = append(buf, '1')
				} else {
					buf = append(buf, '0')
				}
			case time.Time:
				buf = append(buf, v.Format("2006-01-02 15:04:05")...)
			default:
				buf = append(buf, fmt.Sprint(v)...)
			}
		}
		buf = append(buf, "</R>\n"...)
		n++
	}
	buf = append(buf, "</DATA>\n"...)
	return buf, n
}

// ── Вариант 3: только rows.Next()+Scan() без обработки (измеряем mutex) ──
func benchScanOnly(db *sql.DB) int {
	rows, _ := db.Query("SELECT * FROM Users")
	defer rows.Close()

	cols, _ := rows.Columns()
	colCount := len(cols)
	values := make([]interface{}, colCount)
	scanArgs := make([]interface{}, colCount)
	for i := range values {
		scanArgs[i] = &values[i]
	}

	n := 0
	for rows.Next() {
		rows.Scan(scanArgs...)
		n++
	}
	return n
}

// ── Вариант 4: *string scan (наш текущий путь для дат) ────────────────────
func benchStringScan(db *sql.DB) int {
	rows, _ := db.Query("SELECT * FROM Users")
	defer rows.Close()

	cols, _ := rows.Columns()
	colCount := len(cols)
	ptrs := make([]*string, colCount)
	scanArgs := make([]interface{}, colCount)
	strs := make([]string, colCount)
	for i := range ptrs {
		ptrs[i] = &strs[i]
		scanArgs[i] = ptrs[i]
	}

	n := 0
	for rows.Next() {
		rows.Scan(scanArgs...)
		n++
	}
	return n
}

func runN(name string, n int, f func() (int, int64)) {
	fmt.Printf("%-35s ", name)
	var times [10]float64
	for i := 0; i < n; i++ {
		t0 := time.Now()
		rows, _ := f()
		ms := float64(time.Since(t0).Milliseconds())
		times[i] = ms
		_ = rows
	}
	minT := times[0]
	var sum float64
	for _, t := range times[:n] {
		sum += t
		if t < minT {
			minT = t
		}
	}
	mf := float64(100000*16) / (minT / 1000) / 1e6
	fmt.Printf("min=%4.0fms  avg=%4.0fms  %.2f Mfields/s\n", minT, sum/float64(n), mf)
}

func main() {
	db, err := sql.Open("sqlite", DBFile)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer db.Close()

	fmt.Println("Прогрев...")
	benchDynamicToFile(db)
	benchDynamicMemOnly(db)
	benchScanOnly(db)
	benchStringScan(db)

	fmt.Println("\n=== 10 прогонов, метрика — МИНИМУМ ===\n")

	runN("1. interface{} scan → файл", 10, func() (int, int64) {
		_, n := benchDynamicToFile(db)
		return n, 0
	})
	runN("2. interface{} scan → память", 10, func() (int, int64) {
		buf, n := benchDynamicMemOnly(db)
		return n, int64(len(buf))
	})
	runN("3. scan only (mutex baseline)", 10, func() (int, int64) {
		return benchScanOnly(db), 0
	})
	runN("4. *string scan only (наш путь)", 10, func() (int, int64) {
		return benchStringScan(db), 0
	})

	// Справка: Python
	fmt.Println()
	fmt.Println("Для сравнения (из предыдущих замеров):")
	fmt.Println("  Python fetchall() → CSV файл: ~600ms")
	fmt.Println("  Наш ScanSQLRows (*string): ~1270ms min")
	fmt.Println("  DuckDB in-memory read: ~293ms")
}
