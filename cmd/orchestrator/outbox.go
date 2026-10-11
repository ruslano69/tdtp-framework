package main

// outbox.go — the database side of the job-event outbox (see events.go for the
// events themselves and the dispatcher that delivers them).
//
// Timestamps are stored as fixed-width UTC text, so the due check is a plain
// string comparison: RFC3339Nano trims trailing zeros, and "…05.1Z" sorts
// after "…05.12Z" as text, which is the wrong way round.

import (
	"database/sql"
	"errors"
	"time"
)

const outboxTimeLayout = "2006-01-02T15:04:05.000Z"

func outboxTS(t time.Time) string { return t.UTC().Format(outboxTimeLayout) }

// OutboxEvent is one row waiting for delivery to one sink.
type OutboxEvent struct {
	ID        string // event_id: unique per row, stable across retries
	JobID     string
	Sink      string
	Type      string
	Payload   []byte
	CreatedAt time.Time
}

// insertOutboxTx adds rows inside an open transaction.
func insertOutboxTx(ex execer, events []OutboxEvent) error {
	for _, e := range events {
		ts := outboxTS(e.CreatedAt)
		if _, err := ex.Exec(`
			INSERT INTO outbox(id, job_id, sink, type, payload, status, attempts, next_attempt_at, created_at)
			VALUES(?,?,?,?,?,'pending',0,?,?)`,
			e.ID, e.JobID, e.Sink, e.Type, string(e.Payload), ts, ts); err != nil {
			return err
		}
	}
	return nil
}

// AddOutbox writes events on their own, for events that have no job state to
// change with them (job.started).
func (d *OrchestratorDB) AddOutbox(events []OutboxEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertOutboxTx(tx, events); err != nil {
		return err
	}
	return tx.Commit()
}

// outboxDue is a pending row the dispatcher may attempt now.
type outboxDue struct {
	ID       string
	Sink     string
	Type     string
	JobID    string
	Payload  []byte
	Attempts int
}

// dueOutbox returns up to limit pending rows whose next attempt has come.
//
// A row is held back while an EARLIER pending row of the same job and sink is
// still waiting: job.finished must not overtake a job.started that is being
// retried. The guarantee is per job and per sink; across jobs there is no
// order. A dead row does not hold anything back — the dispatcher would
// otherwise wait forever on an event that will never be delivered.
func (d *OrchestratorDB) dueOutbox(now time.Time, limit int) ([]outboxDue, error) {
	rows, err := d.db.Query(`
		SELECT o.id, o.sink, o.type, o.job_id, o.payload, o.attempts
		  FROM outbox o
		 WHERE o.status = 'pending'
		   AND o.next_attempt_at <= ?
		   AND NOT EXISTS (
		         SELECT 1 FROM outbox e
		          WHERE e.job_id = o.job_id
		            AND e.sink = o.sink
		            AND e.status = 'pending'
		            AND e.rowid < o.rowid)
		 ORDER BY o.rowid
		 LIMIT ?`, outboxTS(now), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []outboxDue
	for rows.Next() {
		var r outboxDue
		var payload string
		if err := rows.Scan(&r.ID, &r.Sink, &r.Type, &r.JobID, &payload, &r.Attempts); err != nil {
			return nil, err
		}
		r.Payload = []byte(payload)
		out = append(out, r)
	}
	return out, rows.Err()
}

// markOutboxSent records a delivered row.
func (d *OrchestratorDB) markOutboxSent(id string, now time.Time) error {
	_, err := d.db.Exec(
		`UPDATE outbox SET status='sent', sent_at=? WHERE id=?`, outboxTS(now), id)
	return err
}

// markOutboxRetry reschedules a row after a transient failure.
func (d *OrchestratorDB) markOutboxRetry(id string, attempts int, next time.Time, class string) error {
	_, err := d.db.Exec(
		`UPDATE outbox SET attempts=?, next_attempt_at=?, last_error=? WHERE id=?`,
		attempts, outboxTS(next), class, id)
	return err
}

// markOutboxDead gives up on a row. It stays in the table: a dead event is
// visible to an operator and can be retried, it is not silently dropped.
func (d *OrchestratorDB) markOutboxDead(id string, attempts int, class string) error {
	_, err := d.db.Exec(
		`UPDATE outbox SET status='dead', attempts=?, last_error=? WHERE id=?`,
		attempts, class, id)
	return err
}

// PruneSentOutbox deletes delivered rows older than before. Dead and pending
// rows are never pruned here.
func (d *OrchestratorDB) PruneSentOutbox(before time.Time) (int64, error) {
	res, err := d.db.Exec(
		`DELETE FROM outbox WHERE status='sent' AND sent_at < ?`, outboxTS(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// outboxStatusCounts counts the rows that need attention: pending and dead.
// The status filter lets the index answer without reading delivered rows.
func (d *OrchestratorDB) outboxStatusCounts() (map[string]int, error) {
	rows, err := d.db.Query(`
		SELECT status, COUNT(*) FROM outbox
		 WHERE status IN ('pending', 'dead')
		 GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		counts[status] = n
	}
	return counts, rows.Err()
}

// outboxSinkTotals is one sink's row counts, for the admin view.
type outboxSinkTotals struct {
	Pending    int
	Sent       int
	Dead       int
	LastSentAt *string
}

// outboxTotalsBySink counts every row per sink and status. It reads delivered
// rows as well, so it is for on-demand requests, not for the dispatcher loop.
func (d *OrchestratorDB) outboxTotalsBySink() (map[string]*outboxSinkTotals, error) {
	rows, err := d.db.Query(`
		SELECT sink, status, COUNT(*), MAX(sent_at)
		  FROM outbox
		 GROUP BY sink, status`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]*outboxSinkTotals{}
	for rows.Next() {
		var sink, status string
		var n int
		var lastSent *string
		if err := rows.Scan(&sink, &status, &n, &lastSent); err != nil {
			return nil, err
		}
		t := out[sink]
		if t == nil {
			t = &outboxSinkTotals{}
			out[sink] = t
		}
		switch status {
		case "pending":
			t.Pending = n
		case "sent":
			t.Sent = n
			t.LastSentAt = lastSent
		case "dead":
			t.Dead = n
		}
	}
	return out, rows.Err()
}

// outboxView is one row as an operator sees it. The payload is left out: it
// is the event as a receiver got it, and it is not needed to find a problem.
type outboxView struct {
	ID            string  `json:"id"`
	JobID         string  `json:"job_id"`
	Scenario      string  `json:"scenario"`
	Sink          string  `json:"sink"`
	Type          string  `json:"type"`
	Status        string  `json:"status"`
	Attempts      int     `json:"attempts"`
	LastError     *string `json:"last_error"`
	CreatedAt     string  `json:"created_at"`
	NextAttemptAt string  `json:"next_attempt_at"`
	SentAt        *string `json:"sent_at"`
}

// listOutbox returns the newest rows first, optionally narrowed to one status
// and/or one sink. Callers validate status and limit.
func (d *OrchestratorDB) listOutbox(status, sink string, limit int) ([]outboxView, error) {
	q := `
		SELECT o.id, o.job_id, COALESCE(j.scenario, ''), o.sink, o.type, o.status,
		       o.attempts, o.last_error, o.created_at, o.next_attempt_at, o.sent_at
		  FROM outbox o
		  LEFT JOIN jobs j ON j.id = o.job_id
		 WHERE 1=1`
	var args []any
	if status != "" {
		q += ` AND o.status = ?`
		args = append(args, status)
	}
	if sink != "" {
		q += ` AND o.sink = ?`
		args = append(args, sink)
	}
	q += ` ORDER BY o.rowid DESC LIMIT ?`
	args = append(args, limit)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []outboxView{}
	for rows.Next() {
		var v outboxView
		if err := rows.Scan(&v.ID, &v.JobID, &v.Scenario, &v.Sink, &v.Type, &v.Status,
			&v.Attempts, &v.LastError, &v.CreatedAt, &v.NextAttemptAt, &v.SentAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// requeueDead moves a dead row back to pending with a fresh attempt budget.
// It reports whether the row exists and whether this call moved it: a row
// that is pending or sent exists, but is not requeued.
func (d *OrchestratorDB) requeueDead(id string, now time.Time) (exists, requeued bool, err error) {
	res, err := d.db.Exec(`
		UPDATE outbox SET status='pending', attempts=0, next_attempt_at=?, last_error=NULL
		 WHERE id=? AND status='dead'`, outboxTS(now), id)
	if err != nil {
		return false, false, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return false, false, err
	} else if n == 1 {
		return true, true, nil
	}
	var status string
	err = d.db.QueryRow(`SELECT status FROM outbox WHERE id=?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, false, nil
}
