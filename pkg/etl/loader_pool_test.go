package etl

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// Два источника на одном SQLite-файле — обычная конфигурация: одна база,
// несколько таблиц, несколько записей в sources.
//
// Раньше она не работала. LoadAll поднимает горутину на источник, каждая
// открывала своё соединение, каждое открытие переводило файл в WAL, а перевод
// журнала берёт исключительную блокировку — второй источник получал
// "database is locked (261)" (SQLITE_BUSY_RECOVERY) на проверке соединения.
// Три прогона из трёх, а на трёх источниках иногда проходило, что хуже
// стабильного отказа.
//
// Чинится с двух сторон, и обе нужны: источники с одинаковыми типом, DSN и
// настройками делят одно соединение (эта проверка), а busy_timeout задаётся в
// строке подключения, чтобы конфликт с ДРУГИМ процессом ждал, а не падал —
// прагмой его поставить вовремя нельзя, она выполняется после ping.
func TestLoader_TwoSourcesOneFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shared.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Файл ЗАРАНЕЕ переводится в WAL и остаётся с непустым журналом — иначе
	// отказ не воспроизводится вовсе. Код 261 это SQLITE_BUSY_RECOVERY: он
	// возникает, когда открывающееся соединение застаёт журнал, требующий
	// восстановления, а не на пустом свежесозданном файле. Первая версия этого
	// теста именно поэтому проходила и на сломанном коде.
	//
	// Даже так проверка ловит поломку НЕ ВСЕГДА: это гонка, и на сломанном коде
	// она падала примерно в двух прогонах из трёх. Детерминированного
	// воспроизведения тут не построить — исход зависит от того, чьё открытие
	// попадёт в чужой перевод журнала. Держать такую проверку всё равно стоит:
	// она не даёт ложных срабатываний на исправленном коде (пул убирает саму
	// возможность конфликта), а поломку заметит на второй-третий прогон CI.
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = WAL`); err != nil {
		t.Fatalf("wal: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE departments (DeptID INTEGER PRIMARY KEY, DeptName TEXT);
		CREATE TABLE employees   (EmpID INTEGER PRIMARY KEY, FullName TEXT, DeptID INTEGER);
		INSERT INTO departments VALUES (1,'Engineering'), (2,'Logistics');
		INSERT INTO employees   VALUES (1,'Ann',1), (2,'Bob',2), (3,'Cid',1);
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = db.Close()

	sources := []SourceConfig{
		{Name: "departments", Type: "sqlite", DSN: path, Query: "SELECT DeptID, DeptName FROM departments"},
		{Name: "employees", Type: "sqlite", DSN: path, Query: "SELECT EmpID, FullName, DeptID FROM employees"},
	}

	l := NewLoader(sources, ErrorHandlingConfig{OnSourceError: "fail"})
	results, err := l.LoadAll(ctx)
	if err != nil {
		t.Fatalf("two sources on one file must load: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}

	rows := map[string]int{}
	for _, r := range results {
		if r.Error != nil {
			t.Errorf("source %q: %v", r.SourceName, r.Error)
			continue
		}
		rows[r.SourceName] = len(r.Packet.GetRows())
	}
	if rows["departments"] != 2 {
		t.Errorf("departments = %d rows, want 2", rows["departments"])
	}
	if rows["employees"] != 3 {
		t.Errorf("employees = %d rows, want 3", rows["employees"])
	}
}

// Ключ пула включает настройки, а не только DSN, и это не перестраховка.
//
// Адаптер несёт изменяемое состояние: SetSkipSpecialValues ставится на источник
// и обратно не сбрасывается. Разделяй соединение по одному DSN — и источник с
// fast: true молча включил бы пропуск маркеров соседнему источнику из того же
// файла. Это тихая порча данных вместо экономии соединения, то есть размен в
// худшую сторону.
func TestLoader_FastFlagDoesNotLeakBetweenSources(t *testing.T) {
	a := SourceConfig{Name: "a", Type: "sqlite", DSN: "/tmp/x.db"}
	b := SourceConfig{Name: "b", Type: "sqlite", DSN: "/tmp/x.db"}

	if adapterKey(a, false) == adapterKey(b, true) {
		t.Error("sources with different --fast settings share a pool key; " +
			"one would silently change the other's marker handling")
	}
	if adapterKey(a, false) != adapterKey(b, false) {
		t.Error("identical sources must share one connection — that is the point of the pool")
	}

	// NoDateSentinels меняет разбор дат, значит тоже принадлежит ключу.
	c := SourceConfig{Name: "c", Type: "sqlite", DSN: "/tmp/x.db", NoDateSentinels: []string{"1753-01-01"}}
	if adapterKey(a, false) == adapterKey(c, false) {
		t.Error("NoDateSentinels must be part of the pool key")
	}
}

// busy_timeout обязан быть в строке подключения: прагмой он ставится после
// PingContext, а конфликт случается на самом ping. По умолчанию он ноль, то
// есть занятый файл отказывает немедленно, не подождав.
func TestLoader_SharedFileHasBusyTimeout(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "t.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE t (ID INTEGER); INSERT INTO t VALUES (1);`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = db.Close()

	l := NewLoader([]SourceConfig{
		{Name: "t", Type: "sqlite", DSN: path, Query: "SELECT ID FROM t"},
	}, ErrorHandlingConfig{OnSourceError: "fail"})

	if _, err := l.LoadAll(ctx); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Читаем настройку тем же способом, каким её видит соединение адаптера.
	check, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = check.Close() }()
	var ms int
	if err := check.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&ms); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if ms == 0 {
		t.Error("busy_timeout is 0: a busy file fails immediately instead of waiting")
	}
}
