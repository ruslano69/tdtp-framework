package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// matchesPattern reports whether name matches the given glob pattern.
// An empty pattern matches everything.
// Supports * and ? wildcards (path.Match semantics).
// % is treated as * for SQL-style patterns.
// Matching is case-insensitive.
func MatchesPattern(name, pattern string) bool {
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
	return ListTablesTo(os.Stdout, ctx, config, pattern)
}

// ListTablesTo is ListTables writing its report to w instead of stdout,
// so embedders (tdtpcli_v2 --quiet/--json) control the output stream.
func ListTablesTo(w io.Writer, ctx context.Context, config *adapters.Config, pattern string) error {
	_, err := ListTablesReport(w, ctx, config, pattern)
	return err
}

// ListTablesReport writes the v1 report and returns the same filtered names
// for callers that also need a structured result.
func ListTablesReport(w io.Writer, ctx context.Context, config *adapters.Config, pattern string) ([]string, error) {
	// Create adapter
	adapter, err := adapters.New(ctx, *config)
	if err != nil {
		return nil, fmt.Errorf("failed to create adapter: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	// Get full table list from the database
	tables, err := adapter.GetTableNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	// Filter by pattern
	filtered := tables[:0]
	for _, t := range tables {
		if MatchesPattern(t, pattern) {
			filtered = append(filtered, t)
		}
	}

	// Display results
	if len(filtered) == 0 {
		if pattern != "" {
			reportf(w, "No tables matching %q\n", pattern)
		} else {
			reportln(w, "No tables found")
		}
		return filtered, nil
	}

	if pattern != "" {
		reportf(w, "Found %d table(s) matching %q:\n", len(filtered), pattern)
	} else {
		reportf(w, "Found %d table(s):\n", len(filtered))
	}
	for i, table := range filtered {
		reportf(w, "  %d. %s\n", i+1, table)
	}

	return filtered, nil
}

// ListViews lists all database views with updatable status
func ListViews(ctx context.Context, config *adapters.Config) error {
	return ListViewsTo(os.Stdout, ctx, config)
}

// ListViewsTo is ListViews writing its report to w instead of stdout,
// so embedders (tdtpcli_v2 --quiet/--json) control the output stream.
func ListViewsTo(w io.Writer, ctx context.Context, config *adapters.Config) error {
	_, err := ListViewsReport(w, ctx, config)
	return err
}

// ListViewsReport writes the v1 report and returns the names from the same
// database query for callers that also need a structured result.
func ListViewsReport(w io.Writer, ctx context.Context, config *adapters.Config) ([]string, error) {
	// Create adapter
	adapter, err := adapters.New(ctx, *config)
	if err != nil {
		return nil, fmt.Errorf("failed to create adapter: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	// Get view list
	views, err := adapter.GetViewNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list views: %w", err)
	}
	names := make([]string, 0, len(views))

	// Display results
	if len(views) == 0 {
		reportln(w, "No views found")
		return names, nil
	}

	reportf(w, "Found %d view(s):\n", len(views))
	for i, view := range views {
		names = append(names, view.Name)
		// U* prefix for updatable views, R* prefix for read-only views
		prefix := "R*"
		if view.IsUpdatable {
			prefix = "U*"
		}
		reportf(w, "  %d. %s%s\n", i+1, prefix, view.Name)
	}

	reportln(w, "\nLegend:")
	reportln(w, "  U* = Updatable view (can import)")
	reportln(w, "  R* = Read-only view (export only)")

	return names, nil
}
