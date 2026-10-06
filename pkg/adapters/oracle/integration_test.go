package oracle

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Set TDTP_ORACLE18_DSN and TDTP_ORACLE21_DSN to run the same contract test
// against both supported server versions.
func TestOracleLive18And21(t *testing.T) {
	for _, version := range []string{"18", "21"} {
		dsn := os.Getenv("TDTP_ORACLE" + version + "_DSN")
		if dsn == "" {
			t.Run(version+"c", func(t *testing.T) { t.Skip("Oracle test DSN is not configured") })
			continue
		}
		t.Run(version+"c", func(t *testing.T) { oracleContract(t, dsn, version) })
	}
}

func oracleContract(t *testing.T, dsn, version string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	generic, err := adapters.New(ctx, adapters.Config{Type: AdapterType, DSN: dsn})
	if err != nil {
		t.Fatalf("connect Oracle %sc: %v", version, err)
	}
	a := generic.(*Adapter)
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	banner, err := a.GetDatabaseVersion(ctx)
	if err != nil || !strings.Contains(banner, version+"c") {
		t.Fatalf("version = %q, %v; expected %sc", banner, err, version)
	}
	unique := time.Now().UnixNano() % 1_000_000_000
	source := fmt.Sprintf("TDTP_ORA_SRC_%d", unique)
	target := fmt.Sprintf("TDTP_ORA_DST_%d", unique)
	t.Cleanup(func() {
		_ = a.DropTable(context.Background(), target)
		_ = a.DropTable(context.Background(), source)
	})
	create := fmt.Sprintf(`CREATE TABLE %s (
		ID NUMBER(10,0) PRIMARY KEY,
		LABEL VARCHAR2(80 CHAR),
		AMOUNT NUMBER(20,4),
		CREATED_AT TIMESTAMP(6),
		PAYLOAD BLOB)`, quote(source))
	if _, err := a.db.ExecContext(ctx, create); err != nil {
		t.Fatalf("create source: %v", err)
	}
	insert := fmt.Sprintf("INSERT INTO %s (ID,LABEL,AMOUNT,CREATED_AT,PAYLOAD) VALUES (:1,:2,:3,:4,:5)", quote(source))
	for i, label := range []string{"alpha", "beta", "gamma"} {
		amount := fmt.Sprintf("123456789012345%d.1234", i)
		if _, err := a.db.ExecContext(ctx, insert, i+1, label, amount,
			time.Date(2026, 1, i+1, 12, 34, 56, 123456000, time.UTC), []byte{0, 1, byte(i)}); err != nil {
			t.Fatalf("insert source %d: %v", i, err)
		}
	}
	schema, err := a.GetTableSchema(ctx, source)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if len(schema.Fields) != 5 || !schema.Fields[0].Key ||
		schema.Fields[2].Type != "DECIMAL" || schema.Fields[3].Type != "DATETIME" ||
		schema.Fields[4].Type != "BLOB" {
		t.Fatalf("unexpected schema: %+v", schema.Fields)
	}
	report, err := a.InspectTable(ctx, source)
	if err != nil || report.Stats.TotalRows != 3 || len(report.Columns) != 5 {
		t.Fatalf("inspect = %+v, %v", report, err)
	}
	names, err := a.GetTableNames(ctx)
	if err != nil || !containsName(names, source) {
		t.Fatalf("table list %v, %v", names, err)
	}
	view := fmt.Sprintf("TDTP_ORA_V_%d", unique)
	t.Cleanup(func() { _, _ = a.db.ExecContext(context.Background(), "DROP VIEW "+quote(view)) })
	if _, err := a.db.ExecContext(ctx, "CREATE VIEW "+quote(view)+" AS SELECT ID,LABEL FROM "+quote(source)); err != nil {
		t.Fatalf("create view: %v", err)
	}
	views, err := a.GetViewNames(ctx)
	if err != nil {
		t.Fatalf("list views: %v", err)
	}
	foundView := false
	for _, v := range views {
		if v.Name == view {
			foundView = true
			break
		}
	}
	if !foundView {
		t.Fatalf("view %s missing from list: %+v", view, views)
	}
	if viewSchema, err := a.GetTableSchema(ctx, view); err != nil || len(viewSchema.Fields) != 2 {
		t.Fatalf("view schema = %+v, %v", viewSchema, err)
	}
	mixed := fmt.Sprintf("TDTP Order %d", unique)
	t.Cleanup(func() { _ = a.DropTable(context.Background(), "["+mixed+"]") })
	if _, err := a.db.ExecContext(ctx, "CREATE TABLE "+quote(mixed)+` ("ID" NUMBER(10,0) PRIMARY KEY, "Last Name" VARCHAR2(30 CHAR))`); err != nil {
		t.Fatalf("create quoted table: %v", err)
	}
	if _, err := a.db.ExecContext(ctx, "INSERT INTO "+quote(mixed)+` ("ID", "Last Name") VALUES (1, 'Cherkasov')`); err != nil {
		t.Fatalf("insert quoted table: %v", err)
	}
	quotedQuery := packet.NewQuery()
	quotedQuery.Fields = []string{"ID", "Last Name"}
	quotedPackets, err := a.ExportTableWithQuery(ctx, "["+mixed+"]", quotedQuery, "test", "")
	if err != nil || len(quotedPackets) != 1 {
		t.Fatalf("quoted table export = %d packets, %v", len(quotedPackets), err)
	}
	quotedPackets[0].MaterializeRows()
	if len(quotedPackets[0].Data.Rows) != 1 || !strings.Contains(quotedPackets[0].Data.Rows[0].Value, "Cherkasov") {
		t.Fatalf("quoted table rows = %+v", quotedPackets[0].Data.Rows)
	}
	query := packet.NewQuery()
	query.Filters = &packet.Filters{And: &packet.LogicalGroup{Filters: []packet.Filter{
		{Field: "ID", Operator: "gt", Value: "1"},
	}}}
	query.OrderBy = &packet.OrderBy{Field: "ID", Direction: "ASC"}
	query.Limit, query.Offset = 1, 1
	filtered, err := a.ExportTableWithQuery(ctx, source, query, "test", "")
	if err != nil {
		t.Fatalf("filtered export: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("filtered packets = %d", len(filtered))
	}
	filtered[0].MaterializeRows()
	if len(filtered[0].Data.Rows) != 1 || !strings.HasPrefix(filtered[0].Data.Rows[0].Value, "3|") {
		t.Fatalf("page result = %+v", filtered[0].Data.Rows)
	}
	tail := packet.NewQuery()
	tail.OrderBy = &packet.OrderBy{Field: "ID", Direction: "ASC"}
	tail.Limit = -1
	tailPackets, err := a.ExportTableWithQuery(ctx, source, tail, "test", "")
	if err != nil {
		t.Fatalf("tail export: %v", err)
	}
	tailPackets[0].MaterializeRows()
	if len(tailPackets[0].Data.Rows) != 1 || !strings.HasPrefix(tailPackets[0].Data.Rows[0].Value, "3|") {
		t.Fatalf("tail result = %+v", tailPackets[0].Data.Rows)
	}
	packets, err := a.ExportTable(ctx, source)
	if err != nil {
		t.Fatalf("full export: %v", err)
	}
	if len(packets) != 1 {
		t.Fatalf("full export packets = %d", len(packets))
	}
	packets[0].MaterializeRows()
	if got := packets[0].Data.Rows[0].Value; !strings.Contains(got, "1234567890123450.1234") {
		t.Fatalf("numeric value lost on export: %q", got)
	}
	rawPacket, err := a.ExecuteRawQuery(ctx, "SELECT ID, AMOUNT FROM "+quote(source)+" WHERE ID <= 2 ORDER BY ID")
	if err != nil {
		t.Fatalf("pipeline source query: %v", err)
	}
	rawPacket.MaterializeRows()
	if len(rawPacket.Data.Rows) != 2 || !strings.Contains(rawPacket.Data.Rows[0].Value, "1234567890123450.1234") {
		t.Fatalf("pipeline query lost rows or precision: %+v", rawPacket.Data.Rows)
	}
	inc := adapters.IncrementalConfig{Enabled: true, Mode: "incremental", Strategy: "sequence",
		TrackingField: "ID", BatchSize: 2, OrderBy: "ASC"}
	firstBatch, last, err := a.ExportTableIncremental(ctx, source, inc)
	if err != nil || len(firstBatch) != 1 || last != "2" {
		t.Fatalf("first incremental batch = %d, %q, %v", len(firstBatch), last, err)
	}
	inc.InitialValue = last
	secondBatch, last, err := a.ExportTableIncremental(ctx, source, inc)
	if err != nil || len(secondBatch) != 1 || last != "3" {
		t.Fatalf("second incremental batch = %d, %q, %v", len(secondBatch), last, err)
	}
	inc.InitialValue = last
	emptyBatch, last, err := a.ExportTableIncremental(ctx, source, inc)
	if err != nil || len(emptyBatch) != 0 || last != "3" {
		t.Fatalf("empty incremental batch = %d, %q, %v", len(emptyBatch), last, err)
	}
	inc.TrackingField, inc.Strategy, inc.InitialValue = "CREATED_AT", "timestamp", "2026-01-02T12:34:56.123456Z"
	dateBatch, dateLast, err := a.ExportTableIncremental(ctx, source, inc)
	if err != nil || len(dateBatch) != 1 || !strings.HasPrefix(dateLast, "2026-01-03T") {
		t.Fatalf("datetime incremental batch = %d, %q, %v", len(dateBatch), dateLast, err)
	}
	rollbackTarget := fmt.Sprintf("TDTP_ORA_RB_%d", unique)
	t.Cleanup(func() { _ = a.DropTable(context.Background(), rollbackTarget) })
	rollbackPacket := *packets[0]
	rollbackPacket.Header.TableName = rollbackTarget
	if err := a.ImportPacket(ctx, &rollbackPacket, adapters.ImportStrategy("invalid")); err == nil {
		t.Fatal("invalid import strategy unexpectedly accepted")
	}
	if exists, err := a.TableExists(ctx, rollbackTarget); err != nil || exists {
		t.Fatalf("invalid strategy created a table: exists=%v err=%v", exists, err)
	}
	if err := a.ImportPackets(ctx, []*packet.DataPacket{&rollbackPacket, &rollbackPacket}, adapters.StrategyFail); err == nil {
		t.Fatal("duplicate second packet unexpectedly imported")
	}
	if count, err := a.GetRowCount(ctx, rollbackTarget); err != nil || count != 0 {
		t.Fatalf("multi-packet import did not roll back: %d, %v", count, err)
	}
	packets[0].Header.TableName = target
	if err := a.ImportPacket(ctx, packets[0], adapters.StrategyReplace); err != nil {
		t.Fatalf("round-trip import: %v", err)
	}
	if count, err := a.GetRowCount(ctx, target); err != nil || count != 3 {
		t.Fatalf("import row count = %d, %v", count, err)
	}
	var amount string
	if err := a.db.QueryRowContext(ctx, "SELECT TO_CHAR(AMOUNT, 'FM99999999999999999990D0000', 'NLS_NUMERIC_CHARACTERS=''.,''') FROM "+quote(target)+" WHERE ID = 1").Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != "1234567890123450.1234" {
		t.Fatalf("decimal precision lost: %q", amount)
	}
	var payload []byte
	var created time.Time
	if err := a.db.QueryRowContext(ctx, "SELECT PAYLOAD, CREATED_AT FROM "+quote(target)+" WHERE ID = 1").Scan(&payload, &created); err != nil {
		t.Fatal(err)
	}
	if string(payload) != string([]byte{0, 1, 0}) || created.Year() != 2026 || created.Nanosecond() != 123456000 {
		t.Fatalf("binary/datetime round trip changed: payload=%v created=%v", payload, created)
	}
	if _, err := a.db.ExecContext(ctx, "UPDATE "+quote(target)+" SET LABEL = 'manual' WHERE ID = 1"); err != nil {
		t.Fatal(err)
	}
	if err := a.ImportPacket(ctx, packets[0], adapters.StrategyIgnore); err != nil {
		t.Fatalf("ignore: %v", err)
	}
	var label string
	if err := a.db.QueryRowContext(ctx, "SELECT LABEL FROM "+quote(target)+" WHERE ID = 1").Scan(&label); err != nil || label != "manual" {
		t.Fatalf("ignore changed row: %q, %v", label, err)
	}
	if err := a.ImportPacket(ctx, packets[0], adapters.StrategyReplace); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := a.db.QueryRowContext(ctx, "SELECT LABEL FROM "+quote(target)+" WHERE ID = 1").Scan(&label); err != nil || label != "alpha" {
		t.Fatalf("replace did not update row: %q, %v", label, err)
	}
	if err := a.ImportPacket(ctx, packets[0], adapters.StrategyFail); err == nil {
		t.Fatal("duplicate insert with fail strategy succeeded")
	}
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
