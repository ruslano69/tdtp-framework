package main

// events.go — job events for downstream systems.
//
// The orchestrator consumes events (pubsub.go) and, from this file on, also
// publishes its own: job.started and job.finished. An event is written to the
// outbox in the same transaction as the job state it describes (db.go,
// FinishJob), and a dispatcher delivers it to each sink that asked for it.
//
// Guarantees, and what they cost:
//   - at-least-once: a crash between a send and its mark-as-sent sends the
//     event again. The receiver deduplicates on event_id.
//   - ordered per job and sink (job.started before job.finished), not across
//     jobs.
//   - a failed delivery is retried with backoff; after max_attempts it becomes
//     dead — kept in the outbox, visible to an operator, and never blocking
//     another event or a job.
//
// Configuration is an administrator's file (--events), never a scenario's:
// where events go is not something a scenario YAML can decide.
//
// Only the "log" sink exists so far. The webhook and RabbitMQ sinks follow
// (docs/proposals/orchestrator-inventory-and-events.md, §3.5).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

const (
	EventJobStarted  = "job.started"
	EventJobFinished = "job.finished"

	defaultMaxAttempts = 10
	maxEventBackoff    = 5 * time.Minute
	outboxBatch        = 100
	outboxTick         = time.Second
	outboxRetention    = 7 * 24 * time.Hour
	outboxPruneEvery   = time.Hour
	outboxGaugeEvery   = 15 * time.Second
)

var (
	knownEventTypes = []string{EventJobStarted, EventJobFinished}
	knownJobStatus  = []string{string(JobDone), string(JobFailed), string(JobCancelled)}
	knownSinkTypes  = []string{"log", "webhook", "rabbitmq"}
)

// EventSinkDef is one entry of the events file.
type EventSinkDef struct {
	Name          string   `yaml:"name"`
	Type          string   `yaml:"type"`
	Types         []string `yaml:"types"`          // empty = [job.finished]
	Scenarios     []string `yaml:"scenarios"`      // path.Match globs; empty = all
	Statuses      []string `yaml:"statuses"`       // job.finished only; empty = all
	IncludeParams bool     `yaml:"include_params"` // off: params stay inside the orchestrator
	MaxAttempts   int      `yaml:"max_attempts"`   // empty = 10

	// webhook only
	URL               string `yaml:"url"`
	Secret            string `yaml:"secret"`              // env:VARIABLE — never a literal
	AllowInsecureHTTP bool   `yaml:"allow_insecure_http"` // plain http, localhost only

	// rabbitmq only
	RabbitMQ *rabbitmqSinkDef `yaml:"rabbitmq,omitempty"`
}

type eventsFile struct {
	Sinks []EventSinkDef `yaml:"sinks"`
}

// LoadEvents reads the events file. An empty path means no sinks: the
// orchestrator writes no outbox rows at all.
//
// Unknown keys are an error. A typo such as "scenario:" for "scenarios:" would
// otherwise be ignored and the sink would receive every scenario — the one
// direction a filter typo can fail is toward more disclosure.
func LoadEvents(filePath string) ([]EventSinkDef, error) {
	if filePath == "" {
		return nil, nil
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("events: open %s: %w", filePath, err)
	}
	defer func() { _ = f.Close() }()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var ef eventsFile
	if err := dec.Decode(&ef); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("events: %s defines no sinks", filePath)
		}
		return nil, fmt.Errorf("events: parse %s: %w", filePath, err)
	}
	if len(ef.Sinks) == 0 {
		return nil, fmt.Errorf("events: %s defines no sinks", filePath)
	}
	if err := prepareSinks(ef.Sinks); err != nil {
		return nil, fmt.Errorf("events: %s: %w", filePath, err)
	}
	// A missing secret is found now, at startup, not on the first event.
	for _, s := range ef.Sinks {
		switch s.Type {
		case "webhook":
			if _, err := sinkSecret(s); err != nil {
				return nil, fmt.Errorf("events: %s: %w", filePath, err)
			}
		case "rabbitmq":
			if _, err := newRabbitMQSink(s); err != nil {
				return nil, fmt.Errorf("events: %s: %w", filePath, err)
			}
		}
	}
	return ef.Sinks, nil
}

// prepareSinks validates the definitions and fills in defaults. It rewrites
// the slice in place, so callers that build definitions by hand (tests) get
// the same result as LoadEvents.
func prepareSinks(sinks []EventSinkDef) error {
	seen := map[string]bool{}
	for i := range sinks {
		s := &sinks[i]
		if s.Name == "" {
			return fmt.Errorf("sink #%d: name is required", i+1)
		}
		if seen[s.Name] {
			return fmt.Errorf("sink %q: duplicate name", s.Name)
		}
		seen[s.Name] = true

		if !slices.Contains(knownSinkTypes, s.Type) {
			return fmt.Errorf("sink %q: type %q is not supported (supported: %v)", s.Name, s.Type, knownSinkTypes)
		}
		switch s.Type {
		case "log":
			if s.URL != "" || s.Secret != "" || s.AllowInsecureHTTP || s.RabbitMQ != nil {
				return fmt.Errorf("sink %q: url, secret, allow_insecure_http and rabbitmq apply to their own sink types only", s.Name)
			}
		case "webhook":
			if s.RabbitMQ != nil {
				return fmt.Errorf("sink %q: the rabbitmq block applies to rabbitmq sinks only", s.Name)
			}
			if err := validateWebhookDef(*s); err != nil {
				return fmt.Errorf("sink %q: %w", s.Name, err)
			}
		case "rabbitmq":
			if s.URL != "" || s.Secret != "" || s.AllowInsecureHTTP {
				return fmt.Errorf("sink %q: url, secret and allow_insecure_http apply to webhook sinks only", s.Name)
			}
			if err := validateRabbitMQDef(*s); err != nil {
				return fmt.Errorf("sink %q: %w", s.Name, err)
			}
		}
		if len(s.Types) == 0 {
			s.Types = []string{EventJobFinished}
		}
		for _, t := range s.Types {
			if !slices.Contains(knownEventTypes, t) {
				return fmt.Errorf("sink %q: unknown event type %q (known: %v)", s.Name, t, knownEventTypes)
			}
		}
		for _, st := range s.Statuses {
			if !slices.Contains(knownJobStatus, st) {
				return fmt.Errorf("sink %q: unknown status %q (known: %v)", s.Name, st, knownJobStatus)
			}
		}
		for _, g := range s.Scenarios {
			if _, err := path.Match(g, "probe"); err != nil {
				return fmt.Errorf("sink %q: scenario pattern %q: %w", s.Name, g, err)
			}
		}
		if s.MaxAttempts == 0 {
			s.MaxAttempts = defaultMaxAttempts
		}
		if s.MaxAttempts < 1 {
			return fmt.Errorf("sink %q: max_attempts must be at least 1", s.Name)
		}
	}
	return nil
}

// ─── Event payload ───────────────────────────────────────────────────────────

// jobEvent is the JSON a receiver gets. It carries no job log: output can hold
// row data and driver errors with hosts in them. The log stays in the job
// record, behind the existing RBAC.
type jobEvent struct {
	EventID    string            `json:"event_id"`
	Type       string            `json:"type"`
	OccurredAt time.Time         `json:"occurred_at"`
	Job        jobEventJob       `json:"job"`
	Artifact   *jobEventArtifact `json:"artifact"`
	Error      *string           `json:"error"`
	Params     map[string]string `json:"params"`
}

type jobEventJob struct {
	ID          string     `json:"id"`
	Scenario    string     `json:"scenario"`
	ScheduleID  *string    `json:"schedule_id"`
	SubmittedBy *string    `json:"submitted_by"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	DurationMs  *int64     `json:"duration_ms"`
	Runner      string     `json:"runner"`
}

type jobEventArtifact struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// eventRouter turns one job transition into one outbox row per matching sink.
// A nil router is valid and produces nothing: an executor built without an
// events file, or in a test, takes the same path with no special casing.
type eventRouter struct {
	sinks []EventSinkDef
}

func newEventRouter(sinks []EventSinkDef) *eventRouter {
	if len(sinks) == 0 {
		return nil
	}
	return &eventRouter{sinks: sinks}
}

// build produces the rows for one event. status is the job's status now
// ("running" for job.started). art and errMsg apply to job.finished only.
func (r *eventRouter) build(typ string, job *Job, status string, art *ArtifactMeta, errMsg string, now time.Time) []OutboxEvent {
	if r == nil {
		return nil
	}
	var rows []OutboxEvent
	for _, s := range r.sinks {
		if !s.wants(typ, job.Scenario, status) {
			continue
		}
		ev := jobEvent{
			EventID:    uuid.New().String(),
			Type:       typ,
			OccurredAt: now.UTC(),
			Job: jobEventJob{
				ID:          job.ID,
				Scenario:    job.Scenario,
				ScheduleID:  optString(job.ScheduleID),
				SubmittedBy: optString(job.SubmittedBy),
				Status:      status,
				StartedAt:   job.StartedAt.UTC(),
				Runner:      job.Runner,
			},
		}
		if typ == EventJobFinished {
			finished := now.UTC()
			ms := finished.Sub(job.StartedAt).Milliseconds()
			ev.Job.FinishedAt = &finished
			ev.Job.DurationMs = &ms
			if art != nil {
				ev.Artifact = &jobEventArtifact{SHA256: art.SHA256, Size: art.Size}
			}
			if errMsg != "" {
				ev.Error = &errMsg
			}
		}
		if s.IncludeParams {
			ev.Params = job.Params
		}
		payload, err := json.Marshal(ev)
		if err != nil {
			// Cannot happen for this shape; if it did, dropping the one event is
			// less bad than failing the job the event describes.
			log.Error().Err(err).Str("sink", s.Name).Str("job_id", job.ID).Msg("job event: marshal failed")
			continue
		}
		rows = append(rows, OutboxEvent{
			ID:        ev.EventID,
			JobID:     job.ID,
			Sink:      s.Name,
			Type:      typ,
			Payload:   payload,
			CreatedAt: now,
		})
	}
	return rows
}

// wants reports whether this sink asked for the event. statuses filters
// job.finished only: job.started has no terminal status to compare.
func (s EventSinkDef) wants(typ, scenario, status string) bool {
	if !slices.Contains(s.Types, typ) {
		return false
	}
	if len(s.Scenarios) > 0 {
		matched := false
		for _, g := range s.Scenarios {
			if ok, _ := path.Match(g, scenario); ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if typ == EventJobFinished && len(s.Statuses) > 0 && !slices.Contains(s.Statuses, status) {
		return false
	}
	return true
}

// ─── Sinks ───────────────────────────────────────────────────────────────────

// EventSink delivers one outbox row. Send returns nil only when the receiver
// has accepted the event; anything else is an error the dispatcher acts on.
type EventSink interface {
	Send(ctx context.Context, ev OutboxEvent) error
}

// SinkError classifies a failed delivery. Class is what the outbox keeps and
// what the API shows — never the raw text, which can contain a host. Err is
// for the orchestrator's own log only.
type SinkError struct {
	Class     string
	Permanent bool // true: retrying the same event cannot succeed
	Err       error
}

func (e *SinkError) Error() string {
	if e.Err == nil {
		return "sink: " + e.Class
	}
	return "sink " + e.Class + ": " + e.Err.Error()
}

func (e *SinkError) Unwrap() error { return e.Err }

func classifySinkErr(err error) (class string, permanent bool) {
	var se *SinkError
	if errors.As(err, &se) {
		return se.Class, se.Permanent
	}
	return "other", false
}

// logSink writes one line per event to the orchestrator log. It never fails,
// and it does not log the payload, so it cannot leak what the payload omits.
type logSink struct {
	name string
}

func (s logSink) Send(_ context.Context, ev OutboxEvent) error {
	log.Info().
		Str("sink", s.name).
		Str("event_id", ev.ID).
		Str("type", ev.Type).
		Str("job_id", ev.JobID).
		Msg("job event")
	return nil
}

func buildSink(def EventSinkDef) (EventSink, error) {
	switch def.Type {
	case "log":
		return logSink{name: def.Name}, nil
	case "webhook":
		sink, err := newWebhookSink(def)
		if err != nil {
			return nil, err
		}
		return sink, nil
	case "rabbitmq":
		sink, err := newRabbitMQSink(def)
		if err != nil {
			return nil, err
		}
		return sink, nil
	default:
		return nil, fmt.Errorf("sink %q: unsupported type %q", def.Name, def.Type)
	}
}

// ─── Dispatcher ──────────────────────────────────────────────────────────────

// EventDispatcher drains the outbox. It is a single goroutine: a slow sink
// delays the others. Per-sink workers are the obvious next step once a
// network sink exists, not now.
type EventDispatcher struct {
	db          *OrchestratorDB
	sinks       map[string]EventSink
	maxAttempts map[string]int
	backoff     func(attempt int) time.Duration
	now         func() time.Time
	batch       int
}

func newEventDispatcher(db *OrchestratorDB, defs []EventSinkDef) (*EventDispatcher, error) {
	d := &EventDispatcher{
		db:          db,
		sinks:       make(map[string]EventSink, len(defs)),
		maxAttempts: make(map[string]int, len(defs)),
		backoff:     eventBackoff,
		now:         time.Now,
		batch:       outboxBatch,
	}
	for _, def := range defs {
		sink, err := buildSink(def)
		if err != nil {
			return nil, err
		}
		d.sinks[def.Name] = sink
		d.maxAttempts[def.Name] = def.MaxAttempts
	}
	return d, nil
}

// eventBackoff is 1s, 2s, 4s … capped at maxEventBackoff. attempt is the
// number of failures so far, starting at 1.
func eventBackoff(attempt int) time.Duration {
	if attempt > 9 { // 2^9 s is already past the cap
		return maxEventBackoff
	}
	d := time.Second << (attempt - 1)
	if d > maxEventBackoff {
		return maxEventBackoff
	}
	return d
}

// RunOnce makes one pass over the due rows and returns how many were sent.
func (d *EventDispatcher) RunOnce(ctx context.Context) (int, error) {
	due, err := d.db.dueOutbox(d.now(), d.batch)
	if err != nil {
		return 0, fmt.Errorf("outbox: due: %w", err)
	}
	delivered := 0
	for _, r := range due {
		if ctx.Err() != nil {
			break // shutting down: leave the rest pending, they are not failures
		}
		sink, ok := d.sinks[r.Sink]
		if !ok {
			// The sink was in the events file when the row was written and is
			// not now. Nothing will ever deliver it: dead, and loud about it.
			_ = d.db.markOutboxDead(r.ID, r.Attempts, "no_sink")
			log.Error().Str("event_id", r.ID).Str("sink", r.Sink).Msg("job event dead: sink no longer configured")
			continue
		}

		ev := OutboxEvent{ID: r.ID, JobID: r.JobID, Sink: r.Sink, Type: r.Type, Payload: r.Payload}
		sendErr := sink.Send(ctx, ev)
		if sendErr == nil {
			if err := d.db.markOutboxSent(r.ID, d.now()); err != nil {
				// Delivered, but not recorded: it will be sent again. At-least-once.
				log.Error().Err(err).Str("event_id", r.ID).Msg("job event sent but not marked")
				continue
			}
			eventsSentTotal.WithLabelValues(r.Sink, r.Type).Inc()
			delivered++
			continue
		}
		if ctx.Err() != nil {
			break // the send was cut short by shutdown, not refused
		}

		class, permanent := classifySinkErr(sendErr)
		eventsFailedTotal.WithLabelValues(r.Sink, class).Inc()
		attempts := r.Attempts + 1
		if permanent || attempts >= d.maxAttempts[r.Sink] {
			_ = d.db.markOutboxDead(r.ID, attempts, class)
			log.Error().Err(sendErr).Str("event_id", r.ID).Str("sink", r.Sink).
				Str("class", class).Int("attempts", attempts).Msg("job event dead")
			continue
		}
		next := d.now().Add(d.backoff(attempts))
		_ = d.db.markOutboxRetry(r.ID, attempts, next, class)
		log.Warn().Err(sendErr).Str("event_id", r.ID).Str("sink", r.Sink).
			Str("class", class).Int("attempts", attempts).Time("next", next).Msg("job event retry scheduled")
	}
	return delivered, nil
}

// Run delivers until ctx is cancelled. It also prunes delivered rows past
// their retention, once at start and then hourly, and refreshes the queue
// gauges every few seconds.
func (d *EventDispatcher) Run(ctx context.Context) {
	d.prune()
	d.refreshGauges()
	tick := time.NewTicker(outboxTick)
	defer tick.Stop()
	pruneTick := time.NewTicker(outboxPruneEvery)
	defer pruneTick.Stop()
	gaugeTick := time.NewTicker(outboxGaugeEvery)
	defer gaugeTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if _, err := d.RunOnce(ctx); err != nil && ctx.Err() == nil {
				log.Error().Err(err).Msg("job events: dispatch pass failed")
			}
		case <-pruneTick.C:
			d.prune()
		case <-gaugeTick.C:
			d.refreshGauges()
		}
	}
}

// refreshGauges publishes how many rows are waiting and how many have given
// up. Counted from the index, so it stays cheap while delivered rows age out.
func (d *EventDispatcher) refreshGauges() {
	counts, err := d.db.outboxStatusCounts()
	if err != nil {
		log.Error().Err(err).Msg("job events: gauge refresh failed")
		return
	}
	outboxPending.Set(float64(counts["pending"]))
	outboxDead.Set(float64(counts["dead"]))
}

func (d *EventDispatcher) prune() {
	n, err := d.db.PruneSentOutbox(d.now().Add(-outboxRetention))
	if err != nil {
		log.Error().Err(err).Msg("job events: prune failed")
		return
	}
	if n > 0 {
		log.Info().Int64("rows", n).Msg("job events: pruned delivered rows")
	}
}

// StartEventDispatcher runs the dispatcher for the given sinks and returns a
// function that stops it and waits for the current pass to finish. With no
// sinks it does nothing.
func StartEventDispatcher(db *OrchestratorDB, defs []EventSinkDef) (func(), error) {
	if len(defs) == 0 {
		return func() {}, nil
	}
	d, err := newEventDispatcher(db, defs)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.Run(ctx)
	}()
	return func() {
		cancel()
		<-done
		for _, s := range d.sinks {
			if c, ok := s.(io.Closer); ok {
				_ = c.Close()
			}
		}
	}, nil
}
