package main

// inventory.go — the reachability inventory: which endpoints every scenario
// depends on, probed now, with each run's result kept.
//
// A run reads every scenario, derives its targets, probes each distinct target
// once, and writes the whole result in one transaction. One run is in flight at
// a time, and a run is not repeated inside inventoryTTL unless asked for, so a
// busy API cannot be used to hammer the databases behind it.
//
// Nothing here starts a scenario or changes an endpoint; see inventory_probe.go.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
)

const (
	inventoryTTL       = time.Minute
	inventoryParallel  = 8
	inventoryRetention = 30 * 24 * time.Hour
)

var inventoryKinds = []string{"db", "file", "s3", "broker", "redis", "dir", "scenario"}
var inventoryStatuses = []string{"ok", "degraded", "unresolved"}

var (
	inventoryTargets = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orchestrator_inventory_targets",
		Help: "Targets in the latest inventory run, by kind and status.",
	}, []string{"kind", "status"})

	inventoryLastRun = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_inventory_last_run_timestamp_seconds",
		Help: "Unix time at which the latest inventory run finished.",
	})
)

// invRow is one endpoint, or one scenario that could not be read, in a run.
type invRow struct {
	Key       string   `json:"target_key"`
	Kind      string   `json:"kind"`
	Status    string   `json:"status"`
	Class     string   `json:"error_class,omitempty"`
	LatencyMs int64    `json:"latency_ms"`
	Scenarios []string `json:"scenarios"`
}

// InventoryService runs and records inventories.
type InventoryService struct {
	db     *OrchestratorDB
	scenes map[string]*Scenario
	now    func() time.Time

	mu      sync.Mutex
	running string // run id while one is in flight
}

func NewInventoryService(db *OrchestratorDB, scenes map[string]*Scenario) *InventoryService {
	return &InventoryService{db: db, scenes: scenes, now: time.Now}
}

// Begin decides whether a run starts. It returns the id to report and whether
// this call started the run. started is false when a run is already in flight
// (its id is returned) or when the latest run is fresher than inventoryTTL and
// force is not set (that run's id is returned).
func (s *InventoryService) Begin(force bool) (runID string, started bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running != "" {
		return s.running, false, nil
	}
	last, err := s.db.latestInventoryRun()
	if err != nil {
		return "", false, err
	}
	if !force && last.ID != "" && s.now().Sub(last.At) < inventoryTTL {
		return last.ID, false, nil
	}
	id := uuid.New().String()
	s.running = id
	return id, true, nil
}

// Execute performs a run that Begin reported as started, and records it. It
// must be called exactly once for each such run: the in-flight marker is only
// cleared here.
func (s *InventoryService) Execute(ctx context.Context, runID string) error {
	defer func() {
		s.mu.Lock()
		s.running = ""
		s.mu.Unlock()
	}()
	at := s.now()
	rows := s.collect(ctx)
	if err := s.db.saveInventoryRun(runID, at, rows); err != nil {
		return fmt.Errorf("inventory: record run %s: %w", runID, err)
	}
	recordInventoryMetrics(rows, at)
	if _, err := s.db.pruneInventory(at.Add(-inventoryRetention)); err != nil {
		log.Error().Err(err).Msg("inventory: prune failed")
	}
	log.Info().Str("run_id", runID).Int("rows", len(rows)).Msg("inventory run recorded")
	return nil
}

// Run is Begin followed by Execute, for callers that wait. It reports the run
// id and whether a run was done by this call.
func (s *InventoryService) Run(ctx context.Context, force bool) (string, bool, error) {
	id, started, err := s.Begin(force)
	if err != nil || !started {
		return id, started, err
	}
	return id, true, s.Execute(ctx, id)
}

// collect derives the targets of every scenario, probes each distinct endpoint
// once, and returns one row per endpoint and one per unreadable scenario.
func (s *InventoryService) collect(ctx context.Context) []invRow {
	names := make([]string, 0, len(s.scenes))
	for name := range s.scenes {
		names = append(names, name)
	}
	sort.Strings(names)

	byIdent := map[string]*invRow{}
	probes := map[string]func(context.Context) error{}
	keyUses := map[string]int{}
	var rows []invRow
	for _, name := range names {
		targets, err := deriveTargets(s.scenes[name], nil)
		if err != nil {
			class := "parse"
			var ue *unresolvedError
			if errors.As(err, &ue) {
				class = ue.class
				log.Warn().Str("scenario", name).Str("class", class).Str("detail", ue.detail).
					Msg("inventory: scenario unresolved")
			}
			rows = append(rows, invRow{
				Key: "scenario:" + name, Kind: "scenario", Status: "unresolved",
				Class: class, Scenarios: []string{name},
			})
			continue
		}
		for _, t := range targets {
			if r, ok := byIdent[t.ident]; ok {
				r.Scenarios = append(r.Scenarios, name)
				continue
			}
			// Same endpoint, different credentials: a second row. Its key gets a
			// suffix, because the key is what a reader matches against the scenario.
			keyUses[t.Key]++
			key := t.Key
			if keyUses[t.Key] > 1 {
				key = fmt.Sprintf("%s #%d", t.Key, keyUses[t.Key])
			}
			byIdent[t.ident] = &invRow{Key: key, Kind: t.Kind, Scenarios: []string{name}}
			probes[t.ident] = t.probe
		}
	}

	sem := make(chan struct{}, inventoryParallel)
	var wg sync.WaitGroup
	for ident, row := range byIdent {
		wg.Add(1)
		sem <- struct{}{}
		go func(key string, row *invRow, probe func(context.Context) error) {
			defer wg.Done()
			defer func() { <-sem }()
			start := time.Now()
			err := runProbe(ctx, probe)
			row.LatencyMs = time.Since(start).Milliseconds()
			row.Class = classifyProbeErr(err)
			row.Status = "ok"
			if err != nil {
				row.Status = "degraded"
				// The text stays here, in the log. The snapshot keeps the class.
				log.Warn().Err(err).Str("target", key).Str("kind", row.Kind).Str("class", row.Class).
					Msg("inventory: endpoint degraded")
			}
		}(row.Key, row, probes[ident])
	}
	wg.Wait()

	for _, r := range byIdent {
		sort.Strings(r.Scenarios)
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Key < rows[j].Key
	})
	return rows
}

// scenarioStatus is the worst status among a scenario's rows: degraded beats
// unresolved, which beats ok. found is false when no row mentions the scenario.
func scenarioStatus(rows []invRow, name string) (status string, found bool) {
	rank := map[string]int{"ok": 0, "unresolved": 1, "degraded": 2}
	status = "ok"
	for _, r := range rows {
		if !containsString(r.Scenarios, name) {
			continue
		}
		found = true
		if rank[r.Status] > rank[status] {
			status = r.Status
		}
	}
	return status, found
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// recordInventoryMetrics sets every kind/status pair, zeros included, so a
// series that stopped occurring does not keep its last value.
func recordInventoryMetrics(rows []invRow, at time.Time) {
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Kind+"|"+r.Status]++
	}
	for _, k := range inventoryKinds {
		for _, st := range inventoryStatuses {
			inventoryTargets.WithLabelValues(k, st).Set(float64(counts[k+"|"+st]))
		}
	}
	inventoryLastRun.Set(float64(at.Unix()))
}

// WriteInventoryTable prints a run for a terminal, one endpoint per line.
func WriteInventoryTable(w io.Writer, runAt time.Time, rows []invRow) {
	fmt.Fprintf(w, "inventory run at %s\n", runAt.UTC().Format(time.RFC3339))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tKIND\tCLASS\tMS\tENDPOINT\tSCENARIOS")
	for _, r := range rows {
		class := r.Class
		if class == "" {
			class = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", r.Status, r.Kind, class, r.LatencyMs, r.Key,
			strings.Join(r.Scenarios, ","))
	}
	_ = tw.Flush()
}

// ─── storage ─────────────────────────────────────────────────────────────────

type inventoryRun struct {
	ID string
	At time.Time
}

// latestInventoryRun returns the newest run, or a zero value when none exists.
func (d *OrchestratorDB) latestInventoryRun() (inventoryRun, error) {
	var id, at string
	err := d.db.QueryRow(`
		SELECT run_id, run_at FROM inventory_runs
		 ORDER BY run_at DESC LIMIT 1`).Scan(&id, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return inventoryRun{}, nil
	}
	if err != nil {
		return inventoryRun{}, err
	}
	t, err := time.Parse(outboxTimeLayout, at)
	if err != nil {
		return inventoryRun{}, fmt.Errorf("inventory: bad run_at %q: %w", at, err)
	}
	return inventoryRun{ID: id, At: t}, nil
}

// saveInventoryRun writes a whole run in one transaction.
func (d *OrchestratorDB) saveInventoryRun(runID string, at time.Time, rows []invRow) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	ts := outboxTS(at)
	if _, err := tx.Exec(`INSERT INTO inventory_runs(run_id, run_at) VALUES(?,?)`, runID, ts); err != nil {
		return err
	}
	for _, r := range rows {
		scenarios, err := json.Marshal(r.Scenarios)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO inventory_snapshots(run_id, run_at, target_key, kind, status, error_class, latency_ms, scenarios)
			VALUES(?,?,?,?,?,?,?,?)`,
			runID, ts, r.Key, r.Kind, r.Status, nullIfEmpty(r.Class), r.LatencyMs, string(scenarios)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// inventoryRows returns one run's rows, in the order they were recorded.
func (d *OrchestratorDB) inventoryRows(runID string) ([]invRow, error) {
	rows, err := d.db.Query(`
		SELECT target_key, kind, status, COALESCE(error_class, ''), COALESCE(latency_ms, 0), scenarios
		  FROM inventory_snapshots WHERE run_id = ?
		 ORDER BY kind, target_key`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []invRow
	for rows.Next() {
		var r invRow
		var scenarios string
		if err := rows.Scan(&r.Key, &r.Kind, &r.Status, &r.Class, &r.LatencyMs, &scenarios); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(scenarios), &r.Scenarios); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// pruneInventory deletes runs older than before, with their snapshots. It
// returns the number of runs removed.
func (d *OrchestratorDB) pruneInventory(before time.Time) (int64, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	ts := outboxTS(before)
	if _, err := tx.Exec(`DELETE FROM inventory_snapshots WHERE run_id IN
		(SELECT run_id FROM inventory_runs WHERE run_at < ?)`, ts); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM inventory_runs WHERE run_at < ?`, ts)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// runInventoryOnce runs the inventory now, prints it, and returns the exit code:
// 0 when every endpoint answered, 1 when any did not, 2 when the inventory
// could not run at all.
func runInventoryOnce(ctx context.Context, svc *InventoryService, db *OrchestratorDB, out io.Writer) int {
	id, started, err := svc.Begin(true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "inventory:", err)
		return 2
	}
	if !started {
		fmt.Fprintln(os.Stderr, "inventory: a run is already in flight")
		return 2
	}
	if err := svc.Execute(ctx, id); err != nil {
		fmt.Fprintln(os.Stderr, "inventory:", err)
		return 2
	}
	run, err := db.latestInventoryRun()
	if err != nil {
		fmt.Fprintln(os.Stderr, "inventory:", err)
		return 2
	}
	rows, err := db.inventoryRows(id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "inventory:", err)
		return 2
	}
	WriteInventoryTable(out, run.At, rows)
	for _, r := range rows {
		if r.Status == "degraded" {
			return 1
		}
	}
	return 0
}

// StartInventoryTicker runs the inventory every interval until the returned
// function is called. A run that is still going when the next tick arrives is
// not overlapped: the tick is skipped.
func StartInventoryTicker(svc *InventoryService, interval time.Duration) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				id, started, err := svc.Begin(true)
				if err != nil {
					log.Error().Err(err).Msg("inventory: could not start scheduled run")
					continue
				}
				if !started {
					continue
				}
				if err := svc.Execute(ctx, id); err != nil {
					log.Error().Err(err).Str("run_id", id).Msg("inventory: scheduled run failed")
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
