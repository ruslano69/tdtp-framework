//go:build !nosqlite

package main

import (
	// Register SQLite adapter so --database.type: sqlite works.
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
)
