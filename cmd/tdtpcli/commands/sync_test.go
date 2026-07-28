package commands

import (
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

func TestBuildIncrementalQuery(t *testing.T) {
	tests := []struct {
		name          string
		trackingField string
		lastSyncValue string
		batchSize     int
		expectFilter  bool
		expectOrderBy bool
		expectLimit   bool
	}{
		{
			name:          "First sync - no last value",
			trackingField: "updated_at",
			lastSyncValue: "",
			batchSize:     0,
			expectFilter:  false,
			expectOrderBy: true,
			expectLimit:   false,
		},
		{
			name:          "Incremental sync with timestamp",
			trackingField: "updated_at",
			lastSyncValue: "2024-11-17 10:00:00",
			batchSize:     0,
			expectFilter:  true,
			expectOrderBy: true,
			expectLimit:   false,
		},
		{
			name:          "Incremental sync with ID",
			trackingField: "id",
			lastSyncValue: "12345",
			batchSize:     0,
			expectFilter:  true,
			expectOrderBy: true,
			expectLimit:   false,
		},
		{
			name:          "With batch size",
			trackingField: "updated_at",
			lastSyncValue: "2024-11-17 10:00:00",
			batchSize:     1000,
			expectFilter:  true,
			expectOrderBy: true,
			expectLimit:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := buildIncrementalQuery(tt.trackingField, tt.lastSyncValue, tt.batchSize)

			if query == nil {
				t.Fatal("query is nil")
			}

			// Check filter
			if tt.expectFilter {
				if query.Filters == nil {
					t.Error("expected filters to be set")
				} else if query.Filters.And == nil {
					t.Error("expected AND filters to be set")
				} else if len(query.Filters.And.Filters) == 0 {
					t.Error("expected at least one filter")
				} else {
					filter := query.Filters.And.Filters[0]
					if filter.Field != tt.trackingField {
						t.Errorf("expected field %s, got %s", tt.trackingField, filter.Field)
					}
					if filter.Operator != "gt" {
						t.Errorf("expected operator gt, got %s", filter.Operator)
					}
					if filter.Value != tt.lastSyncValue {
						t.Errorf("expected value %s, got %s", tt.lastSyncValue, filter.Value)
					}
				}
			} else {
				if query.Filters != nil && query.Filters.And != nil && len(query.Filters.And.Filters) > 0 {
					t.Error("expected no filters")
				}
			}

			// Check ORDER BY
			if tt.expectOrderBy {
				if query.OrderBy == nil {
					t.Error("expected OrderBy to be set")
				} else {
					if query.OrderBy.Field != tt.trackingField {
						t.Errorf("expected order field %s, got %s", tt.trackingField, query.OrderBy.Field)
					}
					if query.OrderBy.Direction != "ASC" {
						t.Errorf("expected direction ASC, got %s", query.OrderBy.Direction)
					}
				}
			}

			// Check LIMIT
			if tt.expectLimit {
				if query.Limit != tt.batchSize {
					t.Errorf("expected limit %d, got %d", tt.batchSize, query.Limit)
				}
			} else {
				if query.Limit != 0 {
					t.Errorf("expected no limit, got %d", query.Limit)
				}
			}
		})
	}
}

func TestExtractLastSyncValue(t *testing.T) {
	tests := []struct {
		name          string
		packets       []*packet.DataPacket
		trackingField string
		expectValue   string
		expectError   bool
	}{
		{
			name:          "Empty packets",
			packets:       []*packet.DataPacket{},
			trackingField: "updated_at",
			expectValue:   "",
			expectError:   true,
		},
		{
			name: "Single packet with one row",
			packets: []*packet.DataPacket{
				{
					Schema: packet.Schema{
						Fields: []packet.Field{
							{Name: "id", Type: "INTEGER"},
							{Name: "updated_at", Type: "TIMESTAMP"},
						},
					},
					Data: packet.Data{
						Rows: []packet.Row{
							{Value: "1|2024-11-17 10:00:00"},
						},
					},
				},
			},
			trackingField: "updated_at",
			expectValue:   "2024-11-17 10:00:00",
			expectError:   false,
		},
		{
			name: "Multiple rows - extract max",
			packets: []*packet.DataPacket{
				{
					Schema: packet.Schema{
						Fields: []packet.Field{
							{Name: "id", Type: "INTEGER"},
							{Name: "updated_at", Type: "TIMESTAMP"},
						},
					},
					Data: packet.Data{
						Rows: []packet.Row{
							{Value: "1|2024-11-17 10:00:00"},
							{Value: "2|2024-11-17 12:00:00"}, // Max
							{Value: "3|2024-11-17 11:00:00"},
						},
					},
				},
			},
			trackingField: "updated_at",
			expectValue:   "2024-11-17 12:00:00",
			expectError:   false,
		},
		{
			name: "Numeric tracking field",
			packets: []*packet.DataPacket{
				{
					Schema: packet.Schema{
						Fields: []packet.Field{
							{Name: "id", Type: "INTEGER"},
							{Name: "name", Type: "VARCHAR"},
						},
					},
					Data: packet.Data{
						Rows: []packet.Row{
							{Value: "10|Alice"},
							{Value: "25|Bob"}, // Max
							{Value: "15|Charlie"},
						},
					},
				},
			},
			trackingField: "id",
			expectValue:   "25",
			expectError:   false,
		},
		{
			name: "Tracking field not found",
			packets: []*packet.DataPacket{
				{
					Schema: packet.Schema{
						Fields: []packet.Field{
							{Name: "id", Type: "INTEGER"},
						},
					},
					Data: packet.Data{
						Rows: []packet.Row{
							{Value: "1"},
						},
					},
				},
			},
			trackingField: "updated_at",
			expectValue:   "",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := extractLastSyncValue(tt.packets, tt.trackingField)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if value != tt.expectValue {
					t.Errorf("expected value %s, got %s", tt.expectValue, value)
				}
			}
		})
	}
}

// TestIncrementalQueryIsExecutable pushes the query buildIncrementalQuery
// produces through the SQL generator that actually consumes it.
//
// The previous test only compared the operator against a literal, and that
// literal was ">" -- a symbol no part of TDTQL accepts. It passed while every
// incremental sync after the first failed with "unknown operator: >". Asserting
// against the consumer instead of a copy of the value makes the two impossible
// to disagree.
func TestIncrementalQueryIsExecutable(t *testing.T) {
	query := buildIncrementalQuery("updated_at", "2026-01-03T10:00:00Z", 100)

	sql, err := tdtql.NewSQLGenerator().GenerateSQL("orders", query)
	if err != nil {
		t.Fatalf("generated query is not executable: %v", err)
	}

	// The watermark must end up as a strict lower bound: >= would resend the
	// last row every run, <= or = would send nothing new.
	if !strings.Contains(sql, "> '2026-01-03T10:00:00Z'") {
		t.Errorf("expected a strict > bound on the watermark, got: %s", sql)
	}
}

// TestIncrementalQueryFirstRunIsExecutable covers the no-watermark case, which
// takes the other branch and builds no filter at all.
func TestIncrementalQueryFirstRunIsExecutable(t *testing.T) {
	query := buildIncrementalQuery("updated_at", "", 100)

	sql, err := tdtql.NewSQLGenerator().GenerateSQL("orders", query)
	if err != nil {
		t.Fatalf("generated query is not executable: %v", err)
	}
	if strings.Contains(sql, "WHERE") {
		t.Errorf("first run must not filter, got: %s", sql)
	}
}

// TestExtractLastSyncValueNumeric covers an auto-increment tracking field whose
// values do not all have the same number of digits.
//
// The old lexical comparison put "9" above "25" and "100", so the watermark
// stopped at the highest first digit and every row past it was skipped
// permanently. The existing table-driven test missed it because all its ids
// were two digits wide.
func TestExtractLastSyncValueNumeric(t *testing.T) {
	pkt := &packet.DataPacket{
		Schema: packet.Schema{Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "VARCHAR"},
		}},
		Data: packet.Data{Rows: []packet.Row{
			{Value: "9|Alice"},
			{Value: "100|Bob"},
			{Value: "25|Charlie"},
		}},
	}

	got, err := extractLastSyncValue([]*packet.DataPacket{pkt}, "id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "100" {
		t.Errorf("expected watermark 100, got %s", got)
	}
}

func TestTrackingGreater(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"integers across a digit boundary", "100", "99", true},
		{"integers, smaller", "9", "25", false},
		{"negative integers", "-1", "-2", true},
		{"timestamps", "2026-01-04T10:00:00Z", "2026-01-03T10:00:00Z", true},
		{"timestamps, equal", "2026-01-03T10:00:00Z", "2026-01-03T10:00:00Z", false},
		// Fractional seconds sort before a bare "Z" lexically, so this pair is
		// the one a string comparison gets backwards.
		{"fractional beats whole second", "2026-01-03T10:00:00.5Z", "2026-01-03T10:00:00Z", true},
		{"whole second loses to fractional", "2026-01-03T10:00:00Z", "2026-01-03T10:00:00.5Z", false},
		{"non-numeric, non-time falls back to string", "v2", "v1", true},
		// A ULID or similar is fixed-width and base32, where string order is
		// the intended order.
		{"ulid-like", "01HQ0000000000000000000002", "01HQ0000000000000000000001", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trackingGreater(tt.a, tt.b); got != tt.want {
				t.Errorf("trackingGreater(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
