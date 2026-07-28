package base

import (
	"testing"
	"time"
)

// TestFormatTimestampKeepsSubSecond is the point of the change: a value with
// microseconds must survive export.
func TestFormatTimestampKeepsSubSecond(t *testing.T) {
	src := time.Date(2026, 6, 16, 11, 38, 11, 528770000, time.UTC)

	got := formatTimestamp(src)
	if got != "2026-06-16T11:38:11.52877Z" {
		t.Errorf("formatTimestamp = %q, want the microseconds preserved", got)
	}

	// The canonical layout every reader in the framework uses.
	back, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("existing RFC3339 readers cannot parse it: %v", err)
	}
	if !back.Equal(src) {
		t.Errorf("round-trip lost precision: %v != %v", back, src)
	}
}

// TestFormatTimestampWholeSecondsUnchanged is the compatibility guarantee.
// Packets for data with no sub-second component must be byte-identical to what
// the old RFC3339 formatting produced, so their checksums do not move.
func TestFormatTimestampWholeSecondsUnchanged(t *testing.T) {
	for _, tc := range []time.Time{
		time.Date(2026, 6, 16, 11, 38, 11, 0, time.UTC),
		time.Date(1753, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2000, 2, 29, 23, 59, 59, 0, time.UTC),
	} {
		if got, want := formatTimestamp(tc), tc.UTC().Format(time.RFC3339); got != want {
			t.Errorf("formatTimestamp(%v) = %q, want %q (unchanged from before)", tc, got, want)
		}
	}
}

// TestFormatTimestampNormalizesZone keeps the pre-existing behaviour: whatever
// zone the driver hands over, the packet carries UTC.
func TestFormatTimestampNormalizesZone(t *testing.T) {
	eest := time.FixedZone("EEST", 3*3600)
	got := formatTimestamp(time.Date(2026, 6, 16, 14, 38, 11, 500000000, eest))
	if got != "2026-06-16T11:38:11.5Z" {
		t.Errorf("formatTimestamp = %q, want the UTC equivalent", got)
	}
}

func TestNormalizeSQLiteDateTime(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		fieldType string
		want      string
	}{
		{"whole seconds", "2026-06-16 11:38:11", "DATETIME", "2026-06-16T11:38:11Z"},
		// Used to come back as "2026-06-16T11:38:11Z" — the milliseconds SQLite
		// stored were cut off at 19 characters.
		{"milliseconds kept", "2026-06-16 11:38:11.817", "TIMESTAMP", "2026-06-16T11:38:11.817Z"},
		{"microseconds kept", "2026-06-16 11:38:11.528770", "TIMESTAMP", "2026-06-16T11:38:11.528770Z"},
		{"date passes through", "2026-06-16", "DATE", "2026-06-16"},
		{"empty passes through", "", "DATETIME", ""},
		// A value that already states its zone must not be relabelled UTC.
		{"explicit offset kept", "2026-06-16 11:38:11.817+03:00", "TIMESTAMP", "2026-06-16T11:38:11.817+03:00"},
		{"already RFC3339", "2026-06-16T11:38:11Z", "TIMESTAMP", "2026-06-16T11:38:11Z"},
		{"unexpected format", "not a date", "TIMESTAMP", "not a date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSQLiteDateTime(tt.in, tt.fieldType); got != tt.want {
				t.Errorf("normalizeSQLiteDateTime(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
