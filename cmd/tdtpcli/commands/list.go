package commands

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// ListTables lists all tables in the database
func ListTables(ctx context.Context, config *adapters.Config) error {
	// Create adapter
	adapter, err := adapters.New(ctx, *config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(ctx)

	// Get table list
	tables, err := adapter.GetTableNames(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tables: %w", err)
	}

	// Display results
	if len(tables) == 0 {
		fmt.Println("No tables found")
		return nil
	}

	fmt.Printf("Found %d table(s):\n", len(tables))
	for i, table := range tables {
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
	defer adapter.Close(ctx)

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
