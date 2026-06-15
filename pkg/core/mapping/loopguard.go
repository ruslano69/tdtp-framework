package mapping

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// logEntry records one execution of a mapping.
type logEntry struct {
	ID           string    `json:"id"`
	MappingID    string    `json:"mapping_id"`
	SourceSystem string    `json:"source_system"`
	TargetSystem string    `json:"target_system"`
	StartedAt    time.Time `json:"started_at"`
	Status       string    `json:"status"` // "running" | "completed" | "failed"
}

var logMu sync.Mutex

// logFilePath returns the path to the mapping log file.
// Stored in ~/.tdtp/mapping_log.json (created on first use).
func logFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".tdtp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "mapping_log.json"), nil
}

func readLog(path string) ([]logEntry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []logEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		// Corrupted log (e.g. partial write on crash) — reset to empty rather than
		// blocking all future runs. The old log is overwritten on next writeLog call.
		return nil, nil
	}
	return entries, nil
}

func writeLog(path string, entries []logEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// parseMinInterval parses a duration string like "10s", "1m", "30s".
func parseMinInterval(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}

// CheckAndRecord verifies loop guard rules (Layers 2+4) and records the new run.
// Returns a correlation ID and a cleanup function that marks the run as completed.
// Returns an error if the run should be blocked.
func CheckAndRecord(cfg *MappingConfig) (correlationID string, done func(success bool), err error) {
	lg := cfg.LoopGuard
	minInterval, err := parseMinInterval(lg.MinInterval)
	if err != nil {
		return "", nil, fmt.Errorf("invalid min_interval %q: %w", lg.MinInterval, err)
	}

	logMu.Lock()
	defer logMu.Unlock()

	path, err := logFilePath()
	if err != nil {
		return "", nil, fmt.Errorf("loopguard log path: %w", err)
	}

	entries, err := readLog(path)
	if err != nil {
		return "", nil, fmt.Errorf("read mapping log: %w", err)
	}

	now := time.Now().UTC()

	// Layer 4: min_interval cooldown — block if a run with same src+target+mappingID completed recently.
	// We scope checks to the same MappingID so that parallel steps targeting the same
	// src→target pair (e.g. guides + tours + schedule all → postgres-branch) don't
	// block each other. Only an instance of the SAME mapping should block itself.
	if lg.SourceSystem != "" && lg.TargetSystem != "" {
		for _, e := range entries {
			if e.SourceSystem != lg.SourceSystem || e.TargetSystem != lg.TargetSystem || e.MappingID != cfg.ID {
				continue
			}
			if e.Status == "running" {
				return "", nil, fmt.Errorf(
					"loop guard: mapping %s (%s→%s) is already running (id=%s started=%s)",
					cfg.ID, lg.SourceSystem, lg.TargetSystem, e.ID, e.StartedAt.Format(time.RFC3339))
			}
			if minInterval > 0 && e.Status == "completed" && now.Sub(e.StartedAt) < minInterval {
				return "", nil, fmt.Errorf(
					"loop guard: min_interval=%s not elapsed since last run (started=%s, elapsed=%s)",
					lg.MinInterval, e.StartedAt.Format(time.RFC3339), now.Sub(e.StartedAt).Round(time.Millisecond))
			}
		}
	}

	// Record the new run
	id := newUUID()
	entry := logEntry{
		ID:           id,
		MappingID:    cfg.ID,
		SourceSystem: lg.SourceSystem,
		TargetSystem: lg.TargetSystem,
		StartedAt:    now,
		Status:       "running",
	}
	entries = append(entries, entry)

	// Keep only recent entries (last 100) to avoid unbounded growth
	if len(entries) > 100 {
		entries = entries[len(entries)-100:]
	}

	if err := writeLog(path, entries); err != nil {
		return "", nil, fmt.Errorf("write mapping log: %w", err)
	}

	done = func(success bool) {
		logMu.Lock()
		defer logMu.Unlock()

		current, _ := readLog(path)
		status := "completed"
		if !success {
			status = "failed"
		}
		for i, e := range current {
			if e.ID == id {
				current[i].Status = status
				break
			}
		}
		_ = writeLog(path, current)
	}

	return id, done, nil
}

// newUUID generates a simple time-based unique ID (not RFC 4122, good enough for log keys).
func newUUID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
