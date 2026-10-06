package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/sijms/go-ora/v2"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
)

const AdapterType = "oracle"

type Adapter struct {
	db           *sql.DB
	owner        string
	config       adapters.Config
	converter    *base.UniversalTypeConverter
	exportHelper *base.ExportHelper
}

var _ adapters.Adapter = (*Adapter)(nil)

func init() {
	adapters.Register(AdapterType, func() adapters.Adapter { return &Adapter{} })
}

func (a *Adapter) Connect(ctx context.Context, cfg adapters.Config) error {
	if cfg.DSN == "" {
		return fmt.Errorf("Oracle DSN is required")
	}
	db, err := sql.Open("oracle", cfg.DSN)
	if err != nil {
		return fmt.Errorf("open Oracle connection: %w", err)
	}
	if cfg.MaxConns > 0 {
		db.SetMaxOpenConns(cfg.MaxConns)
	}
	if cfg.MinConns > 0 {
		db.SetMaxIdleConns(cfg.MinConns)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("connect to Oracle: %w", err)
	}
	var owner string
	if err := db.QueryRowContext(ctx, "SELECT USER FROM DUAL").Scan(&owner); err != nil {
		_ = db.Close()
		return fmt.Errorf("discover Oracle schema: %w", err)
	}
	if cfg.Schema != "" {
		owner, err = identifier(cfg.Schema)
		if err != nil {
			_ = db.Close()
			return err
		}
	}
	a.db, a.owner, a.config = db, owner, cfg
	a.converter = base.NewUniversalTypeConverter()
	if len(cfg.NoDateSentinels) > 0 {
		a.converter.SetNoDateSentinels(cfg.NoDateSentinels)
	}
	a.exportHelper = base.NewExportHelper(a, a, a.converter, sqlDialect{adapter: a})
	return nil
}

func (a *Adapter) Close(context.Context) error {
	if a.db == nil {
		return nil
	}
	return a.db.Close()
}

func (a *Adapter) Ping(ctx context.Context) error {
	if a.db == nil {
		return fmt.Errorf("Oracle adapter is not connected")
	}
	return a.db.PingContext(ctx)
}

func (a *Adapter) GetDatabaseType() string { return AdapterType }

func (a *Adapter) GetDatabaseVersion(ctx context.Context) (string, error) {
	var version string
	err := a.db.QueryRowContext(ctx, "SELECT BANNER FROM V$VERSION WHERE ROWNUM = 1").Scan(&version)
	return version, err
}

func (a *Adapter) TableExists(ctx context.Context, name string) (bool, error) {
	owner, table, err := a.objectName(name)
	if err != nil {
		return false, err
	}
	var count int
	err = a.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM ALL_OBJECTS WHERE OWNER = :1 AND OBJECT_NAME = :2 AND OBJECT_TYPE IN ('TABLE','VIEW')",
		owner, table).Scan(&count)
	return count > 0, err
}

func (a *Adapter) GetTableNames(ctx context.Context) ([]string, error) {
	rows, err := a.db.QueryContext(ctx,
		"SELECT TABLE_NAME FROM ALL_TABLES WHERE OWNER = :1 ORDER BY TABLE_NAME", a.owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (a *Adapter) GetViewNames(ctx context.Context) ([]adapters.ViewInfo, error) {
	rows, err := a.db.QueryContext(ctx,
		"SELECT VIEW_NAME FROM ALL_VIEWS WHERE OWNER = :1 ORDER BY VIEW_NAME", a.owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var views []adapters.ViewInfo
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		views = append(views, adapters.ViewInfo{Name: name})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Finish the cursor before querying the same pool again.
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range views {
		var n int
		err := a.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM ALL_UPDATABLE_COLUMNS WHERE OWNER = :1 AND TABLE_NAME = :2 AND UPDATABLE = 'YES'",
			a.owner, views[i].Name).Scan(&n)
		if err != nil {
			return nil, err
		}
		views[i].IsUpdatable = n > 0
	}
	return views, nil
}

type oracleTx struct{ tx *sql.Tx }

func (t *oracleTx) Commit(context.Context) error   { return t.tx.Commit() }
func (t *oracleTx) Rollback(context.Context) error { return t.tx.Rollback() }

func (a *Adapter) BeginTx(ctx context.Context) (adapters.Tx, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &oracleTx{tx: tx}, nil
}

// normalizedColumn resolves the caller's case-insensitive spelling against
// the metadata spelling returned by Oracle.
func normalizedColumn(fields []string, wanted string) (string, bool) {
	for _, field := range fields {
		if strings.EqualFold(field, wanted) {
			return field, true
		}
	}
	return "", false
}
