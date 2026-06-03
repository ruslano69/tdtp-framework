package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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

-- Project requests: a client proposes a run; an admin tests and approves/rejects.
CREATE TABLE IF NOT EXISTS requests (
	id             TEXT PRIMARY KEY,
	scenario       TEXT NOT NULL,
	params         TEXT NOT NULL DEFAULT '{}',  -- JSON object
	title          TEXT,                        -- optional human label for the project
	submitter_id   TEXT NOT NULL,               -- token id of the submitter ("" in no-auth)
	submitter_name TEXT NOT NULL,
	status         TEXT NOT NULL DEFAULT 'pending', -- pending|approved|rejected
	review_note    TEXT,
	reviewed_by    TEXT,
	job_id         TEXT,                        -- resulting job after approval+execute
	created_at     DATETIME NOT NULL,
	reviewed_at    DATETIME
);

CREATE INDEX IF NOT EXISTS idx_requests_status    ON requests(status);
CREATE INDEX IF NOT EXISTS idx_requests_submitter ON requests(submitter_id);
CREATE INDEX IF NOT EXISTS idx_requests_created   ON requests(created_at DESC);
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
	// Idempotent migrations.
	migrations := []struct {
		col string
		ddl string
	}{
		{"timezone", `ALTER TABLE schedules ADD COLUMN timezone TEXT NOT NULL DEFAULT ''`},
		{"artifact_path", `ALTER TABLE jobs ADD COLUMN artifact_path TEXT NOT NULL DEFAULT ''`},
		{"artifact_sha256", `ALTER TABLE jobs ADD COLUMN artifact_sha256 TEXT NOT NULL DEFAULT ''`},
		{"artifact_size", `ALTER TABLE jobs ADD COLUMN artifact_size INTEGER NOT NULL DEFAULT 0`},
	}
	for _, m := range migrations {
		if _, err := db.Exec(m.ddl); err != nil {
			if !isDuplicateColumnErr(err) {
				return nil, fmt.Errorf("orchestrator db: migrate %s: %w", m.col, err)
			}
		}
	}
	return &OrchestratorDB{db: db}, nil
}

// isDuplicateColumnErr returns true when SQLite rejects an ALTER TABLE ADD COLUMN
// because the column already exists (error message contains "duplicate column name").
func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "duplicate column name")
}

func (d *OrchestratorDB) Close() error { return d.db.Close() }

// ─── Schedule operations ──────────────────────────────────────────────────────

// ScheduleRecord is a row in the schedules table.
type ScheduleRecord struct {
	ID         string
	Scenario   string
	CronExpr   string
	Params     map[string]string
	Timezone   string
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
		INSERT INTO schedules(id, scenario, cron_expr, params, timezone, enabled, created_at, modified_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			scenario=excluded.scenario,
			cron_expr=excluded.cron_expr,
			params=excluded.params,
			timezone=excluded.timezone,
			enabled=excluded.enabled,
			modified_at=excluded.modified_at`,
		r.ID, r.Scenario, r.CronExpr, string(params), r.Timezone, enabled, now, now,
	)
	return err
}

// ListSchedules returns all schedule records.
func (d *OrchestratorDB) ListSchedules() ([]*ScheduleRecord, error) {
	rows, err := d.db.Query(`
		SELECT id, scenario, cron_expr, params, timezone, enabled,
		       created_at, modified_at, last_run_at, last_status, next_run_at
		FROM schedules ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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
		SELECT id, scenario, cron_expr, params, timezone, enabled,
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

// UpdateJobArtifact records the output file path, SHA-256, and size for a completed job.
func (d *OrchestratorDB) UpdateJobArtifact(id, path, sha256 string, size int64) error {
	_, err := d.db.Exec(
		`UPDATE jobs SET artifact_path=?, artifact_sha256=?, artifact_size=? WHERE id=?`,
		path, sha256, size, id)
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
		SELECT id, schedule_id, scenario, params, status, started_at, finished_at, log, error,
		       artifact_path, artifact_sha256, artifact_size
		FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

// ListJobs returns the N most recent jobs (all scenarios).
func (d *OrchestratorDB) ListJobs(limit int) ([]*Job, error) {
	rows, err := d.db.Query(`
		SELECT id, schedule_id, scenario, params, status, started_at, finished_at, log, error,
		       artifact_path, artifact_sha256, artifact_size
		FROM jobs ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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
		SELECT id, schedule_id, scenario, params, status, started_at, finished_at, log, error,
		       artifact_path, artifact_sha256, artifact_size
		FROM jobs WHERE scenario=? ORDER BY started_at DESC LIMIT ?`, scenario, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Role       string     `json:"role"`
	Scenarios  []string   `json:"scenarios"`
	CreatedAt  time.Time  `json:"created_at"`
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
	defer func() { _ = rows.Close() }()
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

// ─── Project request operations ────────────────────────────────────────────────

// RequestStatus is the review state of a project request.
type RequestStatus string

const (
	ReqPending  RequestStatus = "pending"
	ReqApproved RequestStatus = "approved"
	ReqRejected RequestStatus = "rejected"
)

// ProjectRequest is a client-submitted run proposal awaiting admin review.
type ProjectRequest struct {
	ID            string            `json:"id"`
	Scenario      string            `json:"scenario"`
	Params        map[string]string `json:"params"`
	Title         string            `json:"title,omitempty"`
	SubmitterID   string            `json:"submitter_id"`
	SubmitterName string            `json:"submitter_name"`
	Status        RequestStatus     `json:"status"`
	ReviewNote    string            `json:"review_note,omitempty"`
	ReviewedBy    string            `json:"reviewed_by,omitempty"`
	JobID         string            `json:"job_id,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	ReviewedAt    *time.Time        `json:"reviewed_at,omitempty"`
}

// InsertRequest persists a new pending request.
func (d *OrchestratorDB) InsertRequest(r *ProjectRequest) error {
	params, err := json.Marshal(r.Params)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(
		`INSERT INTO requests(id, scenario, params, title, submitter_id, submitter_name,
		                      status, created_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		r.ID, r.Scenario, string(params), r.Title, r.SubmitterID, r.SubmitterName,
		string(ReqPending), time.Now().UTC().Format(time.RFC3339))
	return err
}

// GetRequest returns one request by ID.
func (d *OrchestratorDB) GetRequest(id string) (*ProjectRequest, error) {
	row := d.db.QueryRow(
		`SELECT id, scenario, params, title, submitter_id, submitter_name,
		        status, review_note, reviewed_by, job_id, created_at, reviewed_at
		 FROM requests WHERE id=?`, id)
	return scanRequest(row)
}

// ListRequests returns requests, optionally filtered by status and/or submitter.
// submitterID="" means no submitter filter (admin view). status="" means all statuses.
func (d *OrchestratorDB) ListRequests(status RequestStatus, submitterID string, limit int) ([]*ProjectRequest, error) {
	q := `SELECT id, scenario, params, title, submitter_id, submitter_name,
	             status, review_note, reviewed_by, job_id, created_at, reviewed_at
	      FROM requests WHERE 1=1`
	var args []any
	if status != "" {
		q += " AND status=?"
		args = append(args, string(status))
	}
	if submitterID != "" {
		q += " AND submitter_id=?"
		args = append(args, submitterID)
	}
	q += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*ProjectRequest
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReviewRequest sets the status, reviewer, note and (for approval) the job_id.
func (d *OrchestratorDB) ReviewRequest(id string, status RequestStatus, reviewedBy, note, jobID string) error {
	_, err := d.db.Exec(
		`UPDATE requests SET status=?, reviewed_by=?, review_note=?, job_id=?, reviewed_at=?
		 WHERE id=?`,
		string(status), reviewedBy, note, jobID,
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func scanRequest(row scannable) (*ProjectRequest, error) {
	var r ProjectRequest
	var paramsJSON, createdAt string
	var title, note, reviewedBy, jobID, reviewedAt sql.NullString
	err := row.Scan(
		&r.ID, &r.Scenario, &paramsJSON, &title, &r.SubmitterID, &r.SubmitterName,
		&r.Status, &note, &reviewedBy, &jobID, &createdAt, &reviewedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(paramsJSON), &r.Params)
	r.Title = title.String
	r.ReviewNote = note.String
	r.ReviewedBy = reviewedBy.String
	r.JobID = jobID.String
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if reviewedAt.Valid {
		if t, err := time.Parse(time.RFC3339, reviewedAt.String); err == nil {
			r.ReviewedAt = &t
		}
	}
	return &r, nil
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
		&r.ID, &r.Scenario, &r.CronExpr, &paramsJSON, &r.Timezone, &enabled,
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
	var artifactPath, artifactSHA256 sql.NullString
	var artifactSize sql.NullInt64

	err := row.Scan(
		&j.ID, &schedID, &j.Scenario, &paramsJSON, &status,
		&startedAt, &finishedAt, &log, &errMsg,
		&artifactPath, &artifactSHA256, &artifactSize,
	)
	if err != nil {
		return nil, err
	}
	j.ScheduleID = schedID.String
	j.Status = JobStatus(status)
	j.Log = log.String
	j.Error = errMsg.String
	j.ArtifactPath = artifactPath.String
	j.ArtifactSHA256 = artifactSHA256.String
	j.ArtifactSize = artifactSize.Int64
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
