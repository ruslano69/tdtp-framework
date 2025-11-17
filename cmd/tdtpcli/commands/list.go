package commands

import (
	"context"
	"fmt"

	"github.com/queuebridge/tdtp/pkg/adapters"
)

// ListTables lists all tables in the database
func ListTables(ctx context.Context, config adapters.Config) error {
	// Create adapter
	adapter, err := adapters.New(ctx, config)
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
