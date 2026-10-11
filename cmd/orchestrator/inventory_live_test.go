//go:build integration

package main

// Live tests for the inventory against PostgreSQL. CI runs them in the live
// job with POSTGRES_TEST_DSN set. Unlike the other live tests, an unreachable
// database here fails rather than skips: the test is the check that the
// inventory reports a real endpoint correctly, and a skip would report nothing.

import (
	"context"
	"net/url"
	"os"
	"testing"
)

// liveScenario builds a scenario whose only source is the given postgres DSN.
func liveScenario(t *testing.T, name, dsn string) *Scenario {
	t.Helper()
	return scenarioFromFile(t, name, "orchestrator:\n  name: "+name+"\nsources:\n  - name: pg\n    type: postgres\n    dsn: \""+dsn+"\"\n")
}

func TestInventory_LivePostgresAnswersOkAndRefusesWrongPassword(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN not set")
	}
	good := liveScenario(t, "pg-good", dsn)

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("POSTGRES_TEST_DSN: %v", err)
	}
	u.User = url.UserPassword(u.User.Username(), "not-the-password-"+u.User.Username())
	bad := liveScenario(t, "pg-bad", u.String())

	db := newScheduleStatusDB(t)
	svc := NewInventoryService(db, map[string]*Scenario{"pg-good": good, "pg-bad": bad})
	id, _, err := svc.Run(context.Background(), true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rows, err := db.inventoryRows(id)
	if err != nil {
		t.Fatal(err)
	}

	var goodRow, badRow *invRow
	for i := range rows {
		switch {
		case rows[i].Kind != "db":
		case containsString(rows[i].Scenarios, "pg-good"):
			goodRow = &rows[i]
		case containsString(rows[i].Scenarios, "pg-bad"):
			badRow = &rows[i]
		}
	}
	if goodRow == nil || badRow == nil {
		t.Fatalf("expected a db row for each scenario, got %+v", rows)
	}
	if goodRow.Status != "ok" {
		t.Fatalf("reachable database: status %s class %q, want ok", goodRow.Status, goodRow.Class)
	}
	if badRow.Status != "degraded" || badRow.Class != "auth" {
		t.Fatalf("wrong password: status %s class %q, want degraded/auth", badRow.Status, badRow.Class)
	}
	// Same server, different credentials: two probes, two rows, so the reachable
	// one is not hidden behind the wrong one. The second key carries a suffix.
	if goodRow.Key == badRow.Key {
		t.Fatalf("one row for two credential sets: %q", goodRow.Key)
	}
}
