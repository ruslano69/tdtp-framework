package main

import (
	"net/http/httptest"
	"testing"
)

func TestQueryLimit(t *testing.T) {
	tests := []struct {
		name, query string
		want        int
	}{
		{"absent", "", 100},
		{"explicit", "?limit=15", 15},
		{"clamped to max", "?limit=9999", 500},
		{"zero falls back", "?limit=0", 100},
		{"negative falls back", "?limit=-5", 100},
		{"garbage falls back", "?limit=all", 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/jobs"+tt.query, nil)
			if got := queryLimit(r, 100, 500); got != tt.want {
				t.Errorf("queryLimit(%q) = %d, want %d", tt.query, got, tt.want)
			}
		})
	}
}
