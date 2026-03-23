package commands

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// matchesPattern reports whether name matches the given glob pattern.
// An empty pattern matches everything.
// Supports * and ? wildcards (path.Match semantics).
// % is treated as * for SQL-style patterns.
// Matching is case-insensitive.
func matchesPattern(name, pattern string) bool {
	if pattern == "" {
		return true
	}
	// Normalize: SQL % → glob *, case-insensitive
	p := strings.ReplaceAll(strings.ToLower(pattern), "%", "*")
	matched, err := path.Match(p, strings.ToLower(name))
	return err == nil && matched
}

// ListTables lists all tables in the database, optionally filtered by a glob pattern.
// pattern="" lists all tables; pattern="user*" lists only matching ones.
func ListTables(ctx context.Context, config *adapters.Config, pattern string) error {
	// Create adapter
	adapter, err := adapters.New(ctx, *config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	// Get full table list from the database
	tables, err := adapter.GetTableNames(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tables: %w", err)
	}

	// Filter by pattern
	filtered := tables[:0]
	for _, t := range tables {
		if matchesPattern(t, pattern) {
			filtered = append(filtered, t)
		}
	}

	// Display results
	if len(filtered) == 0 {
		if pattern != "" {
			fmt.Printf("No tables matching %q\n", pattern)
		} else {
			fmt.Println("No tables found")
		}
		return nil
	}

	if pattern != "" {
		fmt.Printf("Found %d table(s) matching %q:\n", len(filtered), pattern)
	} else {
		fmt.Printf("Found %d table(s):\n", len(filtered))
	}
	for i, table := range filtered {
		fmt.Printf("  %d. %s\n", i+1, table)
	}

	return nil
}

// ListViews lists all database views with updatable status
func ListViews(ctx context.Context, config *adapters.Config) error {
	// Create adapter
	adapter, err := adapters.New(ctx, *config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	// Get view list
	views, err := adapter.GetViewNames(ctx)
	if err != nil {
		return fmt.Errorf("failed to list views: %w", err)
	}

	// Display results
	if len(views) == 0 {
		fmt.Println("No views found")
		return nil
	}

	fmt.Printf("Found %d view(s):\n", len(views))
	for i, view := range views {
		// U* prefix for updatable views, R* prefix for read-only views
		prefix := "R*"
		if view.IsUpdatable {
			prefix = "U*"
		}
		fmt.Printf("  %d. %s%s\n", i+1, prefix, view.Name)
	}

	fmt.Println("\nLegend:")
	fmt.Println("  U* = Updatable view (can import)")
	fmt.Println("  R* = Read-only view (export only)")

	return nil
}
