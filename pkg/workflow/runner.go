package workflow

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"time"
	"unicode"
)

var reStepVar = regexp.MustCompile(`\{\{(\w+)\}\}`)

// ApplyVars replaces {{name}} placeholders with values from vars.
// Exported so callers (e.g. RunSteps) can substitute descriptions before printing.
func ApplyVars(s string, vars map[string]string) string {
	return applyVars(s, vars)
}

// applyVars replaces {{name}} placeholders in a step command string.
func applyVars(s string, vars map[string]string) string {
	if len(vars) == 0 {
		return s
	}
	return reStepVar.ReplaceAllStringFunc(s, func(match string) string {
		m := reStepVar.FindStringSubmatch(match)
		if val, ok := vars[m[1]]; ok {
			return val
		}
		return match
	})
}

// Run executes the workflow using Kahn's topological-sort algorithm.
// Steps in the same "wave" (no ordering constraint between them) run in
// parallel. The next wave starts only when all steps of the current wave
// have finished.
//
// Error propagation rules:
//   - on_error:stop  — abort immediately; dependents never run.
//   - on_error:skip  — mark step as skipped; direct and transitive dependents
//     are also skipped (they print a one-line notice instead of running).
//   - on_error:retry(N) — retry up to N times with exponential back-off (2s→30s).
//     If all retries are exhausted, the step is treated as on_error:stop.
func Run(ctx context.Context, cfg *WorkflowConfig, vars map[string]string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}

	// Substitute vars in the description so callers that print it get the resolved value.
	cfg.Description = applyVars(cfg.Description, vars)

	// Build Kahn's bookkeeping structures.
	inDegree := make(map[string]int, len(cfg.Steps))
	dependents := make(map[string][]string) // id → ids of steps that depend on it

	stepByID := make(map[string]StepConfig, len(cfg.Steps))
	for _, s := range cfg.Steps {
		stepByID[s.ID] = s
		if _, ok := inDegree[s.ID]; !ok {
			inDegree[s.ID] = 0
		}
		for _, dep := range s.DependsOn {
			inDegree[s.ID]++
			dependents[dep] = append(dependents[dep], s.ID)
		}
	}

	// Seed the initial wave.
	var ready []string
	for _, s := range cfg.Steps {
		if inDegree[s.ID] == 0 {
			ready = append(ready, s.ID)
		}
	}

	done := make(map[string]bool, len(cfg.Steps))
	skipped := make(map[string]bool)

	type waveResult struct {
		id             string
		err            error
		skipPropagated bool // true when skipped due to ancestor skip, not own failure
	}

	for len(ready) > 0 {
		wave := ready
		ready = nil

		results := make(chan waveResult, len(wave))
		var wg sync.WaitGroup

		// waveCtx is cancelled when any step fails with on_error:stop (or exhausted
		// retry). This kills the subprocesses of still-running parallel steps instead
		// of waiting for their own timeouts to expire.
		waveCtx, waveCancel := context.WithCancel(ctx)

		for _, id := range wave {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				step := stepByID[id]

				// Propagate skip from any ancestor in this step's deps.
				// All deps are done before this wave starts — safe to read skipped.
				for _, dep := range step.DependsOn {
					if skipped[dep] {
						results <- waveResult{id: id, skipPropagated: true}
						return
					}
				}

				err := runStep(waveCtx, exe, step, vars)
				// If this step's failure would stop the workflow, cancel the wave
				// context so other goroutines' subprocesses exit immediately rather
				// than running to their own timeouts.
				if err != nil {
					policy, _ := ParseOnError(step.OnError)
					if policy.Action != "skip" {
						waveCancel()
					}
				}
				results <- waveResult{id: id, err: err}
			}(id)
		}

		wg.Wait()
		waveCancel() // always release the cancel func
		close(results)

		// Process results in the main goroutine — only place that writes to skipped.
		for r := range results {
			done[r.id] = true
			step := stepByID[r.id]

			if r.skipPropagated {
				skipped[r.id] = true
				fmt.Printf("[steps] ⏭  %s — skipped (ancestor was skipped)\n", r.id)
				// Update in-degrees of dependents even on skip so the DAG drains.
				for _, dep := range dependents[r.id] {
					inDegree[dep]--
					if inDegree[dep] == 0 {
						ready = append(ready, dep)
					}
				}
				continue
			}

			if r.err != nil {
				policy, _ := ParseOnError(step.OnError)
				if policy.Action == "skip" {
					skipped[r.id] = true
					fmt.Printf("[steps] ⚠  %s — failed, continuing (on_error: skip): %v\n", r.id, r.err)
				} else {
					return fmt.Errorf("step %q failed: %w", r.id, r.err)
				}
			}

			for _, dep := range dependents[r.id] {
				inDegree[dep]--
				if inDegree[dep] == 0 {
					ready = append(ready, dep)
				}
			}
		}
	}

	// Any step still not done means there is a cycle.
	for _, s := range cfg.Steps {
		if !done[s.ID] {
			return fmt.Errorf("workflow has a cycle or unresolvable dependency (step %q never ran)", s.ID)
		}
	}
	return nil
}

// runStep executes one step, respecting the retry policy from on_error.
// Each attempt runs the tdtpcli binary as a subprocess with the step's command
// as the argument list. stdout/stderr pass through directly.
func runStep(ctx context.Context, exe string, step StepConfig, vars map[string]string) error {
	policy, _ := ParseOnError(step.OnError)
	maxAttempts := 1
	if policy.Action == "retry" {
		maxAttempts = 1 + policy.Retries
	}

	resolved := applyVars(step.Command, vars)
	args, err := tokenize(resolved)
	if err != nil {
		return fmt.Errorf("parse command: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			// Exponential back-off: 2s, 4s, 8s, … capped at 30s.
			delaySec := math.Min(float64(int(2)<<uint(attempt-2)), 30)
			delay := time.Duration(delaySec) * time.Second
			fmt.Printf("[steps] ↺  %s — retry %d/%d in %s\n", step.ID, attempt-1, policy.Retries, delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		fmt.Printf("[steps] ▶  %s: %s\n", step.ID, resolved)
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		lastErr = cmd.Run()
		if lastErr == nil {
			fmt.Printf("[steps] ✓  %s\n", step.ID)
			return nil
		}
		fmt.Printf("[steps] ✗  %s: %v\n", step.ID, lastErr)
	}
	return lastErr
}

// tokenize splits a command string into argument tokens, respecting single and
// double quotes (same semantics as POSIX shell word-splitting without variable
// expansion). Examples:
//
//	"--pipeline foo.yaml"         → ["--pipeline", "foo.yaml"]
//	`--where "Status = 0"`        → ["--where", "Status = 0"]
//	`--where 'ім'\''я'`           → not attempted — use double quotes instead
func tokenize(s string) ([]string, error) {
	var tokens []string
	var cur []byte
	inSingle, inDouble := false, false

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			} else {
				cur = append(cur, c)
			}
		case inDouble:
			switch {
			case c == '"':
				inDouble = false
			case c == '\\' && i+1 < len(s):
				i++
				cur = append(cur, s[i])
			default:
				cur = append(cur, c)
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case unicode.IsSpace(rune(c)):
			if len(cur) > 0 {
				tokens = append(tokens, string(cur))
				cur = cur[:0]
			}
		default:
			cur = append(cur, c)
		}
	}

	if inSingle {
		return nil, fmt.Errorf("unclosed single quote in command: %q", s)
	}
	if inDouble {
		return nil, fmt.Errorf("unclosed double quote in command: %q", s)
	}
	if len(cur) > 0 {
		tokens = append(tokens, string(cur))
	}
	return tokens, nil
}
