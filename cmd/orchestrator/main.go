// orchestrator — scenario execution server.
//
// HTTP API for manual activation, cron scheduling, and pipeline-completion
// pub/sub triggers, over one or more pluggable runners (tdtpcli by default;
// see runners.go for others).
// Scenarios = rendered YAML files with an optional orchestrator: header,
// including which runner they need (orchestrator.runner:).
// Schedules = stored in SQLite DB; seeded from YAML files on first run.
// Pub/sub triggers = subscribe to pkg/resultlog's tdtp:pipeline:* events and
// run the scenario mapped to each pipeline's result_name (see pubsub.go).
//
// Usage:
//
//	orchestrator --scenarios ./scenarios --db orchestrator.db --tdtpcli ./tdtpcli
//	orchestrator --scenarios ./scenarios --db orchestrator.db --runners ./runners.yaml
//	orchestrator --scenarios ./scenarios --db orchestrator.db --redis-addr localhost:6379 --pubsub ./pubsub.yaml
//
// API:
//
//	GET  /scenarios                   list available scenarios
//	GET  /scenarios/{name}            scenario definition
//	POST /scenarios/{name}/run        run with params → {job_id}
//	POST /scenarios/{name}/approve    approve currently loaded content (admin)
//	GET  /scenarios/{name}/approval   view approval record + live match (admin)
//	DELETE /scenarios/{name}/approval revoke approval (admin)
//	GET  /jobs                        recent jobs (last 100)
//	GET  /jobs/{id}                   job status + log
//	GET  /jobs/{id}/artifact          download the job output file
//	POST /jobs/{id}/cancel             abort a job that hasn't started yet
//	POST /jobs/{id}/stop               request graceful termination of a running job
//	GET  /schedules                   list schedules
//	POST /schedules                   add schedule
//	PATCH /schedules/{id}/enable      resume
//	PATCH /schedules/{id}/disable     pause
//	DELETE /schedules/{id}            remove
//	GET  /healthz
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	scenariosDir := flag.String("scenarios", "./scenarios", "directory with scenario YAML files")
	schedulesDir := flag.String("schedules-seed", "./schedules", "directory with schedule seed YAML files")
	dbPath := flag.String("db", "orchestrator.db", "SQLite database path")
	tdtpcliPath := flag.String("tdtpcli", "./tdtpcli", "path to tdtpcli binary (used only when --runners is empty)")
	runnersPath := flag.String("runners", "", "path to runners.yaml (empty = synthesize a single 'tdtpcli' runner from --tdtpcli)")
	tmpDir := flag.String("tmp", os.TempDir(), "directory for rendered pipeline YAMLs")
	addr := flag.String("addr", ":8080", "listen address")
	licensePath := flag.String("license", "", "path to tdtp.lic (default: TDTP_LICENSE env, ./tdtp.lic, else community)")
	mercuryURL := flag.String("mercury-url", "", "xZMercury base URL for online preflight (empty = skip)")
	requireProd := flag.Bool("require-prod", false, "refuse to start if Mercury is in dev mode or not CA-authorized")
	noAuth := flag.Bool("no-auth", false, "disable token authentication (local dev only — every request is admin)")
	authType := flag.String("auth-type", "token", "authentication type: token|ldap")
	ldapURL := flag.String("ldap-url", "", "LDAP server URL (ldap auth only), e.g. ldap://corp.example.com:389")
	ldapBindDN := flag.String("ldap-bind-dn", "", "LDAP service account DN (ldap auth only)")
	ldapBindPass := flag.String("ldap-bind-pass", "", "LDAP service account password (ldap auth only)")
	ldapBaseDN := flag.String("ldap-base-dn", "", "LDAP search base DN (ldap auth only)")
	redisAddr := flag.String("redis-addr", "", "Redis address for pipeline-completion pub/sub triggers (empty = disabled)")
	redisPassword := flag.String("redis-password", "", "Redis password (pubsub trigger only)")
	redisDB := flag.Int("redis-db", 0, "Redis DB number (pubsub trigger only)")
	pubsubPath := flag.String("pubsub", "", "path to pubsub.yaml mapping pipeline result_name -> scenario (requires --redis-addr)")
	flag.Parse()

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	// Trust gate: verify own license (offline) and preflight Mercury (online).
	trustCtx, trustCancel := context.WithTimeout(context.Background(), 10*time.Second)
	gate, err := NewTrustGate(trustCtx, *licensePath, *mercuryURL, *requireProd)
	trustCancel()
	if err != nil {
		log.Fatal().Err(err).Msg("trust gate failed")
	}
	log.Info().
		Str("license", gate.License.LicenseeName()).
		Str("tier", string(gate.License.GetTier())).
		Int("pipeline_limit", gate.License.PipelineLimit()).
		Msg("license verified")
	if gate.MercuryStatus != nil {
		log.Info().
			Str("mode", gate.MercuryStatus.Mode).
			Bool("ca_authorized", gate.MercuryStatus.CAAuthorized).
			Strs("permissions", gate.MercuryStatus.Permissions).
			Msg("mercury preflight ok")
	}

	// Open DB.
	db, err := OpenOrchestratorDB(*dbPath)
	if err != nil {
		log.Fatal().Err(err).Msg("open orchestrator db")
	}
	// fatal closes db then exits — needed because log.Fatal() calls os.Exit(),
	// which skips deferred functions, so a plain `defer db.Close()` here
	// would never run before any of the fatal checks below.
	fatal := func(err error, msg string) {
		_ = db.Close()
		log.Fatal().Err(err).Msg(msg)
	}

	// Load scenarios from directory.
	scenes, err := LoadScenariosDir(*scenariosDir)
	if err != nil {
		fatal(err, "load scenarios: "+*scenariosDir)
	}
	log.Info().Int("count", len(scenes)).Str("dir", *scenariosDir).Msg("scenarios loaded")

	// Runners: named execution backends. --runners config takes precedence;
	// otherwise synthesize the legacy single-tdtpcli behavior so existing
	// deployments and scenario files are unaffected.
	var runners map[string]RunnerSpec
	if *runnersPath != "" {
		runners, err = LoadRunners(*runnersPath)
		if err != nil {
			fatal(err, "load runners: "+*runnersPath)
		}
	} else {
		runners = map[string]RunnerSpec{
			defaultRunnerName: {Binary: *tdtpcliPath, Args: []string{"--pipeline", "{{.tmpfile}}"}},
		}
	}
	log.Info().Int("count", len(runners)).Str("default", defaultRunnerName).Msg("runners loaded")

	// Fail fast on a typo'd orchestrator.runner: rather than at first Submit().
	if err := ValidateScenarioRunners(scenes, runners, defaultRunnerName); err != nil {
		fatal(err, "scenario runner validation failed")
	}

	// Wire executor and scheduler.
	executor := NewExecutor(runners, defaultRunnerName, filepath.Join(*tmpDir, "orch-pipelines"), db)
	scheduler := NewScheduler(executor, scenes, db, gate)

	// Seed schedules from YAML → DB (idempotent: ON CONFLICT DO UPDATE).
	if err := scheduler.SeedFromDir(*schedulesDir); err != nil {
		log.Warn().Err(err).Msg("schedule seed failed (non-fatal)")
	}

	// Load all enabled schedules from DB and register with cron.
	if err := scheduler.LoadFromDB(); err != nil {
		fatal(err, "load schedules from db")
	}

	// Pub/sub trigger: subscribe to pkg/resultlog's pipeline-completion events
	// (tdtp:pipeline:*) and run the scenario mapped to each result_name.
	subscriber, err := setupPubSub(*redisAddr, *redisPassword, *redisDB, *pubsubPath, scenes, executor, gate, db)
	if err != nil {
		fatal(err, "pubsub setup failed")
	}

	// Authentication: token-based or LDAP.
	authMiddleware, auth, err := setupAuth(db, *authType, *ldapURL, *ldapBindDN, *ldapBindPass, *ldapBaseDN, *noAuth)
	if err != nil {
		fatal(err, "auth setup failed")
	}

	// All fatal-risk init done — register cleanup defers.
	defer func() { _ = db.Close() }()
	scheduler.Start()
	defer scheduler.Stop()

	// Seed active-job gauge from DB (jobs may have been in-flight before restart).
	SyncActiveJobs(db)

	r := newRouter(routerDeps{
		db:             db,
		scenes:         scenes,
		executor:       executor,
		scheduler:      scheduler,
		gate:           gate,
		auth:           auth,
		authMiddleware: authMiddleware,
		subscriber:     subscriber,
		mercuryURL:     *mercuryURL,
	})

	log.Info().Str("addr", *addr).Bool("auth", !*noAuth).Msg("orchestrator started")
	if err := http.ListenAndServe(*addr, r); err != nil {
		log.Error().Err(err).Msg("server error")
	}
}
