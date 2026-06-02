package main

import (
	"testing"
	"time"
)

func TestScheduleRecordTimezone(t *testing.T) {
	db, err := OpenOrchestratorDB(t.TempDir() + "/sched_tz.db")
	if err != nil {
		t.Fatalf("OpenOrchestratorDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	r := &ScheduleRecord{
		ID:         "tz-sched-1",
		Scenario:   "export-payroll",
		CronExpr:   "0 6 * * *",
		Params:     map[string]string{"period": "{{current_month}}"},
		Timezone:   "Europe/Kyiv",
		Enabled:    true,
		CreatedAt:  time.Now().UTC(),
		ModifiedAt: time.Now().UTC(),
	}
	if err := db.UpsertSchedule(r); err != nil {
		t.Fatalf("UpsertSchedule: %v", err)
	}

	got, err := db.GetSchedule("tz-sched-1")
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if got == nil {
		t.Fatal("GetSchedule returned nil, want record")
	}
	if got.Timezone != "Europe/Kyiv" {
		t.Errorf("Timezone = %q, want %q", got.Timezone, "Europe/Kyiv")
	}
}
