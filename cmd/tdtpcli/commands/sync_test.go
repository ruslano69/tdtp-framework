package commands

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
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
					if filter.Operator != ">" {
						t.Errorf("expected operator >, got %s", filter.Operator)
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
