package etl

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// badRetrySource падает быстро в adapters.New (неизвестный тип) — без сети
// и без сна при retry_delay_seconds: 0.
func badRetrySource() SourceConfig {
	return SourceConfig{Name: "s", Type: "bogus-adapter", DSN: "x", Query: "SELECT 1"}
}

// Исчерпание попыток: ошибка обернута счетчиком pkg/retry.
func TestLoader_RetryExhausted(t *testing.T) {
	l := NewLoader([]SourceConfig{badRetrySource()}, ErrorHandlingConfig{
		OnSourceError: "fail", RetryAttempts: 3, RetryDelaySeconds: 0,
	})
	_, err := l.LoadAll(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "max retry attempts (3) exceeded") {
		t.Errorf("expected retry wrapper, got: %v", err)
	}
}

// attempts <= 1 (и ноль у Loader без SetDefaults) — одна попытка,
// старое поведение: сырая ошибка без обертки и без задержек.
func TestLoader_NoRetryOnSingleAttempt(t *testing.T) {
	for _, attempts := range []int{0, 1} {
		l := NewLoader([]SourceConfig{badRetrySource()}, ErrorHandlingConfig{
			OnSourceError: "fail", RetryAttempts: attempts, RetryDelaySeconds: 0,
		})
		_, err := l.LoadAll(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
		if strings.Contains(err.Error(), "max retry attempts") {
			t.Errorf("attempts=%d must not retry, got: %v", attempts, err)
		}
	}
}

// Успешный источник через retry-обертку проходит без ошибки и с данными.
func TestLoader_RetryWrapperSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retry.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER); INSERT INTO t VALUES (1),(2)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = db.Close()

	src := SourceConfig{Name: "s", Type: "sqlite", DSN: path, Query: "SELECT id FROM t"}
	l := NewLoader([]SourceConfig{src}, ErrorHandlingConfig{
		OnSourceError: "fail", RetryAttempts: 3, RetryDelaySeconds: 0,
	})
	results, err := l.LoadAll(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(results) != 1 || results[0].Packet == nil || results[0].Packet.Header.RecordsInPart != 2 {
		t.Fatalf("expected 2 rows, got %+v", results)
	}
}
