package main

// adapters_register.go — the database drivers the inventory probes need.
//
// An adapter registers itself in its init(). The orchestrator uses none of them
// for a scenario run (tdtpcli does that), but the inventory connects to each
// source database, so the adapters are linked here.

import (
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mysql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/oracle"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
)
