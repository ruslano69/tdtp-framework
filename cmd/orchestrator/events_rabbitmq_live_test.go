//go:build integration

package main

// Live tests for the RabbitMQ sink, through the real dispatcher. They need a
// broker: CI runs them in the live adapter job, and a skip there fails the job.
// Locally they read the CI defaults, or RABBITMQ_TEST_* overrides.

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// liveMQ reads the broker address and credentials, and skips the test when
// the broker cannot be reached.
func liveMQ(t *testing.T) (host string, port int, user, pass string) {
	t.Helper()
	host = envDefault("RABBITMQ_TEST_HOST", "localhost")
	port = 5672
	if v := os.Getenv("RABBITMQ_TEST_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("RABBITMQ_TEST_PORT: %v", err)
		}
		port = p
	}
	user = envDefault("RABBITMQ_TEST_USER", "tdtp_test")
	pass = envDefault("RABBITMQ_TEST_PASSWORD", "tdtp_test_password")

	c, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/", user, pass, host, port))
	if err != nil {
		t.Skipf("RabbitMQ not reachable at %s:%d: %v", host, port, err)
	}
	_ = c.Close()
	return host, port, user, pass
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// mqTopology declares an exchange and a queue, and removes both at the end.
// bindKey, when set, binds the queue to the exchange with that key.
func mqTopology(t *testing.T, host string, port int, user, pass, exchange, queue, bindKey string) {
	t.Helper()
	conn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/", user, pass, host, port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		t.Fatalf("channel: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ch.QueueDelete(queue, false, false, false)
		_ = ch.ExchangeDelete(exchange, false, false)
		_ = ch.Close()
		_ = conn.Close()
	})
	if err := ch.ExchangeDeclare(exchange, "direct", false, true, false, false, nil); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if _, err := ch.QueueDeclare(queue, false, true, false, false, nil); err != nil {
		t.Fatalf("queue: %v", err)
	}
	if bindKey != "" {
		if err := ch.QueueBind(queue, bindKey, exchange, false, nil); err != nil {
			t.Fatalf("bind: %v", err)
		}
	}
}

// mqSinkFor builds the sink from the same definition an operator would write,
// with credentials coming from the environment as they must in production.
func mqSinkFor(t *testing.T, host string, port int, exchange string) *rabbitmqSink {
	t.Helper()
	t.Setenv("TDTP_LIVE_MQ_USER", envDefault("RABBITMQ_TEST_USER", "tdtp_test"))
	t.Setenv("TDTP_LIVE_MQ_PASS", envDefault("RABBITMQ_TEST_PASSWORD", "tdtp_test_password"))
	s, err := newRabbitMQSink(EventSinkDef{
		Name: "ops",
		Type: "rabbitmq",
		RabbitMQ: &rabbitmqSinkDef{
			Host:     host,
			Port:     port,
			User:     "env:TDTP_LIVE_MQ_USER",
			Password: "env:TDTP_LIVE_MQ_PASS",
			Exchange: exchange,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The sink publishes through the dispatcher: the event arrives in the queue
// with its id, its content type and the event type as routing key.
func TestRabbitMQSink_LiveDeliversThroughDispatcher(t *testing.T) {
	host, port, user, pass := liveMQ(t)
	ex := fmt.Sprintf("tdtp.sink.test.%d", time.Now().UnixNano())
	q := ex + ".q"
	mqTopology(t, host, port, user, pass, ex, q, EventJobFinished)

	db := newScheduleStatusDB(t)
	payload := []byte(`{"event_id":"ev-live-1","type":"job.finished"}`)
	if err := db.AddOutbox([]OutboxEvent{{ID: "ev-live-1", JobID: "j", Sink: "ops", Type: EventJobFinished, Payload: payload, CreatedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	sink := mqSinkFor(t, host, port, ex)
	t.Cleanup(func() { _ = sink.Close() })
	d := newTestDispatcher(db, map[string]EventSink{"ops": sink}, 5)

	if n, err := d.RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("RunOnce = %d, %v; want one delivery", n, err)
	}
	if rows := readOutbox(t, db); rows[0].Status != "sent" {
		t.Fatalf("row status = %s, want sent", rows[0].Status)
	}

	conn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/", user, pass, host, port))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	ch, _ := conn.Channel()
	msg, ok, err := ch.Get(q, true)
	if err != nil || !ok {
		t.Fatalf("no message in the queue: ok=%v err=%v", ok, err)
	}
	if msg.MessageId != "ev-live-1" || msg.ContentType != "application/json" || string(msg.Body) != string(payload) {
		t.Fatalf("message id=%q type=%q body=%s", msg.MessageId, msg.ContentType, msg.Body)
	}
	if msg.RoutingKey != EventJobFinished {
		t.Fatalf("routing key = %q, want the event type", msg.RoutingKey)
	}
}

// Nothing is bound yet: the event is retried, not lost and not marked sent.
// Once the queue is bound, the next pass delivers it. This is the case the
// retry budget exists for: an administrator who binds a queue in time saves the
// events that were waiting.
func TestRabbitMQSink_LiveUnroutableWaitsForBinding(t *testing.T) {
	host, port, user, pass := liveMQ(t)
	ex := fmt.Sprintf("tdtp.sink.wait.%d", time.Now().UnixNano())
	q := ex + ".q"
	mqTopology(t, host, port, user, pass, ex, q, "") // declared, not bound

	db := newScheduleStatusDB(t)
	if err := db.AddOutbox([]OutboxEvent{{ID: "ev-wait-1", JobID: "j", Sink: "ops", Type: EventJobFinished, Payload: []byte(`{}`), CreatedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	sink := mqSinkFor(t, host, port, ex)
	t.Cleanup(func() { _ = sink.Close() })
	d := newTestDispatcher(db, map[string]EventSink{"ops": sink}, 10)

	if _, err := d.RunOnce(context.Background()); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	rows := readOutbox(t, db)
	if rows[0].Status != "pending" || rows[0].Attempts != 1 || rows[0].LastError == nil || *rows[0].LastError != "unroutable" {
		t.Fatalf("unbound: %+v, want pending, one attempt, class unroutable", rows[0])
	}

	// Bind the queue now, the way an administrator would.
	conn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/", user, pass, host, port))
	if err != nil {
		t.Fatal(err)
	}
	ch, _ := conn.Channel()
	if err := ch.QueueBind(q, EventJobFinished, ex, false, nil); err != nil {
		t.Fatal(err)
	}
	_ = ch.Close()
	_ = conn.Close()

	if n, err := d.RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("pass 2 after binding = %d, %v; want delivery", n, err)
	}
	if rows := readOutbox(t, db); rows[0].Status != "sent" {
		t.Fatalf("after binding: status=%s, want sent", rows[0].Status)
	}
}

// A dropped connection is rebuilt by the next send: the channel does not
// recover on its own, and the sink must not stay broken until a restart.
func TestRabbitMQSink_LiveReconnectsAfterDrop(t *testing.T) {
	host, port, user, pass := liveMQ(t)
	ex := fmt.Sprintf("tdtp.sink.redo.%d", time.Now().UnixNano())
	q := ex + ".q"
	mqTopology(t, host, port, user, pass, ex, q, EventJobFinished)

	sink := mqSinkFor(t, host, port, ex)
	t.Cleanup(func() { _ = sink.Close() })
	ctx := context.Background()
	send := func(id string) error {
		return sink.Send(ctx, OutboxEvent{ID: id, JobID: "j", Sink: "ops", Type: EventJobFinished, Payload: []byte(`{}`)})
	}
	if err := send("ev-redo-1"); err != nil {
		t.Fatalf("first send: %v", err)
	}

	// Simulate the connection going away under the sink.
	sink.drop()
	if err := send("ev-redo-2"); err != nil {
		t.Fatalf("send after a drop: %v", err)
	}
}
