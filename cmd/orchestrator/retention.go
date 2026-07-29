package main

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// Retention bounds, in weeks. One week is the shortest window that still lets
// someone come back after a weekend and read why Friday's run failed; beyond
// 55 the setting stops being a retention policy and becomes "keep everything",
// which is the state this exists to end.
const (
	MinRetentionWeeks = 1
	MaxRetentionWeeks = 55
)

// retentionInterval is how often stale logs are purged while running.
//
// Daily, not hourly: the thing being bounded grows over weeks, so a finer
// schedule would buy nothing and take the write lock more often.
const retentionInterval = 24 * time.Hour

// RetentionPolicy decides what happens to old job logs.
//
// It clears the log text and keeps the row. The two live in one column but have
// nothing else in common: the row is the fact that a scenario ran — when, with
// what status, how many rows — a few dozen bytes that answer "when did this
// table last sync" long after anyone stops caring about the output. The log is
// the steps' stdout, which is 56% of the database and useful for about as long
// as someone might still want to read it.
//
// Deleting rows instead would also orphan the artifact files they point at,
// since artifact_path is the only reference to them.
type RetentionPolicy struct {
	// Weeks is how long log text is kept. 0 disables purging entirely.
	Weeks int

	// VacuumMB is the database size at which a startup VACUUM is worth doing.
	// 0 never vacuums.
	//
	// Set it above the size the database is expected to settle at, or every
	// restart pays for a full rewrite of data that is entirely live.
	VacuumMB int
}

// Enabled reports whether anything is purged at all.
func (p RetentionPolicy) Enabled() bool { return p.Weeks > 0 }

// Validate rejects a policy that cannot be honoured.
func (p RetentionPolicy) Validate() error {
	if p.Weeks != 0 && (p.Weeks < MinRetentionWeeks || p.Weeks > MaxRetentionWeeks) {
		return fmt.Errorf("log retention is %d weeks; must be %d-%d, or 0 to disable",
			p.Weeks, MinRetentionWeeks, MaxRetentionWeeks)
	}
	if p.VacuumMB < 0 {
		return fmt.Errorf("vacuum threshold is %d MB; must not be negative", p.VacuumMB)
	}
	return nil
}

// Cutoff is the timestamp before which logs are considered stale.
func (p RetentionPolicy) Cutoff(now time.Time) time.Time {
	return now.Add(-time.Duration(p.Weeks) * 7 * 24 * time.Hour)
}

// PurgeJobLogs clears the log text of finished jobs started before cutoff and
// returns how many rows it touched.
//
// Running jobs are skipped: their log is still being written, and the executor
// would overwrite the NULL on completion anyway. The error column is left
// alone — it is small, it is rare, and it is the one piece of a failed run
// worth keeping after the surrounding output stops being interesting.
func (o *OrchestratorDB) PurgeJobLogs(cutoff time.Time) (int64, error) {
	res, err := o.db.Exec(`
		UPDATE jobs
		   SET log = NULL
		 WHERE started_at < ?
		   AND log IS NOT NULL
		   AND status != 'running'`,
		cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("purge job logs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("purge job logs: rows affected: %w", err)
	}
	return n, nil
}

// SizeBytes reports the size of the database.
//
// Read through PRAGMA rather than os.Stat so it needs no path and does not have
// to reason about journal or WAL files sitting next to it.
//
// The obvious alternative — asking the freelist how much a VACUUM would return
// — was tried first and is wrong for this workload. Clearing a log column does
// not usually free whole pages: the rows here average a few hundred bytes, so
// several share a page, and nulling their text leaves holes inside pages that
// are still in use. Measured on a real 851-job database, the freelist promised
// 0.04 MB while the VACUUM actually returned 0.45 MB — eleven times more. A
// threshold built on that number would essentially never fire, and the feature
// would look present while doing nothing.
func (o *OrchestratorDB) SizeBytes() (int64, error) {
	var pageCount, pageSize int64
	if err := o.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount); err != nil {
		return 0, fmt.Errorf("page_count: %w", err)
	}
	if err := o.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return 0, fmt.Errorf("page_size: %w", err)
	}
	return pageCount * pageSize, nil
}

// Vacuum rewrites the database, returning free pages to the filesystem.
//
// Only ever called at startup. VACUUM copies the entire database and holds a
// write lock for the duration, so running it on a live orchestrator would stall
// every job dispatch for as long as the copy takes. At startup nothing else is
// running yet and the cost is paid once, before the first schedule fires.
func (o *OrchestratorDB) Vacuum() error {
	if _, err := o.db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	return nil
}

// RunRetention purges stale logs once and reports what it did.
//
// withVacuum is true only at startup. A steady state without vacuuming is not a
// leak: freed pages are reused by later writes, so the file plateaus at its
// working-set size instead of growing without bound. VACUUM only decides
// whether that plateau is also returned to the filesystem.
func RunRetention(db *OrchestratorDB, p RetentionPolicy, withVacuum bool) {
	if !p.Enabled() {
		return
	}

	cutoff := p.Cutoff(time.Now())
	purged, err := db.PurgeJobLogs(cutoff)
	if err != nil {
		// Retention failing must never take the orchestrator down: the worst
		// case is a database that keeps growing, which is where it started.
		log.Error().Err(err).Msg("log retention: purge failed")
		return
	}
	if purged > 0 {
		log.Info().
			Int64("jobs", purged).
			Int("weeks", p.Weeks).
			Time("older_than", cutoff).
			Msg("log retention: cleared job logs")
	}

	if !withVacuum || p.VacuumMB == 0 {
		return
	}

	before, err := db.SizeBytes()
	if err != nil {
		log.Error().Err(err).Msg("log retention: could not measure database size")
		return
	}
	threshold := int64(p.VacuumMB) * 1024 * 1024
	if before < threshold {
		return
	}

	// Above the threshold the database is rewritten whether or not this
	// startup's purge found anything: the holes left by every previous purge
	// are still there, and only a repack closes them. The cost of getting that
	// wrong is one needless rewrite at a restart, which is the cheap direction.
	started := time.Now()
	if err := db.Vacuum(); err != nil {
		log.Error().Err(err).Msg("log retention: vacuum failed")
		return
	}
	after, err := db.SizeBytes()
	if err != nil {
		after = before // reporting only; the vacuum itself succeeded
	}
	// Kilobytes, not megabytes: the threshold is set in MB, but truncating the
	// report to MB prints "1 -> 0" for a database that went 1.09 -> 0.64, which
	// reads as data loss to anyone skimming the log.
	log.Info().
		Int64("before_kb", before/1024).
		Int64("after_kb", after/1024).
		Dur("took", time.Since(started).Round(time.Millisecond)).
		Msg("log retention: vacuumed")
}

// StartRetention runs the purge now (with a VACUUM if enough space would come
// back) and then daily. The returned function stops the loop.
func StartRetention(db *OrchestratorDB, p RetentionPolicy) func() {
	if !p.Enabled() {
		log.Info().Msg("log retention: disabled — job logs are kept forever")
		return func() {}
	}

	log.Info().
		Int("weeks", p.Weeks).
		Int("vacuum_threshold_mb", p.VacuumMB).
		Msg("log retention: enabled")

	RunRetention(db, p, true)

	ticker := time.NewTicker(retentionInterval)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				RunRetention(db, p, false)
			case <-done:
				return
			}
		}
	}()

	return func() {
		ticker.Stop()
		close(done)
	}
}
