package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// fakeSink records what it was asked to send and fails on demand: errs is
// consumed one entry per Send, a nil entry means success.
type fakeSink struct {
	mu    sync.Mutex
	calls []string // "type|job_id"
	errs  []error
}

func (f *fakeSink) Send(_ context.Context, ev OutboxEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ev.Type+"|"+ev.JobID)
	if len(f.errs) == 0 {
		return nil
	}
	err := f.errs[0]
	f.errs = f.errs[1:]
	return err
}

func (f *fakeSink) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// newTestDispatcher builds a dispatcher with the given sinks, no backoff
// delay, and the real clock.
func newTestDispatcher(db *OrchestratorDB, sinks map[string]EventSink, maxAttempts int) *EventDispatcher {
	max := map[string]int{}
	for name := range sinks {
		max[name] = maxAttempts
	}
	return &EventDispatcher{
		db:          db,
		sinks:       sinks,
		maxAttempts: max,
		backoff:     func(int) time.Duration { return 0 },
		now:         time.Now,
		batch:       outboxBatch,
	}
}

// eventDefs returns validated definitions with the given name and filters.
func eventDefs(t *testing.T, defs ...EventSinkDef) []EventSinkDef {
	t.Helper()
	if err := prepareSinks(defs); err != nil {
		t.Fatalf("prepareSinks: %v", err)
	}
	return defs
}

func testJob(id, scenario string) *Job {
	return &Job{
		ID:       id,
		Scenario: scenario,
		Params:   map[string]string{"period": "2026-06"},
		Status:   JobRunning,
		// Stored at second precision, like the jobs table itself, so reads
		// compare equal.
		StartedAt: time.Now().UTC().Truncate(time.Second),
		Runner:    "tdtpcli",
	}
}

type outboxRow struct {
	ID, JobID, Sink, Type, Status, Payload string
	Attempts                               int
	LastError                              *string
}

func readOutbox(t *testing.T, db *OrchestratorDB) []outboxRow {
	t.Helper()
	rows, err := db.db.Query(`
		SELECT id, job_id, sink, type, status, payload, attempts, last_error
		  FROM outbox ORDER BY rowid`)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.ID, &r.JobID, &r.Sink, &r.Type, &r.Status, &r.Payload, &r.Attempts, &r.LastError); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func jobStatusOf(t *testing.T, db *OrchestratorDB, id string) string {
	t.Helper()
	var s string
	if err := db.db.QueryRow(`SELECT status FROM jobs WHERE id=?`, id).Scan(&s); err != nil {
		t.Fatalf("job status: %v", err)
	}
	return s
}

// ─── atomicity of FinishJob ─────────────────────────────────────────────────

func TestFinishJob_CommitsStatusArtifactAndEventTogether(t *testing.T) {
	db := newScheduleStatusDB(t)
	seedSchedule(t, db, "sch")
	job := testJob("j1", "sync-tours")
	job.ScheduleID = "sch"
	if err := db.InsertJob(job); err != nil {
		t.Fatalf("InsertJob: %v", err)
	}
	defs := eventDefs(t, EventSinkDef{Name: "s", Type: "log", Types: []string{EventJobFinished}})
	r := newEventRouter(defs)
	art := &ArtifactMeta{Path: "/out/x.tdtp.xml", SHA256: "abc", Size: 42}

	rows := r.build(EventJobFinished, job, string(JobDone), art, "", time.Now())
	if err := db.FinishJob("j1", JobDone, "log text", "", art, rows); err != nil {
		t.Fatalf("FinishJob: %v", err)
	}

	got, err := db.GetJob("j1")
	if err != nil || got == nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != JobDone || got.ArtifactSHA256 != "abc" || got.ArtifactSize != 42 {
		t.Fatalf("job not finished with artifact: status=%s sha=%s size=%d", got.Status, got.ArtifactSHA256, got.ArtifactSize)
	}
	if ev := readOutbox(t, db); len(ev) != 1 || ev[0].Status != "pending" || ev[0].Type != EventJobFinished {
		t.Fatalf("outbox = %+v, want one pending job.finished", ev)
	}
	var lastStatus string
	if err := db.db.QueryRow(`SELECT last_status FROM schedules WHERE id='sch'`).Scan(&lastStatus); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if lastStatus != string(JobDone) {
		t.Fatalf("schedule last_status = %q, want done", lastStatus)
	}
}

// A fault after the job update and the schedule update, but before the outbox
// insert, must leave NOTHING committed: not the status, not the schedule.
// A job that shows done with no event announcing it is the failure this
// transaction exists to prevent.
func TestFinishJob_RollsBackWhenOutboxInsertFails(t *testing.T) {
	db := newScheduleStatusDB(t)
	seedSchedule(t, db, "sch")
	job := testJob("j2", "sync-tours")
	job.ScheduleID = "sch"
	if err := db.InsertJob(job); err != nil {
		t.Fatalf("InsertJob: %v", err)
	}
	if _, err := db.db.Exec(`DROP TABLE outbox`); err != nil {
		t.Fatalf("drop outbox: %v", err)
	}

	rows := []OutboxEvent{{ID: "e1", JobID: "j2", Sink: "s", Type: EventJobFinished, Payload: []byte(`{}`), CreatedAt: time.Now()}}
	if err := db.FinishJob("j2", JobFailed, "", "boom", nil, rows); err == nil {
		t.Fatal("FinishJob succeeded with no outbox table")
	}

	if s := jobStatusOf(t, db, "j2"); s != string(JobRunning) {
		t.Fatalf("job status = %q after rolled-back finish, want running", s)
	}
	var lastStatus string
	if err := db.db.QueryRow(`SELECT last_status FROM schedules WHERE id='sch'`).Scan(&lastStatus); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if lastStatus != "running" {
		t.Fatalf("schedule last_status = %q after rolled-back finish, want running", lastStatus)
	}
}

// ─── delivery ────────────────────────────────────────────────────────────────

// A crash after commit, before delivery, must not lose the event: the row is
// still pending in the file, and a new process delivers it.
func TestDispatcher_DeliversPendingEventAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orch.db")
	db, err := OpenOrchestratorDB(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	job := testJob("j3", "sync-tours")
	if err := db.InsertJob(job); err != nil {
		t.Fatalf("InsertJob: %v", err)
	}
	r := newEventRouter(eventDefs(t, EventSinkDef{Name: "s", Type: "log"}))
	rows := r.build(EventJobFinished, job, string(JobDone), nil, "", time.Now())
	if err := db.FinishJob("j3", JobDone, "", "", nil, rows); err != nil {
		t.Fatalf("FinishJob: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db2, err := OpenOrchestratorDB(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	sink := &fakeSink{}
	d := newTestDispatcher(db2, map[string]EventSink{"s": sink}, 3)

	if n, err := d.RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("RunOnce = %d, %v; want 1, nil", n, err)
	}
	if got := sink.sent(); len(got) != 1 || got[0] != EventJobFinished+"|j3" {
		t.Fatalf("sent = %v", got)
	}
	if ev := readOutbox(t, db2); ev[0].Status != "sent" {
		t.Fatalf("row status = %s, want sent", ev[0].Status)
	}
}

func TestDispatcher_RetriesThenMarksDead(t *testing.T) {
	db := newScheduleStatusDB(t)
	if err := db.AddOutbox([]OutboxEvent{{ID: "e", JobID: "j", Sink: "s", Type: EventJobFinished, Payload: []byte(`{}`), CreatedAt: time.Now()}}); err != nil {
		t.Fatalf("AddOutbox: %v", err)
	}
	fail := errors.New("connection reset by peer, host db.internal")
	sink := &fakeSink{errs: []error{fail, fail, fail, fail}}
	d := newTestDispatcher(db, map[string]EventSink{"s": sink}, 3)

	for i := 0; i < 3; i++ {
		if _, err := d.RunOnce(context.Background()); err != nil {
			t.Fatalf("pass %d: %v", i+1, err)
		}
	}

	ev := readOutbox(t, db)
	if ev[0].Status != "dead" || ev[0].Attempts != 3 {
		t.Fatalf("after 3 failures: status=%s attempts=%d, want dead/3", ev[0].Status, ev[0].Attempts)
	}
	// The error text names a host. It must not reach the outbox; only the class does.
	if ev[0].LastError == nil || *ev[0].LastError != "other" {
		t.Fatalf("last_error = %v, want class \"other\"", ev[0].LastError)
	}
	if strings.Contains(*ev[0].LastError, "db.internal") {
		t.Fatal("raw error text leaked into the outbox")
	}
	if n := len(sink.sent()); n != 3 {
		t.Fatalf("sink called %d times, want 3", n)
	}
}

func TestDispatcher_PermanentFailureIsDeadAtOnce(t *testing.T) {
	db := newScheduleStatusDB(t)
	if err := db.AddOutbox([]OutboxEvent{{ID: "e", JobID: "j", Sink: "s", Type: EventJobFinished, Payload: []byte(`{}`), CreatedAt: time.Now()}}); err != nil {
		t.Fatalf("AddOutbox: %v", err)
	}
	sink := &fakeSink{errs: []error{&SinkError{Class: "auth", Permanent: true}}}
	d := newTestDispatcher(db, map[string]EventSink{"s": sink}, 10)

	if _, err := d.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	ev := readOutbox(t, db)
	if ev[0].Status != "dead" || ev[0].Attempts != 1 || ev[0].LastError == nil || *ev[0].LastError != "auth" {
		t.Fatalf("permanent failure: %+v, want dead after one attempt with class auth", ev[0])
	}
}

// A row whose sink was removed from the config is dead, not pending forever.
func TestDispatcher_MissingSinkIsDeadNotPending(t *testing.T) {
	db := newScheduleStatusDB(t)
	if err := db.AddOutbox([]OutboxEvent{{ID: "e", JobID: "j", Sink: "gone", Type: EventJobFinished, Payload: []byte(`{}`), CreatedAt: time.Now()}}); err != nil {
		t.Fatalf("AddOutbox: %v", err)
	}
	d := newTestDispatcher(db, map[string]EventSink{}, 10)
	if _, err := d.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if ev := readOutbox(t, db); ev[0].Status != "dead" || ev[0].LastError == nil || *ev[0].LastError != "no_sink" {
		t.Fatalf("row = %+v, want dead with no_sink", ev[0])
	}
}

// job.started must reach the sink before job.finished, even when its first
// attempt fails. The finished row waits for it rather than overtaking it.
func TestDispatcher_JobFinishedNotDeliveredBeforeStarted(t *testing.T) {
	db := newScheduleStatusDB(t)
	job := testJob("j4", "sync-tours")
	started := []OutboxEvent{{ID: "s1", JobID: "j4", Sink: "s", Type: EventJobStarted, Payload: []byte(`{}`), CreatedAt: time.Now()}}
	if err := db.AddOutbox(started); err != nil {
		t.Fatalf("AddOutbox started: %v", err)
	}
	r := newEventRouter(eventDefs(t, EventSinkDef{Name: "s", Type: "log", Types: []string{EventJobStarted, EventJobFinished}}))
	finished := r.build(EventJobFinished, job, string(JobDone), nil, "", time.Now())
	if err := db.FinishJob("j4", JobDone, "", "", nil, finished); err != nil {
		t.Fatalf("FinishJob: %v", err)
	}

	sink := &fakeSink{errs: []error{errors.New("transient")}}
	d := newTestDispatcher(db, map[string]EventSink{"s": sink}, 10)

	// Pass 1: started fails; finished must not be attempted.
	if _, err := d.RunOnce(context.Background()); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	for _, c := range sink.sent() {
		if strings.HasPrefix(c, EventJobFinished) {
			t.Fatalf("job.finished attempted while job.started still pending: %v", sink.sent())
		}
	}
	// Pass 2 and 3 deliver both, in order.
	for i := 0; i < 2; i++ {
		if _, err := d.RunOnce(context.Background()); err != nil {
			t.Fatalf("pass %d: %v", i+2, err)
		}
	}
	got := sink.sent()
	if len(got) < 3 || got[len(got)-2] != EventJobStarted+"|j4" || got[len(got)-1] != EventJobFinished+"|j4" {
		t.Fatalf("delivery order = %v, want started then finished for j4", got)
	}
}

// ─── executor integration ────────────────────────────────────────────────────

// A real Submit writes started then finished, the outcome is recorded, and
// the job log — which here carries a DSN with a password — stays out of every
// event.
func TestExecutor_EmitsStartedThenFinishedWithoutLog(t *testing.T) {
	run := func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		return []byte("connect postgres://tdtp:secretpw@db.internal/x failed"), errors.New("exit status 1")
	}
	e, db := newTestExecutor(t, run)
	e.events = newEventRouter(eventDefs(t, EventSinkDef{Name: "s", Type: "log", Types: []string{EventJobStarted, EventJobFinished}}))

	s := scenarioFromYAML("sync-tours", "orchestrator:\n  name: sync-tours\n")
	job, err := e.Submit(s, map[string]string{"period": "2026-06"}, "", "")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitJob(t, e)

	if st := jobStatusOf(t, db, job.ID); st != string(JobFailed) {
		t.Fatalf("job status = %s, want failed", st)
	}
	ev := readOutbox(t, db)
	if len(ev) != 2 {
		t.Fatalf("outbox has %d rows, want 2 (started, finished): %+v", len(ev), ev)
	}
	if ev[0].Type != EventJobStarted || ev[1].Type != EventJobFinished {
		t.Fatalf("order = %s, %s; want started then finished", ev[0].Type, ev[1].Type)
	}
	for _, r := range ev {
		if strings.Contains(r.Payload, "secretpw") || strings.Contains(r.Payload, "db.internal") {
			t.Fatalf("event payload carries the job log: %s", r.Payload)
		}
	}

	var fin map[string]any
	if err := json.Unmarshal([]byte(ev[1].Payload), &fin); err != nil {
		t.Fatalf("finished payload is not JSON: %v", err)
	}
	if _, has := fin["log"]; has {
		t.Fatal("finished payload has a log field")
	}
	if fin["error"] != "exit status 1" {
		t.Fatalf("error = %v, want the process error only", fin["error"])
	}
	if p, has := fin["params"]; has && p != nil {
		t.Fatalf("params present without include_params: %v", p)
	}
}

// ─── routing ─────────────────────────────────────────────────────────────────

func TestEventRouter_FiltersByTypeScenarioAndStatus(t *testing.T) {
	defs := eventDefs(t,
		EventSinkDef{Name: "failures", Type: "log", Types: []string{EventJobFinished}, Scenarios: []string{"sync-*"}, Statuses: []string{"failed"}},
		EventSinkDef{Name: "starts", Type: "log", Types: []string{EventJobStarted}},
		EventSinkDef{Name: "default", Type: "log"},
	)
	r := newEventRouter(defs)
	now := time.Now()

	cases := []struct {
		name     string
		typ      string
		scenario string
		status   string
		want     []string
	}{
		{"failed sync", EventJobFinished, "sync-tours", "failed", []string{"failures", "default"}},
		{"done sync", EventJobFinished, "sync-tours", "done", []string{"default"}},
		{"failed other scenario", EventJobFinished, "report", "failed", []string{"default"}},
		{"started", EventJobStarted, "report", "running", []string{"starts"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := r.build(c.typ, testJob("j", c.scenario), c.status, nil, "", now)
			var got []string
			for _, row := range rows {
				got = append(got, row.Sink)
			}
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("sinks = %v, want %v", got, c.want)
			}
		})
	}
}

func TestEventRouter_NilRouterProducesNothing(t *testing.T) {
	var r *eventRouter
	if rows := r.build(EventJobFinished, testJob("j", "s"), "done", nil, "", time.Now()); rows != nil {
		t.Fatalf("nil router produced %d rows", len(rows))
	}
}

func TestEventRouter_IncludeParamsIsPerSink(t *testing.T) {
	defs := eventDefs(t,
		EventSinkDef{Name: "plain", Type: "log"},
		EventSinkDef{Name: "params", Type: "log", IncludeParams: true},
	)
	rows := newEventRouter(defs).build(EventJobFinished, testJob("j", "s"), "done", nil, "", time.Now())
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if strings.Contains(string(rows[0].Payload), "2026-06") {
		t.Fatal("plain sink received params")
	}
	if !strings.Contains(string(rows[1].Payload), "2026-06") {
		t.Fatal("params sink did not receive params")
	}
}

// ─── configuration ───────────────────────────────────────────────────────────

func TestPrepareSinks_Validation(t *testing.T) {
	cases := []struct {
		name string
		def  EventSinkDef
		want string // substring of the error; "" = valid
	}{
		{"valid defaults", EventSinkDef{Name: "a", Type: "log"}, ""},
		{"no name", EventSinkDef{Type: "log"}, "name is required"},
		{"unknown sink type", EventSinkDef{Name: "a", Type: "kafka"}, "not supported"},
		{"unknown event type", EventSinkDef{Name: "a", Type: "log", Types: []string{"job.exploded"}}, "unknown event type"},
		{"unknown status", EventSinkDef{Name: "a", Type: "log", Statuses: []string{"ok"}}, "unknown status"},
		{"bad glob", EventSinkDef{Name: "a", Type: "log", Scenarios: []string{"sync-["}}, "scenario pattern"},
		{"zero is default", EventSinkDef{Name: "a", Type: "log", MaxAttempts: 0}, ""},
		{"negative attempts", EventSinkDef{Name: "a", Type: "log", MaxAttempts: -1}, "max_attempts"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defs := []EventSinkDef{c.def}
			err := prepareSinks(defs)
			if c.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if defs[0].MaxAttempts != defaultMaxAttempts || len(defs[0].Types) != 1 || defs[0].Types[0] != EventJobFinished {
					t.Fatalf("defaults not applied: %+v", defs[0])
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want substring %q", err, c.want)
			}
		})
	}

	dup := []EventSinkDef{{Name: "a", Type: "log"}, {Name: "a", Type: "log"}}
	if err := prepareSinks(dup); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate names: %v", err)
	}
}

// A misspelled key must fail loudly. "scenario:" read as nothing would make
// the sink match every scenario, which is the direction that discloses more.
func TestLoadEvents_RejectsUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.yaml")
	body := "sinks:\n  - name: a\n    type: log\n    scenario: sync-tours\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadEvents(path)
	if err == nil || !strings.Contains(err.Error(), "scenario") {
		t.Fatalf("LoadEvents accepted a misspelled key: %v", err)
	}
}

func TestLoadEvents_EmptyPathMeansNoSinks(t *testing.T) {
	defs, err := LoadEvents("")
	if err != nil || defs != nil {
		t.Fatalf("LoadEvents(\"\") = %v, %v; want nil, nil", defs, err)
	}
	stop, err := StartEventDispatcher(nil, nil)
	if err != nil {
		t.Fatalf("StartEventDispatcher with no sinks: %v", err)
	}
	stop()
}

func TestEventBackoff_DoublesAndCaps(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i, w := range want {
		if got := eventBackoff(i + 1); got != w {
			t.Fatalf("eventBackoff(%d) = %v, want %v", i+1, got, w)
		}
	}
	if got := eventBackoff(20); got != maxEventBackoff {
		t.Fatalf("eventBackoff(20) = %v, want cap %v", got, maxEventBackoff)
	}
}

// A delivered row whose "sent" state cannot be recorded does not stop the pass:
// the other rows still go out, the failure comes back in the error, and the row
// stays pending, so it is sent again rather than silently treated as done.
func TestDispatcher_RecordFailureIsReturnedNotSwallowed(t *testing.T) {
	db := newScheduleStatusDB(t)
	if err := db.AddOutbox([]OutboxEvent{
		{ID: "rec-1", JobID: "ja", Sink: "s", Type: EventJobFinished, Payload: []byte(`{}`), CreatedAt: time.Now()},
		{ID: "rec-2", JobID: "jb", Sink: "s", Type: EventJobFinished, Payload: []byte(`{}`), CreatedAt: time.Now()},
	}); err != nil {
		t.Fatalf("AddOutbox: %v", err)
	}
	if _, err := db.db.Exec(`CREATE TRIGGER refuse_sent BEFORE UPDATE OF status ON outbox
		WHEN NEW.status = 'sent' BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("trigger: %v", err)
	}

	sink := &fakeSink{}
	d := newTestDispatcher(db, map[string]EventSink{"s": sink}, 5)
	n, err := d.RunOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rec-1") {
		t.Fatalf("record failure swallowed: err = %v", err)
	}
	if got := sink.sent(); len(got) != 2 {
		t.Fatalf("pass stopped early: sends = %v, want both rows attempted", got)
	}
	if n != 0 {
		t.Fatalf("delivered = %d, want 0: no state was recorded", n)
	}
	for _, row := range readOutbox(t, db) {
		if row.Status != "pending" {
			t.Fatalf("row %s is %s, want pending so it is sent again", row.ID, row.Status)
		}
	}
}
