package main

// pubsub.go — event-driven scenario triggers from pipeline completion.
//
// pkg/resultlog already publishes to Redis after every tdtpcli --pipeline
// run configured with result_log.type: redis:
//
//	SET  tdtp:pipeline:<result_name>:state  <JSON>  EX <ttl>  — for polling
//	PUB  tdtp:pipeline:<result_name>        <JSON>             — for event-driven routing
//
// (see examples/07-redis-orchestration for the reference Python consumer).
// That side was already built; the orchestrator subscribing to it was not.
// Subscriber closes that gap: it PSUBSCRIBEs to tdtp:pipeline:*, matches
// each event's result_name against a static, admin-configured mapping (never
// the message content itself — a scenario name is never read off the wire),
// and triggers the mapped scenario through the exact same executor.Submit
// path as cron and manual activation — so approval, trust gates, and audit
// all apply identically, no special-casing for this trigger source.
//
// The event payload's shape (pkg/resultlog/event.PipelineResult) is imported
// from its own leaf package rather than pkg/resultlog directly — pkg/resultlog
// pulls in pkg/etl for its publisher side, and pkg/etl statically links
// kafka-go, RabbitMQ, and excelize even though none of that is reachable
// from here. Importing the leaf package alone keeps this binary lean.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"github.com/ruslano69/tdtp-framework/pkg/resultlog/event"
)

// pipelineEventPattern matches every pipeline's completion channel, per the
// pkg/resultlog convention — one subscription covers all pipelines; adding
// a new one needs no orchestrator config beyond a pubsub.yaml entry.
const pipelineEventPattern = "tdtp:pipeline:*"

// SubscriptionDef maps one pipeline's result_name to a scenario to trigger.
type SubscriptionDef struct {
	ResultName string            `yaml:"result_name"`
	Scenario   string            `yaml:"scenario"`
	OnStatus   []string          `yaml:"on_status"` // default: ["success"]
	Params     map[string]string `yaml:"params"`    // static; merged with pipeline_name/result_name/status
}

type pubsubFile struct {
	Subscriptions []SubscriptionDef `yaml:"subscriptions"`
}

// LoadPubSub reads a pubsub.yaml file. Missing on_status defaults to
// ["success"] — reacting to failures/completed_with_errors is opt-in, not
// the default, since a scenario firing on every failed upstream run is
// rarely what an admin actually wants without thinking about it first.
func LoadPubSub(path string) ([]SubscriptionDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pubsub: read %s: %w", path, err)
	}
	var pf pubsubFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("pubsub: parse %s: %w", path, err)
	}
	for i := range pf.Subscriptions {
		if pf.Subscriptions[i].ResultName == "" || pf.Subscriptions[i].Scenario == "" {
			return nil, fmt.Errorf("pubsub: %s: subscription %d missing result_name or scenario", path, i)
		}
		if len(pf.Subscriptions[i].OnStatus) == 0 {
			pf.Subscriptions[i].OnStatus = []string{"success"}
		}
	}
	return pf.Subscriptions, nil
}

// ValidatePubSubScenarios checks that every subscription's scenario is
// actually loaded, the same fail-fast-at-startup treatment as runners get.
func ValidatePubSubScenarios(subs []SubscriptionDef, scenes map[string]*Scenario) error {
	for _, s := range subs {
		if _, ok := scenes[s.Scenario]; !ok {
			return fmt.Errorf("pubsub subscription for result_name %q references unknown scenario %q", s.ResultName, s.Scenario)
		}
	}
	return nil
}

func statusAllowed(status string, allowed []string) bool {
	for _, a := range allowed {
		if a == status {
			return true
		}
	}
	return false
}

// Subscriber listens for pipeline-completion events and triggers the
// scenarios subscribed to them. It runs for the lifetime of the orchestrator
// process, reconnecting with backoff if the Redis connection drops.
type Subscriber struct {
	client    *redis.Client
	subs      map[string]SubscriptionDef // result_name -> def
	scenes    map[string]*Scenario
	executor  *Executor
	gate      *TrustGate
	db        *OrchestratorDB
	connected atomic.Bool
}

func NewSubscriber(client *redis.Client, subs []SubscriptionDef, scenes map[string]*Scenario, executor *Executor, gate *TrustGate, db *OrchestratorDB) *Subscriber {
	byName := make(map[string]SubscriptionDef, len(subs))
	for _, s := range subs {
		byName[s.ResultName] = s
	}
	return &Subscriber{client: client, subs: byName, scenes: scenes, executor: executor, gate: gate, db: db}
}

// Connected reports whether the subscriber currently has a live Redis
// connection — surfaced on /healthz so a dead broker isn't silent.
func (s *Subscriber) Connected() bool { return s.connected.Load() }

// Run blocks, reconnecting with exponential backoff (capped at 30s) until
// ctx is cancelled. Intended to be launched in its own goroutine.
func (s *Subscriber) Run(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := s.runOnce(ctx); err != nil {
			s.connected.Store(false)
			log.Warn().Err(err).Msg("pubsub subscriber lost connection, retrying")
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second // clean exit (ctx cancelled) — no need to keep backoff state
	}
}

func (s *Subscriber) runOnce(ctx context.Context) error {
	pubsub := s.client.PSubscribe(ctx, pipelineEventPattern)
	defer func() { _ = pubsub.Close() }()

	if _, err := pubsub.Receive(ctx); err != nil {
		return fmt.Errorf("pubsub: subscribe: %w", err)
	}
	s.connected.Store(true)
	log.Info().Str("pattern", pipelineEventPattern).Msg("pubsub subscriber connected")

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return fmt.Errorf("pubsub: channel closed")
			}
			s.dispatch(msg.Payload)
		}
	}
}

// dispatch resolves and triggers the scenario for one pipeline-completion
// event. Never trusts the message for anything but the result_name lookup
// key and the small set of fields templated into scenario params — no field
// in the payload can name a scenario or bypass approval/trust-gate checks.
func (s *Subscriber) dispatch(payload string) {
	var result event.PipelineResult
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		log.Warn().Err(err).Msg("pubsub: malformed pipeline event, ignoring")
		return
	}

	def, ok := s.subs[result.ResultName]
	if !ok {
		return // no subscription cares about this result_name
	}
	if !statusAllowed(result.Status, def.OnStatus) {
		return
	}
	scene, ok := s.scenes[def.Scenario]
	if !ok {
		log.Error().Str("scenario", def.Scenario).Str("result_name", result.ResultName).
			Msg("pubsub: subscription references a scenario that is no longer loaded")
		return
	}
	if err := VerifyScenarioChecksum(s.db, scene); err != nil {
		log.Warn().Err(err).Str("scenario", def.Scenario).Msg("pubsub: scenario not approved, skipping trigger")
		return
	}
	if s.gate != nil {
		if err := s.gate.GateScenario(scene); err != nil {
			log.Warn().Err(err).Str("scenario", def.Scenario).Msg("pubsub: trust gate refused scenario, skipping trigger")
			return
		}
	}

	params := make(map[string]string, len(def.Params)+3)
	for k, v := range def.Params {
		params[k] = v
	}
	params["pipeline_name"] = result.PipelineName
	params["result_name"] = result.ResultName
	params["status"] = result.Status

	resolved, err := scene.ValidateParams(params)
	if err != nil {
		log.Warn().Err(err).Str("scenario", def.Scenario).Msg("pubsub: param validation failed, skipping trigger")
		return
	}
	if _, err := s.executor.Submit(scene, resolved, "", ""); err != nil {
		log.Error().Err(err).Str("scenario", def.Scenario).Msg("pubsub: submit failed")
	}
}
