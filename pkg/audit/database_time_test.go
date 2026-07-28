package audit

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTimeTestAppender opens a SQLite audit sink with the same DSN parameter
// cmd/tdtpcli applies, so these tests exercise what production actually writes.
func newTimeTestAppender(t *testing.T) (*DatabaseAppender, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/audit.db?_time_format=sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ap, err := NewDatabaseAppender(DatabaseAppenderConfig{
		DB: db, TableName: "audit_log", Level: LevelStandard, AutoCreateTable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ap, db
}

// TestAuditTimeFilterAcrossZones is the regression this whole change exists for.
//
// Records written at the same instant from nodes on different zones used to be
// compared as raw text, so "2026-07-28T10:00:00Z" sorted below
// "2026-07-28T12:30:00+03:00" even though it is the later instant. A range
// query then returned fewer rows than it should — silently, with no error.
func TestAuditTimeFilterAcrossZones(t *testing.T) {
	ap, _ := newTimeTestAppender(t)

	eest := time.FixedZone("EEST", 3*3600)
	base := time.Date(2026, 7, 28, 12, 0, 0, 0, eest)

	entries := []time.Time{
		base,                      // 12:00 +0300
		base.Add(time.Hour).UTC(), // 13:00 +0300, expressed as 10:00Z
		base.Add(2 * time.Hour),   // 14:00 +0300
	}
	for i, ts := range entries {
		e := &Entry{ID: string(rune('a' + i)), Timestamp: ts, Operation: "query", Status: "success"}
		if err := ap.Append(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ap.Query(context.Background(), QueryFilter{StartTime: base.Add(30 * time.Minute)})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("range query returned %d rows, want 2 (the 13:00 record, written in UTC, is the one that used to disappear)", len(got))
	}
}

// TestAuditDeleteOlderThanAcrossZones covers the same comparison on the
// retention path, where getting it wrong destroys records rather than hiding
// them.
func TestAuditDeleteOlderThanAcrossZones(t *testing.T) {
	ap, _ := newTimeTestAppender(t)

	eest := time.FixedZone("EEST", 3*3600)
	base := time.Date(2026, 7, 28, 12, 0, 0, 0, eest)

	// "a" is genuinely old and must go. "b" is an hour NEWER than the cutoff
	// but was written by a caller holding a UTC time, so its text begins
	// "10:00" against a cutoff text of "11:00" — under the old comparison it
	// looked older than the cutoff and was destroyed.
	for i, ts := range []time.Time{
		base.Add(-2 * time.Hour),  // 10:00 +0300 — older than the cutoff
		base.Add(time.Hour).UTC(), // 13:00 +0300 as 10:00Z — newer than the cutoff
	} {
		e := &Entry{ID: string(rune('a' + i)), Timestamp: ts, Operation: "query", Status: "success"}
		if err := ap.Append(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}

	n, err := ap.DeleteOlderThan(context.Background(), base.Add(-time.Hour)) // cutoff 11:00 +0300
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteOlderThan removed %d rows, want exactly 1", n)
	}

	left, err := ap.Query(context.Background(), QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].ID != "b" {
		t.Errorf("retention destroyed the wrong record; survivors: %+v", left)
	}
}

// TestAuditTimestampStoredCanonically pins the bytes in the column.
//
// The driver's default is time.Time.String(), which put a zone abbreviation
// and a monotonic clock reading ("m=+10.312223101") into a TIMESTAMP column.
// Read back through the driver both forms look fine — it re-parses them — so
// the raw text is read here deliberately.
func TestAuditTimestampStoredCanonically(t *testing.T) {
	ap, db := newTimeTestAppender(t)

	e := &Entry{ID: "x", Timestamp: time.Now(), Operation: "query", Status: "success"}
	if err := ap.Append(context.Background(), e); err != nil {
		t.Fatal(err)
	}

	// CAST defeats the driver's decltype-driven re-parsing of TIMESTAMP columns.
	var raw string
	if err := db.QueryRow(`SELECT CAST(timestamp AS TEXT) FROM audit_log`).Scan(&raw); err != nil {
		t.Fatal(err)
	}

	if _, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", raw); err != nil {
		t.Errorf("stored timestamp %q is not canonical SQLite datetime: %v", raw, err)
	}
	if want := "+00:00"; len(raw) < 6 || raw[len(raw)-6:] != want {
		t.Errorf("stored timestamp %q does not end in %s — rows in mixed zones cannot be compared as text", raw, want)
	}
}

// TestAuditTextOrderMatchesTimeOrder is the property the two tests above rely
// on: once every row is UTC in a fixed-width offset format, sorting the column
// as text is sorting it as time. Fractional seconds are the awkward part —
// the format trims trailing zeros, so widths differ between rows.
func TestAuditTextOrderMatchesTimeOrder(t *testing.T) {
	ap, db := newTimeTestAppender(t)

	base := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	ordered := []time.Time{
		base,
		base.Add(500 * time.Microsecond),
		base.Add(45 * time.Millisecond),
		base.Add(500 * time.Millisecond),
		base.Add(time.Second),
		base.Add(time.Hour),
	}
	for i, ts := range ordered {
		e := &Entry{ID: string(rune('a' + i)), Timestamp: ts, Operation: "query", Status: "success"}
		if err := ap.Append(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := db.Query(`SELECT id FROM audit_log ORDER BY CAST(timestamp AS TEXT) ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	want := []string{"a", "b", "c", "d", "e", "f"}
	for i := range want {
		if i >= len(ids) || ids[i] != want[i] {
			t.Fatalf("text order %v does not match chronological order %v", ids, want)
		}
	}
}
