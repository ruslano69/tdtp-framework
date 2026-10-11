package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/brokers"
)

func rabbitDef(mut func(*rabbitmqSinkDef)) EventSinkDef {
	r := &rabbitmqSinkDef{
		Host:     "mq.internal",
		User:     "env:TDTP_TEST_MQ_USER",
		Password: "env:TDTP_TEST_MQ_PASS",
		Exchange: "tdtp.events",
	}
	if mut != nil {
		mut(r)
	}
	return EventSinkDef{Name: "ops", Type: "rabbitmq", RabbitMQ: r}
}

func TestRabbitMQDef_Validation(t *testing.T) {
	t.Setenv("TDTP_TEST_MQ_USER", "ops")
	t.Setenv("TDTP_TEST_MQ_PASS", "PASSVALUE-from-env")

	cases := []struct {
		name string
		def  EventSinkDef
		want string // "" = valid
	}{
		{"valid", rabbitDef(nil), ""},
		{"no rabbitmq block", EventSinkDef{Name: "ops", Type: "rabbitmq"}, "rabbitmq block is required"},
		{"no host", rabbitDef(func(r *rabbitmqSinkDef) { r.Host = "" }), "host is required"},
		{"no exchange", rabbitDef(func(r *rabbitmqSinkDef) { r.Exchange = "" }), "exchange is required"},
		{"literal password refused", rabbitDef(func(r *rabbitmqSinkDef) { r.Password = "hunter2-literal" }), "rabbitmq.password must be env:VARIABLE"},
		{"literal user refused", rabbitDef(func(r *rabbitmqSinkDef) { r.User = "tdtp" }), "rabbitmq.user must be env:VARIABLE"},
		{"port out of range", rabbitDef(func(r *rabbitmqSinkDef) { r.Port = 70000 }), "port is out of range"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRabbitMQDef(c.def)
			if c.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want substring %q", err, c.want)
			}
			// A refused literal must not come back in the error text.
			if strings.Contains(err.Error(), "hunter2") {
				t.Fatalf("error echoes the literal value: %v", err)
			}
		})
	}
}

// A missing variable is named, its value is never there (there is none), and
// the failure happens at startup, not on the first event.
func TestRabbitMQDef_MissingVariableNamedAtStartup(t *testing.T) {
	t.Setenv("TDTP_TEST_MQ_USER", "ops")
	t.Setenv("TDTP_TEST_MQ_PASS", "")

	defs := []EventSinkDef{rabbitDef(nil)}
	err := prepareSinks(defs)
	if err != nil {
		t.Fatalf("prepareSinks: %v", err)
	}
	if _, err := newRabbitMQSink(defs[0]); err == nil || !strings.Contains(err.Error(), "TDTP_TEST_MQ_PASS") {
		t.Fatalf("missing password: %v; want it to name TDTP_TEST_MQ_PASS", err)
	}
}

func TestRabbitMQSink_ConfigDefaults(t *testing.T) {
	t.Setenv("TDTP_TEST_MQ_USER", "ops")
	t.Setenv("TDTP_TEST_MQ_PASS", "pw")

	s, err := newRabbitMQSink(rabbitDef(nil))
	if err != nil {
		t.Fatal(err)
	}
	cfg := s.cfg
	if cfg.Port != 5672 || cfg.VHost != "/" {
		t.Fatalf("defaults: port=%d vhost=%q, want 5672 and /", cfg.Port, cfg.VHost)
	}
	// Publish-only and confirms are not options here: the sink never declares
	// anything and never reports a send it did not confirm.
	if !cfg.PublishOnly || !cfg.PublisherConfirms {
		t.Fatalf("sink config must be publish-only with confirms: %+v", cfg)
	}
	if cfg.User != "ops" || cfg.Password != "pw" {
		t.Fatal("credentials not resolved from the environment")
	}

	tls, err := newRabbitMQSink(rabbitDef(func(r *rabbitmqSinkDef) { r.UseTLS = true }))
	if err != nil {
		t.Fatal(err)
	}
	if tls.cfg.Port != 5671 || !tls.cfg.UseTLS {
		t.Fatalf("tls default port = %d, want 5671", tls.cfg.Port)
	}
}

func TestRabbitMQFields_RejectedOnOtherSinkTypes(t *testing.T) {
	defs := []EventSinkDef{{Name: "a", Type: "log", RabbitMQ: &rabbitmqSinkDef{Host: "x"}}}
	if err := prepareSinks(defs); err == nil {
		t.Fatal("a log sink with a rabbitmq block was accepted")
	}
	defs = []EventSinkDef{{Name: "a", Type: "webhook", URL: "https://x.example.com/", Secret: "env:S", RabbitMQ: &rabbitmqSinkDef{Host: "x"}}}
	if err := prepareSinks(defs); err == nil {
		t.Fatal("a webhook sink with a rabbitmq block was accepted")
	}
}

// sinkErrorFrom keeps the class and the permanence the broker layer decided.
func TestSinkErrorFrom_PassesClassThrough(t *testing.T) {
	err := sinkErrorFrom(brokers.ErrUnroutable)
	class, perm := classifySinkErr(err)
	if class != "unroutable" || perm {
		t.Fatalf("class=%q permanent=%v, want unroutable transient", class, perm)
	}
}

// The nested rabbitmq: block parses from the events file as an operator writes
// it, and the unknown-key rule covers it too.
func TestLoadEvents_RabbitMQBlockParsesFromYAML(t *testing.T) {
	t.Setenv("TDTP_TEST_MQ_USER", "ops")
	t.Setenv("TDTP_TEST_MQ_PASS", "pw")
	path := filepath.Join(t.TempDir(), "events.yaml")
	body := `sinks:
  - name: ops-queue
    type: rabbitmq
    types: [job.started, job.finished]
    rabbitmq:
      host: mq.internal
      port: 5672
      user: env:TDTP_TEST_MQ_USER
      password: env:TDTP_TEST_MQ_PASS
      vhost: /
      exchange: tdtp.events
      routing_key: job
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	defs, err := LoadEvents(path)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(defs) != 1 || defs[0].RabbitMQ == nil || defs[0].RabbitMQ.Exchange != "tdtp.events" || defs[0].RabbitMQ.RoutingKey != "job" {
		t.Fatalf("parsed %+v", defs)
	}

	bad := strings.Replace(body, "routing_key: job", "routing_keys: job", 1)
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEvents(path); err == nil || !strings.Contains(err.Error(), "routing_keys") {
		t.Fatalf("misspelled rabbitmq key accepted: %v", err)
	}
}
