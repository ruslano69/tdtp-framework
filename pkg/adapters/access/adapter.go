//go:build windows

// Package access provides a TDTP adapter for Microsoft Access (.mdb/.accdb) via ODBC.
//
// IMPORTANT CONSTRAINTS:
//   - Windows only: uses Win32 COM (ADOX), MDAC ODBC and SysWOW64\cscript.exe.
//   - 32-bit only: Microsoft Jet 4.0 ODBC is a 32-bit component.
//     Always build with GOARCH=386:
//     $env:GOARCH="386"; go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/
//
// Schema introspection uses ADOX (ActiveX Data Objects Extensions) via an embedded
// VBScript running under C:\Windows\SysWOW64\cscript.exe. The ADOX path requires
// 32-bit COM regardless of the host process bitness, so x64 builds are not supported.
package access

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/alexbrainman/odbc" // register odbc driver
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func init() {
	adapters.Register("access", func() adapters.Adapter {
		return &Adapter{}
	})
}

// Adapter implements adapters.Adapter for Microsoft Access via ODBC.
// Column names arrive as UTF-8 via ODBC Unicode API (SQLDescribeColW).
// Cell data from old Jet 2.x databases may arrive as ANSI bytes — use charset config to convert.
type Adapter struct {
	db           *sql.DB
	config       adapters.Config
	exportHelper *base.ExportHelper
	converter    *base.UniversalTypeConverter
	decoder      *encoding.Decoder // non-nil when charset conversion needed (e.g. windows-1251)
}

// resolveDecoder returns a charmap decoder for the given charset name, or nil for UTF-8/empty.
func resolveDecoder(charset string) *encoding.Decoder {
	switch strings.ToLower(strings.ReplaceAll(charset, "-", "")) {
	case "windows1251", "cp1251", "1251", "cyrillic":
		return charmap.Windows1251.NewDecoder()
	case "windows1252", "cp1252", "1252", "latin1":
		return charmap.Windows1252.NewDecoder()
	case "koi8r":
		return charmap.KOI8R.NewDecoder()
	case "iso88591":
		return charmap.ISO8859_1.NewDecoder()
	case "iso88592":
		return charmap.ISO8859_2.NewDecoder()
	default:
		return nil
	}
}

// decodeString converts a string from the source charset to UTF-8.
// Returns s unchanged when decoder is nil (already UTF-8).
func (a *Adapter) decodeString(s string) string {
	if a.decoder == nil {
		return s
	}
	out, err := a.decoder.String(s)
	if err != nil {
		return s // return original on decode error
	}
	return out
}

// Connect opens an ODBC connection to an Access .mdb/.accdb file.
// DSN format (connection string):
//
//	Driver={Microsoft Access Driver (*.mdb, *.accdb)};DBQ=C:\path\to\db.mdb;SystemDB=C:\path\to\system.mda;UID=Admin;PWD=secret;
//
// Config.Charset: set to "windows-1251" (or other) if text data needs conversion to UTF-8.
func (a *Adapter) Connect(ctx context.Context, cfg adapters.Config) error {
	dsn := cfg.DSN
	if dsn == "" {
		return fmt.Errorf("access: DSN (connection string) is required")
	}

	db, err := sql.Open("odbc", dsn)
	if err != nil {
		return fmt.Errorf("access: failed to open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("access: failed to ping: %w", err)
	}

	a.db = db
	a.config = cfg
	a.decoder = resolveDecoder(cfg.Charset)

	// Init converter and export helper
	a.converter = base.NewUniversalTypeConverter()
	a.exportHelper = base.NewExportHelper(a, a, a.converter, nil)

	return nil
}

func (a *Adapter) Close(ctx context.Context) error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

func (a *Adapter) Ping(ctx context.Context) error {
	if a.db == nil {
		return fmt.Errorf("access: not connected")
	}
	return a.db.PingContext(ctx)
}

func (a *Adapter) GetDatabaseType() string { return "access" }

func (a *Adapter) GetDatabaseVersion(ctx context.Context) (string, error) {
	return "Microsoft Access (Jet/ACE via ODBC)", nil
}

func (a *Adapter) DB() *sql.DB { return a.db }

// TableExists checks if a table exists.
func (a *Adapter) TableExists(ctx context.Context, tableName string) (bool, error) {
	names, err := a.GetTableNames(ctx)
	if err != nil {
		return false, err
	}
	for _, n := range names {
		if strings.EqualFold(n, tableName) {
			return true, nil
		}
	}
	return false, nil
}

// GetTableNames returns all user table names (excludes MSys* system tables).
func (a *Adapter) GetTableNames(ctx context.Context) ([]string, error) {
	// MSysObjects: Type=1 are tables.
	// Flags=0 are normal local tables; Flags=NULL occurs in Jet 2.x databases where
	// the field was not always populated. Exclude MSys*/~TMPCLP* system names explicitly.
	// Do NOT filter by Flags=0 alone — that misses ~half the tables in old .mdb files.
	query := `SELECT Name FROM MSysObjects WHERE Type=1 AND (Flags=0 OR IsNull(Flags)) AND Left(Name,4)<>'MSys' AND Left(Name,7)<>'~TMPCLP' ORDER BY Name`
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		// Fallback: MSysObjects might be restricted; try INFORMATION_SCHEMA-like query
		return a.getTableNamesFallback(ctx)
	}
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// getTableNamesFallback uses SELECT on known-empty query to discover tables via error.
// Actually uses sp_tables equivalent via ODBC catalog — just try a simpler query.
func (a *Adapter) getTableNamesFallback(ctx context.Context) ([]string, error) {
	// Access ODBC exposes tables through ODBC catalog API;
	// alexbrainman/odbc doesn't expose it directly, so we hint via a known query.
	// Return empty and let the user specify the table explicitly.
	return nil, fmt.Errorf("access: cannot read MSysObjects (no permission); specify table name explicitly")
}

// GetViewNames returns views — Access calls them queries.
func (a *Adapter) GetViewNames(ctx context.Context) ([]adapters.ViewInfo, error) {
	query := `SELECT Name FROM MSysObjects WHERE Type=5 AND (Flags=0 OR IsNull(Flags)) AND Left(Name,4)<>'MSys' ORDER BY Name`
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, nil // views not accessible
	}
	defer func() { _ = rows.Close() }()

	var views []adapters.ViewInfo
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		views = append(views, adapters.ViewInfo{Name: name, IsUpdatable: false})
	}
	return views, rows.Err()
}

// BeginTx starts a transaction.
func (a *Adapter) BeginTx(ctx context.Context) (adapters.Tx, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &accessTx{tx: tx}, nil
}

type accessTx struct{ tx *sql.Tx }

func (t *accessTx) Commit(ctx context.Context) error   { return t.tx.Commit() }
func (t *accessTx) Rollback(ctx context.Context) error { return t.tx.Rollback() }

// ExportTable exports a full table.
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTable(ctx, tableName)
}

// ExportTableWithQuery exports with TDTQL filters.
func (a *Adapter) ExportTableWithQuery(ctx context.Context, tableName string, query *packet.Query, sender, recipient string) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTableWithQuery(ctx, tableName, query, sender, recipient)
}

// ExportTableIncremental is not implemented for Access.
func (a *Adapter) ExportTableIncremental(ctx context.Context, tableName string, cfg adapters.IncrementalConfig) ([]*packet.DataPacket, string, error) {
	return nil, "", fmt.Errorf("access: incremental export not supported")
}

// ExecuteRawQuery runs an arbitrary SELECT and returns a DataPacket.
func (a *Adapter) ExecuteRawQuery(ctx context.Context, query string) (*packet.DataPacket, error) {
	if a.db == nil {
		return nil, fmt.Errorf("access: not connected")
	}
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("access: query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("access: failed to get columns: %w", err)
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("access: failed to get column types: %w", err)
	}

	schema := packet.Schema{Fields: make([]packet.Field, len(columns))}
	for i, col := range columns {
		_ = columnTypes[i] // type info not available via Jet ODBC; TEXT is safe default
		schema.Fields[i] = packet.Field{Name: col, Type: "TEXT", Length: 1000}
	}

	scannedRows, err := a.scanRows(rows, schema)
	if err != nil {
		return nil, fmt.Errorf("access: scan failed: %w", err)
	}

	dp := packet.NewDataPacket(packet.TypeReference, "query_result")
	dp.Schema = schema
	dp.Data = packet.RowsToData(scannedRows)
	dp.Header.RecordsInPart = len(scannedRows)
	return dp, nil
}
