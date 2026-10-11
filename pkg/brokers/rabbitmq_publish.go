package brokers

// rabbitmq_publish.go — publishing with an explicit route, message id and,
// when asked for, publisher confirms.
//
// Send (the original path, used by export-broker and the rest) is unchanged in
// behaviour: no confirms, the configured exchange and key, application/xml.
// Anything that needs to know a message was accepted — the orchestrator's event
// sink — opts in with PublisherConfirms and calls PublishWith.

import (
	"context"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Publication is one message for PublishWith. A zero Exchange or RoutingKey
// falls back to the broker's configuration; a zero ContentType is
// application/xml, which is what Send has always used.
type Publication struct {
	Exchange    string
	RoutingKey  string
	ContentType string
	MessageID   string
	Body        []byte
}

var (
	// ErrUnroutable: the broker accepted the message but no queue took it. The
	// exchange has no binding for this key, or the queue is gone.
	ErrUnroutable = errors.New("message not routed to any queue")
	// ErrNacked: the broker itself refused the message.
	ErrNacked = errors.New("broker refused the message")
)

// Send publishes to the configured exchange and key, as it always has.
func (r *RabbitMQ) Send(ctx context.Context, message []byte) error {
	return r.PublishWith(ctx, Publication{Body: message})
}

// PublishWith publishes one message. With PublisherConfirms set it returns nil
// only after the broker confirmed the message and routed it to a queue.
//
// The publish is mandatory in confirm mode: a message no queue can take comes
// back as a return, and is reported as ErrUnroutable rather than acknowledged
// and dropped. That is the default exchange's silent failure, and it is what
// the confirm wait exists to catch.
func (r *RabbitMQ) PublishWith(ctx context.Context, p Publication) error {
	if r.channel == nil {
		return fmt.Errorf("not connected to RabbitMQ")
	}
	if p.Exchange == "" {
		p.Exchange = r.config.Exchange
	}
	if p.RoutingKey == "" {
		p.RoutingKey = r.config.RoutingKey
		if p.RoutingKey == "" {
			p.RoutingKey = r.config.Queue
		}
	}
	if p.ContentType == "" {
		p.ContentType = "application/xml" // TDTP packets are XML
	}

	confirmed := r.config.PublisherConfirms
	var want uint64
	if confirmed {
		// Returns for an earlier, abandoned publish may still be waiting in
		// the buffer; they are not this message's, so drop them first.
		drainReturns(r.returns)
		want = r.channel.GetNextPublishSeqNo()
	}

	err := r.channel.PublishWithContext(ctx, p.Exchange, p.RoutingKey, confirmed, false, amqp.Publishing{
		ContentType:  p.ContentType,
		Body:         p.Body,
		DeliveryMode: amqp.Persistent, // survives a broker restart
		Timestamp:    time.Now(),
		MessageId:    p.MessageID,
	})
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	if !confirmed {
		return nil
	}
	return r.awaitConfirm(ctx, want)
}

// awaitConfirm waits for the confirmation of publish number want. Confirmations
// of earlier publishes that were abandoned by a timeout arrive first and are
// skipped: each publish is judged by its own tag, never by whichever ack
// happens to come next.
func (r *RabbitMQ) awaitConfirm(ctx context.Context, want uint64) error {
	for {
		select {
		case c, ok := <-r.confirms:
			if !ok {
				return fmt.Errorf("connection closed before the broker confirmed the message")
			}
			if c.DeliveryTag < want {
				continue // an abandoned earlier publish
			}
			if c.DeliveryTag > want {
				return fmt.Errorf("confirmation %d arrived for publish %d", c.DeliveryTag, want)
			}
			if !c.Ack {
				return ErrNacked
			}
			// The broker sends a return before the ack of a message it could
			// not route, so a return waiting now is this message's.
			select {
			case ret := <-r.returns:
				return fmt.Errorf("%w: exchange %q, key %q", ErrUnroutable, ret.Exchange, ret.RoutingKey)
			default:
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("no confirmation from the broker: %w", ctx.Err())
		}
	}
}

func drainReturns(ch <-chan amqp.Return) {
	if ch == nil {
		return
	}
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// enableConfirms switches the channel into confirm mode and keeps the
// confirmation and return channels the publish path waits on.
func (r *RabbitMQ) enableConfirms() error {
	if err := r.channel.Confirm(false); err != nil {
		return fmt.Errorf("enable publisher confirms: %w", err)
	}
	// Buffered: the client delivers in order on its own goroutine, and a
	// publish is synchronous, so one slot is enough for what is outstanding.
	r.confirms = r.channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	r.returns = r.channel.NotifyReturn(make(chan amqp.Return, 1))
	return nil
}

// Connected reports whether the connection and channel are still usable. A
// client that is not connected must be rebuilt: the channel does not recover.
func (r *RabbitMQ) Connected() bool {
	return r.conn != nil && r.channel != nil && !r.conn.IsClosed()
}

// ErrorClass maps an error from Connect or PublishWith to a short failure
// class and whether retrying the same message can help. The class is what an
// operator sees; the error text may name a host or a user, so it stays out.
//
//	auth        ACCESS_REFUSED (403)            permanent
//	not_found   NOT_FOUND (404), exchange gone  transient: may be recreated
//	unroutable  no queue took the message       transient: may be bound
//	nack        the broker refused it           transient
//	timeout     no answer in time               transient
//	network     anything else                   transient
func ErrorClass(err error) (class string, permanent bool) {
	switch {
	case errors.Is(err, ErrUnroutable):
		return "unroutable", false
	case errors.Is(err, ErrNacked):
		return "nack", false
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout", false
	}
	var ae *amqp.Error
	if errors.As(err, &ae) {
		switch ae.Code {
		case amqp.AccessRefused:
			return "auth", true
		case amqp.NotFound:
			return "not_found", false
		}
	}
	return "network", false
}
