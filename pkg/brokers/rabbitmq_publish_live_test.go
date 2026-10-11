//go:build integration

package brokers

// Live tests for confirmed publishing. They need a broker; CI runs them in the
// live adapter job (see .github/workflows/ci.yml) and a skip there fails that
// job. Locally they read the same defaults as CI, or RABBITMQ_TEST_* overrides.
//
// Each test declares its own exchange and queue with a raw amqp connection and
// removes them afterwards, so the tests cannot see each other's messages.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func liveRabbitConfig(t *testing.T) Config {
	t.Helper()
	port := 5672
	if v := os.Getenv("RABBITMQ_TEST_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("RABBITMQ_TEST_PORT: %v", err)
		}
		port = p
	}
	cfg := Config{
		Type:     "rabbitmq",
		Host:     envOr("RABBITMQ_TEST_HOST", "localhost"),
		Port:     port,
		User:     envOr("RABBITMQ_TEST_USER", "tdtp_test"),
		Password: envOr("RABBITMQ_TEST_PASSWORD", "tdtp_test_password"),
		VHost:    "/",
	}
	// Reachability is decided here, once, so every test reports it the same way.
	c, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port))
	if err != nil {
		t.Skipf("RabbitMQ not reachable at %s:%d: %v", cfg.Host, cfg.Port, err)
	}
	_ = c.Close()
	return cfg
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// rawChannel opens a plain amqp channel for setup and inspection.
func rawChannel(t *testing.T, cfg Config) (*amqp.Connection, *amqp.Channel) {
	t.Helper()
	conn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port))
	if err != nil {
		t.Fatalf("raw dial: %v", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		t.Fatalf("raw channel: %v", err)
	}
	return conn, ch
}

// uniqueName keeps parallel or repeated runs off each other's exchanges.
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s.%d", prefix, time.Now().UnixNano())
}

// withTopology declares a direct exchange and a queue bound to it with key, and
// removes both when the test ends.
func withTopology(t *testing.T, cfg Config, exchange, queue, key string) {
	t.Helper()
	conn, ch := rawChannel(t, cfg)
	t.Cleanup(func() {
		c2, ch2 := rawChannel(t, cfg)
		_, _ = ch2.QueueDelete(queue, false, false, false)
		_ = ch2.ExchangeDelete(exchange, false, false)
		_ = ch2.Close()
		_ = c2.Close()
		_ = ch.Close()
		_ = conn.Close()
	})
	if err := ch.ExchangeDeclare(exchange, "direct", false, true, false, false, nil); err != nil {
		t.Fatalf("declare exchange: %v", err)
	}
	if _, err := ch.QueueDeclare(queue, false, true, false, false, nil); err != nil {
		t.Fatalf("declare queue: %v", err)
	}
	if err := ch.QueueBind(queue, key, exchange, false, nil); err != nil {
		t.Fatalf("bind queue: %v", err)
	}
}

// A confirmed publish that a queue takes returns only after the broker has
// routed it, and the message arrives with its id and content type intact.
func TestRabbitMQPublish_LiveConfirmedRoutedMessage(t *testing.T) {
	cfg := liveRabbitConfig(t)
	ex, q := uniqueName("tdtp.pub.test"), uniqueName("tdtp.pub.test.q")
	withTopology(t, cfg, ex, q, "job.finished")

	cfg.Exchange = ex
	cfg.PublishOnly = true // publishing only: the test queue is bound by withTopology
	cfg.PublisherConfirms = true
	b, err := NewRabbitMQ(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = b.Close() }()

	body := []byte(`{"event_id":"ev-42","type":"job.finished"}`)
	if err := b.PublishWith(ctx, Publication{RoutingKey: "job.finished", ContentType: "application/json", MessageID: "ev-42", Body: body}); err != nil {
		t.Fatalf("PublishWith: %v", err)
	}

	conn, ch := rawChannel(t, cfg)
	defer func() { _ = conn.Close() }()
	msg, ok, err := ch.Get(q, true)
	if err != nil || !ok {
		t.Fatalf("queue has no message after a confirmed publish: ok=%v err=%v", ok, err)
	}
	if string(msg.Body) != string(body) || msg.MessageId != "ev-42" || msg.ContentType != "application/json" {
		t.Fatalf("message = body %q id %q type %q", msg.Body, msg.MessageId, msg.ContentType)
	}
}

// Nothing is bound to the key: the broker accepts the publish but no queue
// takes it. Without confirms this is silent; with them it must be an error.
func TestRabbitMQPublish_LiveUnroutableIsReported(t *testing.T) {
	cfg := liveRabbitConfig(t)
	ex, q := uniqueName("tdtp.pub.unroute"), uniqueName("tdtp.pub.unroute.q")
	withTopology(t, cfg, ex, q, "job.finished")

	cfg.Exchange = ex
	cfg.PublishOnly = true // publishing only: the test queue is bound by withTopology
	cfg.PublisherConfirms = true
	b, err := NewRabbitMQ(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = b.Close() }()

	err = b.PublishWith(ctx, Publication{RoutingKey: "job.started", Body: []byte(`{}`)})
	if !errors.Is(err, ErrUnroutable) {
		t.Fatalf("publish to an unbound key: %v; want ErrUnroutable", err)
	}
	if class, perm := ErrorClass(err); class != "unroutable" || perm {
		t.Fatalf("class = %q permanent=%v, want unroutable transient", class, perm)
	}

	// The same connection still works afterwards: an unroutable message does
	// not kill the channel.
	if err := b.PublishWith(ctx, Publication{RoutingKey: "job.finished", Body: []byte(`{}`)}); err != nil {
		t.Fatalf("publish after an unroutable one: %v", err)
	}
}

// publish_only never declares: an exchange that does not exist is refused, and
// the error names the cause as not_found, which is transient.
func TestRabbitMQPublish_LivePublishOnlyNeverDeclares(t *testing.T) {
	cfg := liveRabbitConfig(t)
	missing := uniqueName("tdtp.pub.missing")

	cfg.Exchange = missing
	cfg.PublishOnly = true
	b, err := NewRabbitMQ(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = b.Connect(ctx)
	if err == nil {
		_ = b.Close()
		t.Fatal("Connect succeeded for an exchange that does not exist")
	}
	if class, perm := ErrorClass(err); class != "not_found" || perm {
		t.Fatalf("class = %q permanent=%v, want not_found transient", class, perm)
	}

	// And it really was not created.
	conn, ch := rawChannel(t, cfg)
	defer func() { _ = conn.Close() }()
	if err := ch.ExchangeDeclarePassive(missing, "direct", false, true, false, false, nil); err == nil {
		t.Fatal("publish_only created the exchange")
	}
}

// Wrong credentials are permanent: retrying the same password cannot succeed.
func TestRabbitMQPublish_LiveBadCredentialsIsPermanent(t *testing.T) {
	cfg := liveRabbitConfig(t)
	cfg.Password = "definitely-not-the-password-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	cfg.Exchange = "amq.direct"
	cfg.PublishOnly = true
	b, err := NewRabbitMQ(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = b.Connect(ctx)
	if err == nil {
		_ = b.Close()
		t.Fatal("Connect succeeded with a wrong password")
	}
	if class, perm := ErrorClass(err); class != "auth" || !perm {
		t.Fatalf("class = %q permanent=%v, want auth permanent (err: %v)", class, perm, err)
	}
}
