package mapping

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Execute applies cfg to pkt: remaps fields for each target and upserts into the target DB.
// When dryRun is true the transformation is performed but no data is written.
func Execute(ctx context.Context, cfg *MappingConfig, pkt *packet.DataPacket, dryRun bool) error {
	// Build source field index: name → column index
	srcIndex := make(map[string]int, len(pkt.Schema.Fields))
	for i, f := range pkt.Schema.Fields {
		srcIndex[strings.ToLower(f.Name)] = i
	}

	rows := pkt.GetRows()

	for _, target := range cfg.Targets {
		// Resolve schema and bare table name. A dotted target table
		// ("edm.edm_employees") is split into schema + table; otherwise the
		// connection-level schema is used (default "public").
		schemaName, tableName := splitSchemaTable(target.Table, cfg.TargetConn.Schema)

		mapped, err := buildTargetPacket(target, tableName, rows, srcIndex)
		if err != nil {
			return fmt.Errorf("build target packet for %q: %w", target.Table, err)
		}

		if dryRun {
			fmt.Printf("[dry-run] target=%q schema=%q table=%q rows=%d upsert_key=%q\n",
				target.Table, schemaName, tableName, len(rows), target.UpsertKey)
			for i, f := range mapped.Schema.Fields {
				fmt.Printf("  field[%d]: %s (key=%v)\n", i, f.Name, f.Key)
			}
			continue
		}

		// Open a fresh adapter per target with the resolved schema. The
		// postgres adapter prefixes the table with this schema when != "public".
		adapter, err := adapters.New(ctx, adapters.Config{
			Type:   cfg.TargetConn.Type,
			DSN:    cfg.TargetConn.DSN,
			Schema: schemaName,
		})
		if err != nil {
			return fmt.Errorf("connect to target %s: %w", cfg.TargetConn.Type, err)
		}

		if err := adapter.ImportPacket(ctx, mapped, adapters.StrategyReplace); err != nil {
			_ = adapter.Close(ctx)
			return fmt.Errorf("import to %s.%s: %w", schemaName, tableName, err)
		}
		_ = adapter.Close(ctx)
		fmt.Printf("✓ %d rows upserted → %s.%s\n", len(rows), schemaName, tableName)
	}
	return nil
}

// splitSchemaTable splits a possibly schema-qualified table name.
// "edm.edm_employees" → ("edm", "edm_employees").
// "edm_employees" → (defaultSchema or "public", "edm_employees").
func splitSchemaTable(table, defaultSchema string) (schema, name string) {
	if i := strings.IndexByte(table, '.'); i > 0 {
		return table[:i], table[i+1:]
	}
	if defaultSchema == "" {
		defaultSchema = "public"
	}
	return defaultSchema, table
}

// buildTargetPacket creates a new DataPacket with remapped fields for the given target.
// tableName is the bare table name (schema is applied by the adapter separately).
func buildTargetPacket(target Target, tableName string, rows [][]string, srcIndex map[string]int) (*packet.DataPacket, error) {
	// Build schema for the target packet
	fields := make([]packet.Field, len(target.Fields))
	for i, fm := range target.Fields {
		fields[i] = packet.Field{
			Name: fm.To,
			Type: "string", // generic; adapter casts to DB type via column metadata
			Key:  strings.EqualFold(fm.To, target.UpsertKey),
		}
	}

	// Remap each row
	outRows := make([][]string, 0, len(rows))
	for _, srcRow := range rows {
		outRow := make([]string, len(target.Fields))
		for i, fm := range target.Fields {
			colIdx, ok := srcIndex[strings.ToLower(fm.From)]
			if !ok {
				return nil, fmt.Errorf("source field %q not found in packet schema", fm.From)
			}
			val := ""
			if colIdx < len(srcRow) {
				val = srcRow[colIdx]
			}
			if len(fm.Enum) > 0 {
				if mapped, exists := fm.Enum[val]; exists {
					val = mapped
				}
			}
			outRow[i] = val
		}
		outRows = append(outRows, outRow)
	}

	pkt := packet.NewDataPacket(packet.TypeReference, tableName)
	pkt.Schema = packet.Schema{Fields: fields}
	pkt.SetRows(outRows)
	return pkt, nil
}
