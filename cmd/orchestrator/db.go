package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const orchSchema = `
CREATE TABLE IF NOT EXISTS schedules (
	id           TEXT PRIMARY KEY,
	scenario     TEXT NOT NULL,
	cron_expr    TEXT NOT NULL,
	params       TEXT NOT NULL DEFAULT '{}',   -- JSON object
	enabled      INTEGER NOT NULL DEFAULT 1,
	created_at   DATETIME NOT NULL,
	modified_at  DATETIME NOT NULL,
	last_run_at  DATETIME,
	last_status  TEXT,                         -- done|failed|running
	next_run_at  DATETIME
);

CREATE TABLE IF NOT EXISTS jobs (
	id           TEXT PRIMARY KEY,
	schedule_id  TEXT REFERENCES schedules(id), -- NULL = manual run
	scenario     TEXT NOT NULL,
	params       TEXT NOT NULL DEFAULT '{}',
	status       TEXT NOT NULL DEFAULT 'pending',
	started_at   DATETIME NOT NULL,
	finished_at  DATETIME,
	log          TEXT,
	error        TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_schedule ON jobs(schedule_id);
CREATE INDEX IF NOT EXISTS idx_jobs_status   ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_started  ON jobs(started_at DESC);

CREATE TABLE IF NOT EXISTS tokens (
	id          TEXT PRIMARY KEY,
	token_hash  TEXT NOT NULL UNIQUE,           -- SHA-256(raw token), hex
	name        TEXT NOT NULL,                  -- human label
	role        TEXT NOT NULL,                  -- admin|activator|consumer
	scenarios   TEXT NOT NULL DEFAULT '[]',     -- JSON array; empty = all scenarios
	created_at  DATETIME NOT NULL,
	last_used_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_tokens_hash ON tokens(token_hash);
`

// OrchestratorDB wraps the orchestrator SQLite database.
type OrchestratorDB struct {
	db *sql.DB
}

// OpenOrchestratorDB opens (or creates) the orchestrator database.
func OpenOrchestratorDB(path string) (*OrchestratorDB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("orchestrator db: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(orchSchema); err != nil {
		return nil, fmt.Errorf("orchestrator db: schema: %w", err)
	}
	return &OrchestratorDB{db: db}, nil
}

func (d *OrchestratorDB) Close() error { return d.db.Close() }

// ─── Schedule operations ──────────────────────────────────────────────────────

// ScheduleRecord is a row in the schedules table.
type ScheduleRecord struct {
	ID         string
	Scenario   string
	CronExpr   string
	Params     map[string]string
	Enabled    bool
	CreatedAt  time.Time
	ModifiedAt time.Time
	LastRunAt  *time.Time
	LastStatus string
	NextRunAt  *time.Time
}

// UpsertSchedule inserts or replaces a schedule record.
func (d *OrchestratorDB) UpsertSchedule(r *ScheduleRecord) error {
	params, err := json.Marshal(r.Params)
	if err != nil {
		return err
	}
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.db.Exec(`
		INSERT INTO schedules(id, scenario, cron_expr, params, enabled, created_at, modified_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			scenario=excluded.scenario,
			cron_expr=excluded.cron_expr,
			params=excluded.params,
			enabled=excluded.enabled,
			modified_at=excluded.modified_at`,
		r.ID, r.Scenario, r.CronExpr, string(params), enabled, now, now,
	)
	return err
}

// ListSchedules returns all schedule records.
func (d *OrchestratorDB) ListSchedules() ([]*ScheduleRecord, error) {
	rows, err := d.db.Query(`
		SELECT id, scenario, cron_expr, params, enabled,
		       created_at, modified_at, last_run_at, last_status, next_run_at
		FROM schedules ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ScheduleRecord
	for rows.Next() {
		r, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetSchedule returns one schedule by ID.
func (d *OrchestratorDB) GetSchedule(id string) (*ScheduleRecord, error) {
	row := d.db.QueryRow(`
		SELECT id, scenario, cron_expr, params, enabled,
		       created_at, modified_at, last_run_at, last_status, next_run_at
		FROM schedules WHERE id=?`, id)
	r, err := scanSchedule(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// SetEnabled pauses or resumes a schedule.
func (d *OrchestratorDB) SetEnabled(id string, enabled bool) error {
	e := 0
	if enabled {
		e = 1
	}
	_, err := d.db.Exec(`UPDATE schedules SET enabled=?, modified_at=? WHERE id=?`,
		e, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// DeleteSchedule removes a schedule (does not affect existing jobs).
func (d *OrchestratorDB) DeleteSchedule(id string) error {
	_, err := d.db.Exec(`DELETE FROM schedules WHERE id=?`, id)
	return err
}

// TouchScheduleRun updates last_run_at, last_status, next_run_at after a run.
func (d *OrchestratorDB) TouchScheduleRun(id, status string, nextRun *time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var nextRunStr interface{}
	if nextRun != nil {
		nextRunStr = nextRun.UTC().Format(time.RFC3339)
	}
	_, err := d.db.Exec(
		`UPDATE schedules SET last_run_at=?, last_status=?, next_run_at=? WHERE id=?`,
		now, status, nextRunStr, id)
	return err
}

// ─── Job operations ───────────────────────────────────────────────────────────

// InsertJob persists a new job record.
func (d *OrchestratorDB) InsertJob(j *Job) error {
	params, err := json.Marshal(j.Params)
	if err != nil {
		return err
	}
	var schedID interface{}
	if j.ScheduleID != "" {
		schedID = j.ScheduleID
	}
	_, err = d.db.Exec(`
		INSERT INTO jobs(id, schedule_id, scenario, params, status, started_at)
		VALUES(?,?,?,?,?,?)`,
		j.ID, schedID, j.Scenario, string(params),
		string(j.Status), j.StartedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// UpdateJobDone updates status, finished_at, log, error when a job completes.
func (d *OrchestratorDB) UpdateJobDone(id string, status JobStatus, log, errMsg string) error {
	_, err := d.db.Exec(`
		UPDATE jobs SET status=?, finished_at=?, log=?, error=? WHERE id=?`,
		string(status), time.Now().UTC().Format(time.RFC3339), log, errMsg, id)
	return err
}

// UpdateJobStatus updates just the status field (e.g. pending→running).
func (d *OrchestratorDB) UpdateJobStatus(id string, status JobStatus) error {
	_, err := d.db.Exec(`UPDATE jobs SET status=? WHERE id=?`, string(status), id)
	return err
}

// CountActiveJobs returns the number of jobs currently pending or running.
// Used to enforce the licensed concurrent-pipeline limit.
func (d *OrchestratorDB) CountActiveJobs() (int, error) {
	var n int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM jobs WHERE status IN ('pending','running')`).Scan(&n)
	return n, err
}

// GetJob returns one job by ID.
func (d *OrchestratorDB) GetJob(id string) (*Job, error) {
	row := d.db.QueryRow(`
		SELECT id, schedule_id, scenario, params, status, started_at, finished_at, log, error
		FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

// ListJobs returns the N most recent jobs (all scenarios).
func (d *OrchestratorDB) ListJobs(limit int) ([]*Job, error) {
	rows, err := d.db.Query(`
		SELECT id, schedule_id, scenario, params, status, started_at, finished_at, log, error
		FROM jobs ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJobRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ─── Job listing by scenario (consumer view) ──────────────────────────────────

// ListJobsByScenario returns the N most recent jobs for one scenario.
func (d *OrchestratorDB) ListJobsByScenario(scenario string, limit int) ([]*Job, error) {
	rows, err := d.db.Query(`
		SELECT id, schedule_id, scenario, params, status, started_at, finished_at, log, error
		FROM jobs WHERE scenario=? ORDER BY started_at DESC LIMIT ?`, scenario, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJobRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ─── Token operations ─────────────────────────────────────────────────────────

// TokenRecord is a row in the tokens table (never carries the raw token).
type TokenRecord struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Role       string    `json:"role"`
	Scenarios  []string  `json:"scenarios"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// InsertToken persists a token by its hash. The raw token is never stored.
func (d *OrchestratorDB) InsertToken(id, tokenHash, name, role string, scenarios []string) error {
	sc, err := json.Marshal(scenarios)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(
		`INSERT INTO tokens(id, token_hash, name, role, scenarios, created_at)
		 VALUES(?,?,?,?,?,?)`,
		id, tokenHash, name, role, string(sc), time.Now().UTC().Format(time.RFC3339))
	return err
}

// GetTokenByHash resolves a presented token hash to its record.
// Returns (nil, nil) when not found.
func (d *OrchestratorDB) GetTokenByHash(tokenHash string) (*TokenRecord, error) {
	row := d.db.QueryRow(
		`SELECT id, name, role, scenarios, created_at, last_used_at
		 FROM tokens WHERE token_hash=?`, tokenHash)
	var r TokenRecord
	var scJSON, createdAt string
	var lastUsed sql.NullString
	if err := row.Scan(&r.ID, &r.Name, &r.Role, &scJSON, &createdAt, &lastUsed); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(scJSON), &r.Scenarios)
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if lastUsed.Valid {
		if t, err := time.Parse(time.RFC3339, lastUsed.String); err == nil {
			r.LastUsedAt = &t
		}
	}
	return &r, nil
}

// TouchToken updates last_used_at for a token.
func (d *OrchestratorDB) TouchToken(id string) error {
	_, err := d.db.Exec(`UPDATE tokens SET last_used_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// ListTokens returns all token records (never the raw tokens).
func (d *OrchestratorDB) ListTokens() ([]*TokenRecord, error) {
	rows, err := d.db.Query(
		`SELECT id, name, role, scenarios, created_at, last_used_at FROM tokens ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TokenRecord
	for rows.Next() {
		var r TokenRecord
		var scJSON, createdAt string
		var lastUsed sql.NullString
		if err := rows.Scan(&r.ID, &r.Name, &r.Role, &scJSON, &createdAt, &lastUsed); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(scJSON), &r.Scenarios)
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if lastUsed.Valid {
			if t, err := time.Parse(time.RFC3339, lastUsed.String); err == nil {
				r.LastUsedAt = &t
			}
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// DeleteToken removes a token by ID.
func (d *OrchestratorDB) DeleteToken(id string) error {
	_, err := d.db.Exec(`DELETE FROM tokens WHERE id=?`, id)
	return err
}

// CountTokens returns the number of tokens (for bootstrap detection).
func (d *OrchestratorDB) CountTokens() (int, error) {
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM tokens`).Scan(&n)
	return n, err
}

// ─── Scan helpers ─────────────────────────────────────────────────────────────

type scannable interface {
	Scan(dest ...any) error
}

func scanSchedule(row scannable) (*ScheduleRecord, error) {
	var r ScheduleRecord
	var paramsJSON string
	var enabled int
	var createdAt, modifiedAt string
	var lastRunAt, nextRunAt, lastStatus sql.NullString

	err := row.Scan(
		&r.ID, &r.Scenario, &r.CronExpr, &paramsJSON, &enabled,
		&createdAt, &modifiedAt, &lastRunAt, &lastStatus, &nextRunAt,
	)
	if err != nil {
		return nil, err
	}
	r.Enabled = enabled == 1
	r.LastStatus = lastStatus.String
	if err := json.Unmarshal([]byte(paramsJSON), &r.Params); err != nil {
		r.Params = map[string]string{}
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		r.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, modifiedAt); err == nil {
		r.ModifiedAt = t
	}
	if lastRunAt.Valid {
		if t, err := time.Parse(time.RFC3339, lastRunAt.String); err == nil {
			r.LastRunAt = &t
		}
	}
	if nextRunAt.Valid {
		if t, err := time.Parse(time.RFC3339, nextRunAt.String); err == nil {
			r.NextRunAt = &t
		}
	}
	return &r, nil
}

func scanJob(row scannable) (*Job, error) { return scanJobRow(row) }

func scanJobRow(row scannable) (*Job, error) {
	var j Job
	var paramsJSON, startedAt string
	var schedID, finishedAt, log, errMsg sql.NullString
	var status string

	err := row.Scan(
		&j.ID, &schedID, &j.Scenario, &paramsJSON, &status,
		&startedAt, &finishedAt, &log, &errMsg,
	)
	if err != nil {
		return nil, err
	}
	j.ScheduleID = schedID.String
	j.Status = JobStatus(status)
	j.Log = log.String
	j.Error = errMsg.String
	if err := json.Unmarshal([]byte(paramsJSON), &j.Params); err != nil {
		j.Params = map[string]string{}
	}
	if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
		j.StartedAt = t
	}
	if finishedAt.Valid {
		if t, err := time.Parse(time.RFC3339, finishedAt.String); err == nil {
			j.FinishedAt = &t
		}
	}
	return &j, nil
}
