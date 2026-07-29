package main

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// gaugeFor reads the current value of the schedule gauge, or reports that no
// series exists yet — which is itself a meaningful state.
func gaugeFor(t *testing.T, id, scenario string) (float64, bool) {
	t.Helper()
	// Count first: GetMetricWithLabelValues creates the series as a side
	// effect, so asking for the metric before checking is how you prove a
	// series exists that you just created.
	if testutil.CollectAndCount(scheduleLastStatus) == 0 {
		return 0, false
	}
	g, err := scheduleLastStatus.GetMetricWithLabelValues(id, scenario)
	if err != nil {
		t.Fatal(err)
	}
	return testutil.ToFloat64(g), true
}

// TestScheduleGaugeFollowsTheOutcome is the fix this test exists for.
//
// The gauge used to be written at dispatch, with the status the scheduler
// stamps at that moment — "running" — which mapped to the sentinel meaning
// "never run". So a schedule that had just completed successfully reported
// that it had never run, for good: an alert on the sentinel fired constantly,
// one on failure never fired at all, and /schedules and Prometheus disagreed
// about the same schedule.
func TestScheduleGaugeFollowsTheOutcome(t *testing.T) {
	scheduleLastStatus.Reset()

	db := newScheduleStatusDB(t)
	seedSchedule(t, db, "sched-metric")

	// Dispatch alone must not write the gauge: there is no outcome yet.
	if _, exists := gaugeFor(t, "sched-metric", "s"); exists {
		t.Error("gauge was written at dispatch, before any outcome existed")
	}

	if err := db.InsertJob(&Job{ID: "j1", ScheduleID: "sched-metric", Scenario: "s",
		Status: JobRunning, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateJobDone("j1", JobDone, "", ""); err != nil {
		t.Fatal(err)
	}

	v, exists := gaugeFor(t, "sched-metric", "s")
	if !exists {
		t.Fatal("no series after a completed run")
	}
	if v != 1 {
		t.Errorf("gauge = %v after a successful run, want 1", v)
	}
}

func TestScheduleGaugeOnFailureAndCancel(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   JobStatus
	}{
		{"failed", JobFailed},
		{"cancelled", JobCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheduleLastStatus.Reset()
			db := newScheduleStatusDB(t)
			seedSchedule(t, db, "s")

			if err := db.InsertJob(&Job{ID: "j", ScheduleID: "s", Scenario: "s",
				Status: JobRunning, StartedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateJobDone("j", tc.st, "", ""); err != nil {
				t.Fatal(err)
			}

			v, exists := gaugeFor(t, "s", "s")
			if !exists {
				t.Fatal("no series after an outcome")
			}
			if v != 0 {
				t.Errorf("gauge = %v after %s, want 0", v, tc.st)
			}
		})
	}
}

// A manual run belongs to no schedule and must leave every gauge alone.
func TestScheduleGaugeIgnoresManualRuns(t *testing.T) {
	scheduleLastStatus.Reset()

	db := newScheduleStatusDB(t)
	seedSchedule(t, db, "sched-untouched")

	if err := db.InsertJob(&Job{ID: "manual", Scenario: "s",
		Status: JobRunning, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateJobDone("manual", JobDone, "", ""); err != nil {
		t.Fatal(err)
	}

	if n := testutil.CollectAndCount(scheduleLastStatus); n != 0 {
		t.Errorf("a manual run created %d schedule series", n)
	}
}

// The stale-job guard covers the gauge as well as the row: a slow job
// finishing after a newer one started must not report on the schedule.
func TestScheduleGaugeIgnoresStaleJob(t *testing.T) {
	scheduleLastStatus.Reset()

	db := newScheduleStatusDB(t)
	seedSchedule(t, db, "s")

	older := time.Now().UTC().Add(-time.Minute)
	if err := db.InsertJob(&Job{ID: "slow", ScheduleID: "s", Scenario: "s",
		Status: JobRunning, StartedAt: older}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertJob(&Job{ID: "fresh", ScheduleID: "s", Scenario: "s",
		Status: JobRunning, StartedAt: older.Add(30 * time.Second)}); err != nil {
		t.Fatal(err)
	}

	if err := db.UpdateJobDone("fresh", JobDone, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateJobDone("slow", JobFailed, "", ""); err != nil {
		t.Fatal(err)
	}

	v, exists := gaugeFor(t, "s", "s")
	if !exists {
		t.Fatal("no series")
	}
	if v != 1 {
		t.Errorf("gauge = %v — the stale job overwrote the newer run's outcome", v)
	}
}
