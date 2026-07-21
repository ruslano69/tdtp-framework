package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// JobStatus represents the execution state of a scenario run.
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobDone      JobStatus = "done"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled" // Cancel(pending) and Stop(running) both land here
)

// Job tracks one scenario execution.
type Job struct {
	ID             string            `json:"id"`
	ScheduleID     string            `json:"schedule_id,omitempty"` // empty = manual run
	Scenario       string            `json:"scenario"`
	Params         map[string]string `json:"params"`
	Status         JobStatus         `json:"status"`
	SubmittedBy    string            `json:"submitted_by,omitempty"` // principal ID; empty for cron runs
	StartedAt      time.Time         `json:"started_at"`
	FinishedAt     *time.Time        `json:"finished_at,omitempty"`
	Log            string            `json:"log,omitempty"`
	Error          string            `json:"error,omitempty"`
	ArtifactPath   string            `json:"artifact_path,omitempty"`   // local path to output file
	ArtifactSHA256 string            `json:"artifact_sha256,omitempty"` // hex SHA-256 of file
	ArtifactSize   int64             `json:"artifact_size,omitempty"`   // bytes
	CancelledBy    string            `json:"cancelled_by,omitempty"`
	CancelledAt    *time.Time        `json:"cancelled_at,omitempty"`
}

// Typed errors returned by Cancel/Stop so the HTTP layer can map them to
// specific status codes (404 vs 409) instead of a generic 500.
var (
	ErrJobNotFound   = errors.New("job not found")
	ErrJobNotPending = errors.New("job is not pending")
	ErrJobNotRunning = errors.New("job is not running")
)

// runnerFunc executes a command and returns its combined output.
// Injectable so Submit is testable without a real tdtpcli subprocess.
type runnerFunc func(ctx context.Context, bin string, args ...string) ([]byte, error)

// execRunner is the default runner: runs the binary via os/exec.
//
// cmd.Cancel/cmd.WaitDelay (Go 1.20+) give Stop/Cancel a graceful-then-forced
// termination for free: when ctx is cancelled, os/exec first calls Cancel
// (SIGTERM — let tdtpcli clean up), then force-kills if the process hasn't
// exited within WaitDelay. On Windows, Process.Signal doesn't support
// SIGTERM and Cancel will error immediately; WaitDelay's force-kill fallback
// still applies, so termination is still guaranteed, just not graceful.
// The orchestrator's deployment target is Linux, where this is graceful.
func execRunner(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 10 * time.Second
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// runningJob is the live handle for a job whose goroutine is still in
// flight — either not yet dispatched (pending) or actively running.
type runningJob struct {
	cancel  context.CancelFunc
	started bool // true once the goroutine has committed to exec.CommandContext
}

// Executor runs tdtpcli --pipeline with rendered scenario YAML.
// Jobs are persisted to OrchestratorDB so status survives restarts.
type Executor struct {
	tdtpcliPath string
	tmpDir      string
	logDir      string
	db          *OrchestratorDB
	run         runnerFunc
	// done, when non-nil, receives each job ID after its run completes.
	// Used by tests to await async completion without polling.
	done chan string

	mu       sync.Mutex
	registry map[string]*runningJob // jobID -> live handle, while pending or running
}

func NewExecutor(tdtpcliPath, tmpDir string, db *OrchestratorDB) *Executor {
	_ = os.MkdirAll(tmpDir, 0o700)
	logDir := filepath.Join(tmpDir, "logs")
	_ = os.MkdirAll(logDir, 0o700)
	return &Executor{
		tdtpcliPath: tdtpcliPath, tmpDir: tmpDir, logDir: logDir, db: db, run: execRunner,
		registry: make(map[string]*runningJob),
	}
}

// Submit renders the scenario with params and runs tdtpcli asynchronously.
// scheduleID is empty for manual (Activator) runs. submittedBy is the
// principal ID responsible for this run (empty for cron) — it is who,
// besides an Admin, may later Stop or Cancel it.
//
// The subprocess's context is deliberately NOT derived from any HTTP
// request: it is created fresh here and lives exactly as long as the job
// does. An earlier version passed the caller's r.Context() through, which
// is cancelled the moment the triggering HTTP handler returns — since the
// actual run happens in a detached goroutine that outlives the handler,
// that context could be (and likely was) cancelled while tdtpcli was still
// running, killing it prematurely. The independent context here is also
// what Cancel/Stop hold onto to terminate the job on purpose.
func (e *Executor) Submit(s *Scenario, params map[string]string, scheduleID, submittedBy string) (*Job, error) {
	rendered, err := renderTemplate(s.RawYAML, params)
	if err != nil {
		return nil, fmt.Errorf("executor: render template: %w", err)
	}

	tmpFile := filepath.Join(e.tmpDir, "pipeline-"+uuid.New().String()+".yaml")
	if err := os.WriteFile(tmpFile, rendered, 0o600); err != nil {
		return nil, fmt.Errorf("executor: write tmp pipeline: %w", err)
	}

	jobCtx, cancel := context.WithCancel(context.Background())

	job := &Job{
		ID:          uuid.New().String(),
		ScheduleID:  scheduleID,
		Scenario:    s.Orchestrator.Name,
		Params:      params,
		SubmittedBy: submittedBy,
		Status:      JobPending,
		StartedAt:   time.Now().UTC(),
	}
	if err := e.db.InsertJob(job); err != nil {
		cancel()
		_ = os.Remove(tmpFile)
		return nil, fmt.Errorf("executor: persist job: %w", err)
	}
	RecordJobSubmit()

	// Extract the --output path from the rendered YAML, if present.
	// The pipeline YAML may contain an "output:" key that tdtpcli writes to.
	outputPath := extractOutputPath(rendered)

	e.mu.Lock()
	e.registry[job.ID] = &runningJob{cancel: cancel}
	e.mu.Unlock()

	go func() {
		defer func() {
			_ = os.Remove(tmpFile)
			e.mu.Lock()
			delete(e.registry, job.ID)
			e.mu.Unlock()
		}()

		e.mu.Lock()
		if jobCtx.Err() != nil {
			// Cancel() ran before we ever got here — never dispatch the subprocess.
			e.mu.Unlock()
			_ = e.db.UpdateJobDone(job.ID, JobCancelled, "", "")
			RecordJobDone(job.Scenario, string(JobCancelled), job.StartedAt)
			if e.done != nil {
				e.done <- job.ID
			}
			return
		}
		e.registry[job.ID].started = true
		e.mu.Unlock()

		_ = e.db.UpdateJobStatus(job.ID, JobRunning)

		out, runErr := e.run(jobCtx, e.tdtpcliPath, "--pipeline", tmpFile)

		var status JobStatus
		errMsg := ""
		switch {
		case errors.Is(jobCtx.Err(), context.Canceled):
			status = JobCancelled
		case runErr != nil:
			status = JobFailed
			errMsg = runErr.Error()
		default:
			status = JobDone
		}

		logStr := string(out)
		if len(out) > maxJobLogBytes {
			logPath := filepath.Join(e.logDir, "job-"+job.ID+".log")
			_ = os.WriteFile(logPath, out, 0o600)
			tail := out[len(out)-maxJobLogBytes:]
			logStr = "[truncated — full log: " + logPath + "]\n" + string(tail)
		}

		_ = e.db.UpdateJobDone(job.ID, status, logStr, errMsg)
		RecordJobDone(job.Scenario, string(status), job.StartedAt)

		// Compute artifact metadata after a successful run.
		if status == JobDone && outputPath != "" {
			if sha256hex, size, err := fileHashAndSize(outputPath); err == nil {
				_ = e.db.UpdateJobArtifact(job.ID, outputPath, sha256hex, size)
			}
			// If the file doesn't exist (e.g. dry-run), skip silently.
		}

		if e.done != nil {
			e.done <- job.ID
		}
	}()

	return job, nil
}

// Cancel aborts a job that has not started running yet (status=pending).
// Returns ErrJobNotPending if it has already started or finished — use Stop
// for a running job instead.
func (e *Executor) Cancel(jobID, requestedBy string) error {
	job, err := e.db.GetJob(jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return ErrJobNotFound
	}
	if job.Status != JobPending {
		return ErrJobNotPending
	}

	e.mu.Lock()
	rj, ok := e.registry[jobID]
	if !ok {
		e.mu.Unlock()
		return ErrJobNotFound // finished/removed between the DB read above and this lock
	}
	if rj.started {
		e.mu.Unlock()
		return ErrJobNotPending // lost the race: it started running just now
	}
	rj.cancel()
	e.mu.Unlock()

	return e.db.MarkCancelRequested(jobID, requestedBy)
}

// Stop requests graceful termination of a currently running job
// (status=running). Termination itself is asynchronous — Stop returns once
// the request is recorded and the subprocess has been signalled, not once
// it has actually exited; the goroutine started in Submit resolves the
// final "cancelled" status once the process exits.
func (e *Executor) Stop(jobID, requestedBy string) error {
	job, err := e.db.GetJob(jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return ErrJobNotFound
	}
	if job.Status != JobRunning {
		return ErrJobNotRunning
	}

	e.mu.Lock()
	rj, ok := e.registry[jobID]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("job has no live process handle (orchestrator may have restarted): %w", ErrJobNotRunning)
	}
	rj.cancel()
	return e.db.MarkCancelRequested(jobID, requestedBy)
}

// jobActionHandler wraps Cancel/Stop as an HTTP handler: resolves the job,
// checks ownership (Admin may act on any job; anyone else only their own),
// invokes the action, and maps its typed errors to the right status code.
func jobActionHandler(db *OrchestratorDB, action func(jobID, requestedBy string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		job, err := db.GetJob(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if job == nil {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		p := PrincipalFrom(r.Context())
		if p != nil && p.Role != RoleAdmin && job.SubmittedBy != principalID(p) {
			writeError(w, http.StatusForbidden, "not your job")
			return
		}
		if err := action(id, principalName(p)); err != nil {
			switch {
			case errors.Is(err, ErrJobNotFound):
				writeError(w, http.StatusNotFound, err.Error())
			case errors.Is(err, ErrJobNotPending), errors.Is(err, ErrJobNotRunning):
				writeError(w, http.StatusConflict, err.Error())
			default:
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

const maxJobLogBytes = 64 * 1024

// extractOutputPath scans rendered pipeline YAML for an "output:" line and returns
// the value. Returns "" when not found or when the value looks like an s3:// URI
// (non-local paths are not served via the artifact endpoint).
func extractOutputPath(yaml []byte) string {
	for _, line := range strings.Split(string(yaml), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "output:") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, "output:"))
		// Strip surrounding quotes if present.
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') {
			val = val[1 : len(val)-1]
		}
		// Only keep local paths; skip s3://, http://, etc.
		if strings.Contains(val, "://") {
			return ""
		}
		return val
	}
	return ""
}

// fileHashAndSize opens path, computes its SHA-256, and returns (hexHash, size, error).
func fileHashAndSize(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// renderTemplate substitutes {{.param}} in YAML using text/template.
// The orchestrator: block in the YAML is passed through as-is (tdtpcli ignores it).
func renderTemplate(yamlBytes []byte, params map[string]string) ([]byte, error) {
	// text/template uses {{.Name}} syntax.
	// Pipeline YAML uses {{.period}}, {{.department}} etc.
	// We strip the orchestrator: block from the rendered output so the temp file
	// is a clean pipeline YAML (though tdtpcli would ignore it anyway).
	src := string(yamlBytes)

	tmpl, err := template.New("pipeline").
		Option("missingkey=error"). // fail if param referenced but not provided
		Parse(src)
	if err != nil {
		return nil, fmt.Errorf("template parse: %w", err)
	}

	// Convert map[string]string to map[string]any for template execution.
	data := make(map[string]any, len(params))
	for k, v := range params {
		data[k] = v
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("template execute: %w", err)
	}

	return []byte(buf.String()), nil
}
