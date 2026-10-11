package brokers

import (
	"context"
	"errors"
	"fmt"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ErrorClass decides whether the orchestrator retries an event, so each mapping
// is pinned here: an error that moved from transient to permanent (or back)
// would change what happens to every event that hits it.
func TestErrorClass(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		class     string
		permanent bool
	}{
		{"access refused", &amqp.Error{Code: amqp.AccessRefused, Reason: "ACCESS_REFUSED"}, "auth", true},
		{"access refused, wrapped", fmt.Errorf("connect: %w", &amqp.Error{Code: amqp.AccessRefused}), "auth", true},
		{"not found", &amqp.Error{Code: amqp.NotFound, Reason: "NOT_FOUND"}, "not_found", false},
		{"unroutable", fmt.Errorf("%w: exchange x", ErrUnroutable), "unroutable", false},
		{"nacked", ErrNacked, "nack", false},
		{"deadline", context.DeadlineExceeded, "timeout", false},
		{"canceled", fmt.Errorf("wait: %w", context.Canceled), "timeout", false},
		{"plain network error", errors.New("dial tcp: connection refused"), "network", false},
		{"other amqp code", &amqp.Error{Code: 530, Reason: "NOT_ALLOWED"}, "network", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			class, perm := ErrorClass(c.err)
			if class != c.class || perm != c.permanent {
				t.Fatalf("ErrorClass = %q permanent=%v, want %q permanent=%v", class, perm, c.class, c.permanent)
			}
		})
	}
}

// A publish on a client that was never connected is refused, not a panic.
func TestPublishWith_RefusesWhenUnconnected(t *testing.T) {
	b, err := NewRabbitMQ(Config{Queue: "q", Exchange: "ex", RoutingKey: "rk"})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.PublishWith(context.Background(), Publication{Body: []byte("x")}); err == nil {
		t.Fatal("PublishWith on an unconnected client succeeded")
	}
	if b.Connected() {
		t.Fatal("an unconnected client reports Connected")
	}
}

func TestNewRabbitMQ_PublishOnlyRules(t *testing.T) {
	if _, err := NewRabbitMQ(Config{PublishOnly: true}); err == nil {
		t.Fatal("publish_only without an exchange accepted")
	}
	b, err := NewRabbitMQ(Config{PublishOnly: true, Exchange: "ex"})
	if err != nil {
		t.Fatalf("publish_only with an exchange and no queue: %v", err)
	}
	if err := b.startConsuming(); err == nil {
		t.Fatal("a publish-only client started consuming")
	}
	if _, err := NewRabbitMQ(Config{Exchange: "ex"}); err == nil {
		t.Fatal("a queue-less client without publish_only accepted")
	}
}

// awaitConfirm is driven directly through its channels, so the cases a broker
// rarely produces on demand (a late ack from an abandoned publish, a nack, a
// return) are decided here, deterministically.
func newConfirmHarness(confirms []amqp.Confirmation, returns []amqp.Return) *RabbitMQ {
	cc := make(chan amqp.Confirmation, len(confirms)+1)
	for _, c := range confirms {
		cc <- c
	}
	rc := make(chan amqp.Return, len(returns)+1)
	for _, r := range returns {
		rc <- r
	}
	return &RabbitMQ{confirms: cc, returns: rc}
}

// An ack left over from a publish a timeout abandoned must not be taken for
// the current one. The current publish here is 3; the nack for it decides.
func TestAwaitConfirm_SkipsAbandonedAckAndJudgesOwnTag(t *testing.T) {
	r := newConfirmHarness([]amqp.Confirmation{
		{DeliveryTag: 2, Ack: true},
		{DeliveryTag: 3, Ack: false},
	}, nil)
	if err := r.awaitConfirm(context.Background(), 3); !errors.Is(err, ErrNacked) {
		t.Fatalf("awaitConfirm = %v, want ErrNacked from its own tag", err)
	}
}

func TestAwaitConfirm_AckOfOwnTagIsSuccess(t *testing.T) {
	r := newConfirmHarness([]amqp.Confirmation{{DeliveryTag: 4, Ack: true}}, nil)
	if err := r.awaitConfirm(context.Background(), 4); err != nil {
		t.Fatalf("awaitConfirm = %v, want nil", err)
	}
}

// A return with the ack of a message means nothing routed it.
func TestAwaitConfirm_ReturnBeforeAckIsUnroutable(t *testing.T) {
	r := newConfirmHarness(
		[]amqp.Confirmation{{DeliveryTag: 5, Ack: true}},
		[]amqp.Return{{Exchange: "ex", RoutingKey: "k", ReplyText: "NO_ROUTE"}},
	)
	if err := r.awaitConfirm(context.Background(), 5); !errors.Is(err, ErrUnroutable) {
		t.Fatalf("awaitConfirm = %v, want ErrUnroutable", err)
	}
}

// A confirmation for a tag that is ahead of the publish cannot happen unless
// the protocol state has gone wrong; it must not be read as success.
func TestAwaitConfirm_FutureTagIsAnError(t *testing.T) {
	r := newConfirmHarness([]amqp.Confirmation{{DeliveryTag: 7, Ack: true}}, nil)
	err := r.awaitConfirm(context.Background(), 3)
	if err == nil || errors.Is(err, ErrNacked) || errors.Is(err, ErrUnroutable) {
		t.Fatalf("awaitConfirm = %v, want a protocol error", err)
	}
}

func TestAwaitConfirm_ClosedChannelIsAnError(t *testing.T) {
	cc := make(chan amqp.Confirmation)
	close(cc)
	r := &RabbitMQ{confirms: cc, returns: make(chan amqp.Return)}
	if err := r.awaitConfirm(context.Background(), 1); err == nil {
		t.Fatal("a closed confirm channel was read as success")
	}
}

func TestAwaitConfirm_ContextEndsTheWait(t *testing.T) {
	r := newConfirmHarness(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := r.awaitConfirm(ctx, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("awaitConfirm = %v, want context.Canceled", err)
	}
}
