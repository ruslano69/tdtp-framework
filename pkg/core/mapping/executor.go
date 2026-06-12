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
	rows := pkt.GetRows()

	for _, target := range cfg.Targets {
		// Resolve schema and bare table name. A dotted target table
		// ("edm.edm_employees") is split into schema + table; otherwise the
		// connection-level schema is used (default "public").
		schemaName, tableName := splitSchemaTable(target.Table, cfg.TargetConn.Schema)

		mapped, err := buildTargetPacket(target, tableName, rows, pkt.Schema.Fields)
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
//
// Each target field carries over the source field's Type/Subtype/Length and
// SpecialValues so the target adapter applies the same conversion contract as a
// normal import — in particular the NoDate marker ("0000-00-00", Navision/MSSQL
// "no date") is decoded to SQL NULL instead of being written verbatim into a
// DATE column. Enum-remapped fields become free text, so their type is reset.
func buildTargetPacket(target Target, tableName string, rows [][]string, srcFields []packet.Field) (*packet.DataPacket, error) {
	srcIndex := make(map[string]int, len(srcFields))
	for i, f := range srcFields {
		srcIndex[strings.ToLower(f.Name)] = i
	}

	// Build schema for the target packet, inheriting source field metadata.
	fields := make([]packet.Field, len(target.Fields))
	for i, fm := range target.Fields {
		colIdx, ok := srcIndex[strings.ToLower(fm.From)]
		if !ok {
			return nil, fmt.Errorf("source field %q not found in packet schema", fm.From)
		}
		src := srcFields[colIdx]
		f := packet.Field{
			Name: fm.To,
			Key:  strings.EqualFold(fm.To, target.UpsertKey),
		}
		if len(fm.Enum) > 0 {
			// Value is replaced by an arbitrary mapped string — source type no longer applies.
			f.Type = "TEXT"
		} else {
			f.Type = src.Type
			f.Subtype = src.Subtype
			f.Length = src.Length
			f.SpecialValues = src.SpecialValues
		}
		fields[i] = f
	}

	// Remap each row
	outRows := make([][]string, 0, len(rows))
	for _, srcRow := range rows {
		outRow := make([]string, len(target.Fields))
		for i, fm := range target.Fields {
			colIdx := srcIndex[strings.ToLower(fm.From)]
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
