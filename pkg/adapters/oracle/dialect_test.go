package oracle

import (
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

func TestSQLDialect(t *testing.T) {
	a := &Adapter{owner: "TDTP"}
	cases := []struct {
		name, table string
		query       *packet.Query
		want        string
	}{
		{"limit", "orders", &packet.Query{Limit: 5}, `FROM "TDTP"."ORDERS" FETCH FIRST 5 ROWS ONLY`},
		{"offset", "orders", &packet.Query{Offset: 3}, `FROM "TDTP"."ORDERS" OFFSET 3 ROWS`},
		{"page", "orders", &packet.Query{OrderBy: &packet.OrderBy{Field: "ID", Direction: "ASC"}, Limit: 5, Offset: 3}, `ORDER BY ID ASC OFFSET 3 ROWS FETCH NEXT 5 ROWS ONLY`},
		{"quoted table", "[Order Details]", &packet.Query{Limit: 1}, `FROM "TDTP"."Order Details" FETCH FIRST 1 ROWS ONLY`},
		{"tail", "orders", &packet.Query{OrderBy: &packet.OrderBy{Field: "ID", Direction: "ASC"}, Limit: -2}, `FETCH FIRST 2 ROWS ONLY) tdtp_tail ORDER BY ID ASC`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			sql, err := tdtql.NewSQLGenerator().GenerateSQL(tt.table, tt.query)
			if err != nil {
				t.Fatal(err)
			}
			got := (sqlDialect{adapter: a}).AdaptSQL(sql, tt.table, packet.Schema{}, tt.query)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("SQL = %q, want fragment %q", got, tt.want)
			}
			if strings.Contains(got, " LIMIT ") || strings.Contains(got, ") AS _tail") {
				t.Fatalf("Oracle SQL still contains SQLite syntax: %q", got)
			}
		})
	}
}

func TestIdentifierCase(t *testing.T) {
	for input, want := range map[string]string{
		"orders": "ORDERS", "[Order Details]": "Order Details", `"MiXeD"`: "MiXeD",
	} {
		got, err := identifier(input)
		if err != nil || got != want {
			t.Fatalf("identifier(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}
