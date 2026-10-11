package main

// events_rabbitmq.go — the RabbitMQ sink: each event is published to an
// exchange the administrator has already declared, with publisher confirms.
//
// An event is "sent" only when the broker has routed it to a queue. A publish
// nobody can route is an error (unroutable), not a silent success: the default
// exchange would drop it and say nothing.
//
// The sink never declares anything. publish_only means the exchange must exist
// and the administrator binds the queues. A publisher that declared its own
// routes would create paths nobody reviewed.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/brokers"
)

// rabbitmqSinkDef is the rabbitmq: block of a sink. user and password are
// references (env:VARIABLE), never literals, like the webhook secret.
type rabbitmqSinkDef struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`        // empty = 5672, or 5671 with use_tls
	User       string `yaml:"user"`        // env:VARIABLE
	Password   string `yaml:"password"`    // env:VARIABLE
	VHost      string `yaml:"vhost"`       // empty = /
	Exchange   string `yaml:"exchange"`    // must already exist
	RoutingKey string `yaml:"routing_key"` // empty = the event type
	UseTLS     bool   `yaml:"use_tls"`     // amqps, certificate verified
}

func validateRabbitMQDef(def EventSinkDef) error {
	r := def.RabbitMQ
	if r == nil {
		return errors.New("rabbitmq block is required for a rabbitmq sink")
	}
	if r.Host == "" {
		return errors.New("rabbitmq.host is required")
	}
	if r.Port < 0 || r.Port > 65535 {
		return errors.New("rabbitmq.port is out of range")
	}
	if r.Exchange == "" {
		return errors.New("rabbitmq.exchange is required: the sink publishes to an exchange the administrator declared")
	}
	if err := requireEnvRef(def.Name, "rabbitmq.user", r.User); err != nil {
		return err
	}
	return requireEnvRef(def.Name, "rabbitmq.password", r.Password)
}

// newRabbitMQSink resolves the credentials and builds the sink. The broker
// connection is made on the first send, so a broker that is down at startup
// does not stop the orchestrator; its events wait in the outbox.
func newRabbitMQSink(def EventSinkDef) (*rabbitmqSink, error) {
	if err := validateRabbitMQDef(def); err != nil {
		return nil, fmt.Errorf("sink %q: %w", def.Name, err)
	}
	user, err := resolveEnvRef(def.Name, "rabbitmq.user", def.RabbitMQ.User)
	if err != nil {
		return nil, err
	}
	pass, err := resolveEnvRef(def.Name, "rabbitmq.password", def.RabbitMQ.Password)
	if err != nil {
		return nil, err
	}
	r := def.RabbitMQ
	port := r.Port
	if port == 0 {
		port = 5672
		if r.UseTLS {
			port = 5671
		}
	}
	vhost := r.VHost
	if vhost == "" {
		vhost = "/"
	}
	return &rabbitmqSink{
		name:       def.Name,
		routingKey: r.RoutingKey,
		cfg: brokers.Config{
			Type:              "rabbitmq",
			Host:              r.Host,
			Port:              port,
			User:              user,
			Password:          pass,
			VHost:             vhost,
			UseTLS:            r.UseTLS,
			Exchange:          r.Exchange,
			PublishOnly:       true,
			PublisherConfirms: true,
		},
	}, nil
}

type rabbitmqSink struct {
	name       string
	routingKey string // empty: the event type is the routing key
	cfg        brokers.Config
	broker     *brokers.RabbitMQ // nil until the first send, or after a lost connection
}

// Send publishes one event. A connection that has died is rebuilt on the next
// call: an amqp channel does not recover from a dropped connection.
func (s *rabbitmqSink) Send(ctx context.Context, ev OutboxEvent) error {
	if s.broker == nil || !s.broker.Connected() {
		if err := s.connect(ctx); err != nil {
			return err
		}
	}
	key := s.routingKey
	if key == "" {
		key = ev.Type
	}
	err := s.broker.PublishWith(ctx, brokers.Publication{
		RoutingKey:  key,
		ContentType: "application/json",
		MessageID:   ev.ID,
		Body:        ev.Payload,
	})
	if err != nil {
		if !s.broker.Connected() {
			s.drop()
		}
		return sinkErrorFrom(err)
	}
	return nil
}

func (s *rabbitmqSink) connect(ctx context.Context) error {
	b, err := brokers.NewRabbitMQ(s.cfg)
	if err != nil {
		return &SinkError{Class: "other", Permanent: true, Err: err}
	}
	if err := b.Connect(ctx); err != nil {
		return sinkErrorFrom(err)
	}
	s.broker = b
	return nil
}

func (s *rabbitmqSink) drop() {
	if s.broker != nil {
		_ = s.broker.Close()
		s.broker = nil
	}
}

// Close releases the connection at shutdown. The dispatcher calls it after its
// last pass, so no send is in flight.
func (s *rabbitmqSink) Close() error {
	s.drop()
	return nil
}

// sinkErrorFrom turns a broker error into the dispatcher's classified error.
// The class comes from pkg/brokers; the text stays with the error for the log.
func sinkErrorFrom(err error) error {
	class, permanent := brokers.ErrorClass(err)
	return &SinkError{Class: class, Permanent: permanent, Err: err}
}

// requireEnvRef checks that a field is an env: reference with a name. It does
// not read the environment; that happens at startup, in resolveEnvRef.
func requireEnvRef(sink, field, ref string) error {
	if !strings.HasPrefix(ref, secretEnvPrefix) || len(ref) == len(secretEnvPrefix) {
		return fmt.Errorf("%s must be env:VARIABLE — a literal value is refused so it cannot end up in a file", field)
	}
	return nil
}

// resolveEnvRef returns the value an env: reference names. Only the variable's
// name is ever put in an error, never its value.
func resolveEnvRef(sink, field, ref string) (string, error) {
	if err := requireEnvRef(sink, field, ref); err != nil {
		return "", fmt.Errorf("sink %q: %w", sink, err)
	}
	name := strings.TrimPrefix(ref, secretEnvPrefix)
	val := os.Getenv(name)
	if val == "" {
		return "", fmt.Errorf("sink %q: %s: environment variable %s is not set", sink, field, name)
	}
	return val, nil
}

// compile-time check that the sink can be closed by the dispatcher's shutdown.
var _ io.Closer = (*rabbitmqSink)(nil)
