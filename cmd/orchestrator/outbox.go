package main

// outbox.go — the database side of the job-event outbox (see events.go for the
// events themselves and the dispatcher that delivers them).
//
// Timestamps are stored as fixed-width UTC text, so the due check is a plain
// string comparison: RFC3339Nano trims trailing zeros, and "…05.1Z" sorts
// after "…05.12Z" as text, which is the wrong way round.

import (
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
