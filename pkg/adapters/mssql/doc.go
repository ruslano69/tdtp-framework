// Package mssql provides a Microsoft SQL Server adapter for TDTP Framework.
//
// This adapter supports SQL Server 2012 and higher with explicit compatibility modes.
//
// Features:
//   - SQL Server 2012+ compatibility (minimum version)
//   - Explicit compatibility mode configuration
//   - Feature detection for automatic version handling
//   - Strict mode for production safety
//   - Export: SQL Server → TDTP packets
//   - Import: TDTP packets → SQL Server
//   - MERGE for UPSERT operations
//   - OFFSET/FETCH for pagination
//   - Schema support (dbo, custom schemas)
//   - Bulk operations via Table-Valued Parameters
//
// Compatibility Modes:
//   - "2012" - SQL Server 2012-2014 (OFFSET/FETCH, MERGE, no JSON)
//   - "2016" - SQL Server 2016-2017 (adds JSON, STRING_SPLIT)
//   - "2019" - SQL Server 2019+ (all modern features)
//   - "auto" - Auto-detect (not recommended for production)
//
// Usage:
//
//	import (
//	    "context"
//	    "github.com/ruslano69/tdtp-framework-main/pkg/adapters"
//	    _ "github.com/ruslano69/tdtp-framework-main/pkg/adapters/mssql"
//	)
//
//	func main() {
//	    ctx := context.Background()
//
//	    cfg := adapters.Config{
//	        Type: "mssql",
//	        DSN:  "server=localhost;user id=sa;password=pass;database=mydb",
//	        CompatibilityMode: "2012",  // Explicit SQL Server 2012 compatibility
//	        StrictCompatibility: true,   // Error on incompatible functions
//	    }
//
//	    adapter, err := adapters.New(ctx, cfg)
//	    if err != nil {
//	        panic(err)
//	    }
//	    defer adapter.Close(ctx)
//
//	    // Export table
//	    packets, err := adapter.ExportTable(ctx, "Users")
//
//	    // Import packet
//	    err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
//	}
//
// SQL Server 2012 Compatibility:
//
// The adapter is designed to work with SQL Server 2012 as the minimum version.
// When configured with compatibility_mode: "2012", it will ONLY use features
// available in SQL Server 2012:
//
// Supported:
//   - OFFSET/FETCH for pagination
//   - MERGE for UPSERT
//   - IIF, TRY_CONVERT, FORMAT
//   - Table-Valued Parameters
//
// Not Supported (SQL Server 2016+):
//   - JSON_VALUE, JSON_QUERY, FOR JSON
//   - STRING_SPLIT
//   - DROP IF EXISTS
//
// Not Supported (SQL Server 2017+):
//   - STRING_AGG
//   - TRIM, CONCAT_WS
//
// Type Mapping:
//
//	SQL Server Type      TDTP Type       Notes
//	─────────────────────────────────────────────────
//	INT, BIGINT          INTEGER
//	DECIMAL, NUMERIC     DECIMAL         precision, scale
//	NVARCHAR             TEXT            Unicode support
//	BIT                  BOOLEAN
//	DATE                 DATE
//	DATETIME2            TIMESTAMP       High precision
//	UNIQUEIDENTIFIER     TEXT(36)        UUID as string
//	VARBINARY            BLOB
//
// See docs/MSSQL_2012_COMPATIBILITY.md for full compatibility details.
// See docs/MSSQL_DEV_VS_PROD.md for dev vs production environment guidance.
// See docs/MSSQL_COMPATIBILITY_MODES.md for compatibility mode architecture.
package mssql

const (
	// AdapterType is the type identifier for MS SQL Server adapter
	AdapterType = "mssql"

	// Version is the adapter version
	Version = "1.0.0"
)
