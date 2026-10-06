package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
	return a.ImportPackets(ctx, []*packet.DataPacket{pkt}, strategy)
}

// Oracle DDL commits implicitly. Create a missing table before the DML
// transaction, then keep all rows of all packets in one real *sql.Tx.
func (a *Adapter) ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy adapters.ImportStrategy) error {
	if len(packets) == 0 {
		return nil
	}
	switch strategy {
	case adapters.StrategyFail, adapters.StrategyCopy, adapters.StrategyReplace, adapters.StrategyIgnore:
	default:
		return fmt.Errorf("unsupported Oracle import strategy %q", strategy)
	}
	var canonical *packet.DataPacket
	for i, pkt := range packets {
		if pkt == nil {
			return fmt.Errorf("packet %d is nil", i)
		}
		if pkt.Header.Type != packet.TypeReference && pkt.Header.Type != packet.TypeResponse {
			return fmt.Errorf("cannot import Oracle packet type %q", pkt.Header.Type)
		}
		pkt.MaterializeRows()
		if canonical == nil {
			canonical = pkt
		} else if pkt.Header.TableName != canonical.Header.TableName || !packet.SchemaEquals(pkt.Schema, canonical.Schema) {
			return fmt.Errorf("packet %d has a different table or schema", i)
		}
	}
	// replace/ignore need a key to MERGE on. Checked here, before any DDL:
	// Oracle commits CREATE TABLE implicitly, and the same refusal raised
	// later in insertRows used to leave a new empty table behind.
	if strategy == adapters.StrategyReplace || strategy == adapters.StrategyIgnore {
		if len(packet.ExtractKeyFields(canonical.Schema)) == 0 {
			return fmt.Errorf("Oracle %s import requires key fields", strategy)
		}
	}
	name := canonical.Header.TableName
	exists, err := a.TableExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		if err := a.CreateTable(ctx, name, canonical.Schema); err != nil {
			return err
		}
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, pkt := range packets {
		if err := a.insertRows(ctx, tx, name, pkt.Schema, pkt.Data.Rows, strategy); err != nil {
			return fmt.Errorf("import packet %d: %w", i, err)
		}
	}
	return tx.Commit()
}

func (a *Adapter) CreateTable(ctx context.Context, name string, schema packet.Schema) error {
	if len(schema.Fields) == 0 {
		return fmt.Errorf("cannot create Oracle table with no columns")
	}
	table, err := a.quotedTable(name)
	if err != nil {
		return err
	}
	columns := make([]string, 0, len(schema.Fields)+1)
	var keys []string
	for _, field := range schema.Fields {
		if field.Name == "" || strings.ContainsRune(field.Name, 0) {
			return fmt.Errorf("invalid Oracle column name %q", field.Name)
		}
		nativeType, err := typeForField(field)
		if err != nil {
			return err
		}
		col := quote(field.Name) + " " + nativeType
		if field.Key {
			col += " NOT NULL"
			keys = append(keys, quote(field.Name))
		}
		columns = append(columns, col)
	}
	if len(keys) > 0 {
		columns = append(columns, "PRIMARY KEY ("+strings.Join(keys, ", ")+")")
	}
	_, err = a.db.ExecContext(ctx, "CREATE TABLE "+table+" ("+strings.Join(columns, ", ")+")")
	if err != nil {
		return fmt.Errorf("create Oracle table %s: %w", name, err)
	}
	return nil
}

func (a *Adapter) DropTable(ctx context.Context, name string) error {
	table, err := a.quotedTable(name)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, "DROP TABLE "+table+" PURGE")
	return err
}

func (a *Adapter) RenameTable(ctx context.Context, oldName, newName string) error {
	oldOwner, oldTable, err := a.objectName(oldName)
	if err != nil {
		return err
	}
	newOwner, newTable, err := a.objectName(newName)
	if err != nil {
		return err
	}
	if oldOwner != newOwner {
		return fmt.Errorf("Oracle RENAME cannot change table owner")
	}
	_, err = a.db.ExecContext(ctx, "ALTER TABLE "+quote(oldOwner)+"."+quote(oldTable)+" RENAME TO "+quote(newTable))
	return err
}

func (a *Adapter) insertRows(ctx context.Context, tx *sql.Tx, name string, schema packet.Schema, rows []packet.Row, strategy adapters.ImportStrategy) error {
	if len(rows) == 0 {
		return nil
	}
	table, err := a.quotedTable(name)
	if err != nil {
		return err
	}
	if len(schema.Fields) == 0 {
		return fmt.Errorf("Oracle import schema has no fields")
	}
	columns := make([]string, len(schema.Fields))
	binds := make([]string, len(schema.Fields))
	sources := make([]string, len(schema.Fields))
	var keyConditions, updates []string
	for i, field := range schema.Fields {
		col := quote(field.Name)
		columns[i] = col
		binds[i] = fmt.Sprintf(":%d", i+1)
		sources[i] = binds[i] + " AS " + col
		if field.Key {
			keyConditions = append(keyConditions, "t."+col+" = s."+col)
		} else {
			updates = append(updates, "t."+col+" = s."+col)
		}
	}
	var statement string
	switch strategy {
	case adapters.StrategyFail, adapters.StrategyCopy:
		statement = "INSERT INTO " + table + " (" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(binds, ", ") + ")"
	case adapters.StrategyReplace, adapters.StrategyIgnore:
		if len(keyConditions) == 0 {
			return fmt.Errorf("Oracle %s import requires key fields", strategy)
		}
		statement = "MERGE INTO " + table + " t USING (SELECT " + strings.Join(sources, ", ") + " FROM DUAL) s ON (" +
			strings.Join(keyConditions, " AND ") + ")"
		if strategy == adapters.StrategyReplace && len(updates) > 0 {
			statement += " WHEN MATCHED THEN UPDATE SET " + strings.Join(updates, ", ")
		}
		values := make([]string, len(columns))
		for i, col := range columns {
			values[i] = "s." + col
		}
		statement += " WHEN NOT MATCHED THEN INSERT (" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(values, ", ") + ")"
	default:
		return fmt.Errorf("unsupported Oracle import strategy %q", strategy)
	}
	stmt, err := tx.PrepareContext(ctx, statement)
	if err != nil {
		return fmt.Errorf("prepare Oracle import: %w", err)
	}
	defer stmt.Close()
	for i, row := range rows {
		raw := base.ParseRowValues(row)
		args, err := base.ConvertRowToSQLValues(raw, schema, a.converter, AdapterType)
		if err != nil {
			return fmt.Errorf("row %d: %w", i, err)
		}
		// Decimal values must not make a string -> float64 -> NUMBER trip.
		for j, field := range schema.Fields {
			if args[j] == nil {
				continue
			}
			switch strings.ToUpper(field.Type) {
			case "DECIMAL":
				args[j] = raw[j]
			case "BOOLEAN", "BOOL":
				if b, ok := args[j].(bool); ok {
					if b {
						args[j] = 1
					} else {
						args[j] = 0
					}
				}
			}
		}
		if _, err := stmt.ExecContext(ctx, args...); err != nil {
			return fmt.Errorf("row %d: %w", i, err)
		}
	}
	return nil
}
