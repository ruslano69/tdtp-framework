package main

// drivers.go — adapter registration for the v2 binary. Each adapter
// package self-registers on import; without these blank imports
// adapters.New knows no database types. Mirrors cmd/tdtpcli's main.go
// (mssql/mysql/postgres unconditional) plus drivers_sqlite.go (!nosqlite)
// and drivers_access.go (windows-only): same set, same tags.

import (
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mysql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
)
