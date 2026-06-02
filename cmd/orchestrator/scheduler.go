package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// ScheduleDef is used only for YAML seed files.
type ScheduleDef struct {
	ID       string            `yaml:"id"`
	Scenario string            `yaml:"scenario"`
	Schedule string            `yaml:"schedule"` // cron expression
	Params   map[string]string `yaml:"params"`
	Timezone string            `yaml:"timezone"` // IANA timezone name (e.g. "Europe/Moscow"); empty = UTC
}

type scheduleFile struct {
	Schedules []ScheduleDef `yaml:"schedules"`
}

// Scheduler wraps robfig/cron, backed by OrchestratorDB.
// YAML files are seed-only: loaded once into DB on first run,
// then DB is the source of truth — runtime changes via API.
type Scheduler struct {
	c        *cron.Cron
	executor *Executor
	scenes   map[string]*Scenario
	db       *OrchestratorDB
	gate     *TrustGate
	entryIDs map[string]cron.EntryID // schedule DB id → cron entry id
}

func NewScheduler(executor *Executor, scenes map[string]*Scenario, db *OrchestratorDB, gate *TrustGate) *Scheduler {
	return &Scheduler{
		c:        cron.New(),
		executor: executor,
		scenes:   scenes,
		db:       db,
		gate:     gate,
		entryIDs: make(map[string]cron.EntryID),
	}
}

// SeedFromDir reads YAML schedule files and upserts into DB (idempotent).
// Call once at startup before LoadFromDB.
func (s *Scheduler) SeedFromDir(dir string) error {
	entries, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return err
	}
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var sf scheduleFile
		if err := yaml.Unmarshal(data, &sf); err != nil {
			return fmt.Errorf("scheduler: parse %s: %w", path, err)
		}
		for _, def := range sf.Schedules {
			rec := &ScheduleRecord{
				ID:       def.ID,
				Scenario: def.Scenario,
				CronExpr: def.Schedule,
				Params:   def.Params,
				Timezone: def.Timezone,
				Enabled:  true,
			}
			if err := s.db.UpsertSchedule(rec); err != nil {
				return fmt.Errorf("seed schedule %q: %w", def.ID, err)
			}
		}
	}
	return nil
}

// LoadFromDB reads all enabled schedules from DB and registers them with cron.
func (s *Scheduler) LoadFromDB() error {
	schedules, err := s.db.ListSchedules()
	if err != nil {
		return err
	}
	for _, r := range schedules {
		if r.Enabled {
			if err := s.register(r); err != nil {
				return fmt.Errorf("scheduler: register %q: %w", r.ID, err)
			}
		}
	}
	return nil
}

func (s *Scheduler) register(r *ScheduleRecord) error {
	scene, ok := s.scenes[r.Scenario]
	if !ok {
		return fmt.Errorf("scenario %q not found", r.Scenario)
	}
	schedID := r.ID
	params := r.Params
	tz := r.Timezone

	entryID, err := s.c.AddFunc(r.CronExpr, func() {
		resolved := resolveMagicParams(params, tz)
		resolvedParams, valErr := scene.ValidateParams(resolved)
		status := "running"
		switch {
		case valErr != nil:
			status = "failed"
		case s.gate != nil && s.gate.GateScenario(scene) != nil:
			// License expired or scenario no longer permitted — skip the run.
			status = "failed"
		default:
			if _, execErr := s.executor.Submit(context.Background(), scene, resolvedParams, schedID); execErr != nil {
				status = "failed"
			}
		}
		var nextRun *time.Time
		if entry := s.c.Entry(s.entryIDs[schedID]); entry.Valid() {
			t := entry.Next
			nextRun = &t
		}
		_ = s.db.TouchScheduleRun(schedID, status, nextRun)
	})
	if err != nil {
		return err
	}
	s.entryIDs[schedID] = entryID
	return nil
}

// Add inserts a new schedule into DB and registers it with cron.
func (s *Scheduler) Add(r *ScheduleRecord) error {
	if err := s.db.UpsertSchedule(r); err != nil {
		return err
	}
	if r.Enabled {
		return s.register(r)
	}
	return nil
}

// Enable resumes a paused schedule.
func (s *Scheduler) Enable(id string) error {
	if err := s.db.SetEnabled(id, true); err != nil {
		return err
	}
	r, err := s.db.GetSchedule(id)
	if err != nil || r == nil {
		return err
	}
	return s.register(r)
}

// Disable pauses a schedule (removes from cron, keeps in DB with enabled=false).
func (s *Scheduler) Disable(id string) error {
	if err := s.db.SetEnabled(id, false); err != nil {
		return err
	}
	if entryID, ok := s.entryIDs[id]; ok {
		s.c.Remove(entryID)
		delete(s.entryIDs, id)
	}
	return nil
}

// Delete removes a schedule from DB and cron entirely.
func (s *Scheduler) Delete(id string) error {
	if entryID, ok := s.entryIDs[id]; ok {
		s.c.Remove(entryID)
		delete(s.entryIDs, id)
	}
	return s.db.DeleteSchedule(id)
}

func (s *Scheduler) Start() { s.c.Start() }
func (s *Scheduler) Stop()  { s.c.Stop() }

// resolveMagicParams replaces special tokens in schedule params.
// tz is an IANA timezone name; empty or invalid falls back to UTC.
func resolveMagicParams(params map[string]string, tz string) map[string]string {
	loc := time.UTC
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	out := make(map[string]string, len(params))
	now := time.Now().In(loc)
	for k, v := range params {
		v = strings.ReplaceAll(v, "{{current_month}}", now.Format("2006-01"))
		v = strings.ReplaceAll(v, "{{current_date}}", now.Format("2006-01-02"))
		v = strings.ReplaceAll(v, "{{yesterday}}", now.AddDate(0, 0, -1).Format("2006-01-02"))
		out[k] = v
	}
	return out
}
