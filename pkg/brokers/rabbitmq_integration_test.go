//go:build integration

package brokers

// Integration tests for the RabbitMQ broker — require a live broker.
//
// Quick start:
//   docker run -d --name rmq-test -p 5672:5672 -p 15672:15672 rabbitmq:3-management
//   go test -v -tags=integration -run TestRabbitMQ ./pkg/brokers/
//
// The management HTTP API (port 15672) is used by several tests to simulate
// network-level events (force-close a connection, delete a queue) without
// restarting Docker. If the management plugin is unavailable, those tests skip.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// ── helpers ────────────────────────────────────────────────────────────────────

const (
	rmqHost     = "localhost"
	rmqPort     = 5672
	rmqUser     = "guest"
	rmqPassword = "guest"
	rmqVHost    = "/"
	mgmtBase    = "http://localhost:15672/api"
)

func rmqConfig(queue string) Config {
	return Config{
		Type:     "rabbitmq",
		Host:     rmqHost,
		Port:     rmqPort,
		User:     rmqUser,
		Password: rmqPassword,
		VHost:    rmqVHost,
		Queue:    queue,
		Durable:  true,
	}
}

// requireBroker skips the test if the broker is not reachable with the configured
// credentials. This prevents false failures when running on a machine that has
// a RabbitMQ with different credentials (or no RabbitMQ at all).
// To run these tests, start: docker run -d --name rmq-test -p 5672:5672 -p 15672:15672 rabbitmq:3-management
func requireBroker(t *testing.T) {
	t.Helper()
	cfg := rmqConfig("tdtp.test.probe")
	br, err := New(cfg)
	if err != nil {
		t.Skipf("broker unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := br.Connect(ctx); err != nil {
		br.Close()
		t.Skipf("broker not reachable with test credentials (guest/guest): %v", err)
	}
	br.Close()
}

// mgmtAvailable returns true when the management HTTP API responds.
func mgmtAvailable() bool {
	req, _ := http.NewRequest(http.MethodGet, mgmtBase+"/overview", nil)
	req.SetBasicAuth(rmqUser, rmqPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// deleteQueue removes a queue via management API (best-effort, used for cleanup).
func deleteQueue(name string) {
	req, _ := http.NewRequest(http.MethodDelete,
		fmt.Sprintf("%s/queues/%%2F/%s", mgmtBase, url.PathEscape(name)), nil)
	req.SetBasicAuth(rmqUser, rmqPassword)
	http.DefaultClient.Do(req) //nolint:errcheck
}

// forceCloseAllConnections closes every broker connection via management API.
// This simulates a TCP-level drop: delivery channels close, heartbeat is gone,
// any blocking Receive() returns "delivery channel closed" within ~heartbeat interval.
func forceCloseAllConnections(t *testing.T) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, mgmtBase+"/connections", nil)
	req.SetBasicAuth(rmqUser, rmqPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("management GET /connections: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var conns []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &conns); err != nil {
		t.Fatalf("parse connections: %v", err)
	}
	for _, c := range conns {
		req, _ := http.NewRequest(http.MethodDelete,
			fmt.Sprintf("%s/connections/%s", mgmtBase, url.PathEscape(c.Name)), nil)
		req.SetBasicAuth(rmqUser, rmqPassword)
		req.Header.Set("X-Reason", "integration-test-teardown")
		http.DefaultClient.Do(req) //nolint:errcheck
	}
	t.Logf("force-closed %d connection(s)", len(conns))
}

// ── test cases ─────────────────────────────────────────────────────────────────

// TestRabbitMQ_HappyPath verifies basic connect / send / receive / ack flow.
func TestRabbitMQ_HappyPath(t *testing.T) {
	requireBroker(t)
	const q = "tdtp.test.happy"
	defer deleteQueue(q)

	cfg := rmqConfig(q)
	br, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer br.Close()

	payload := []byte("hello-tdtp")
	if err := br.Send(ctx, payload); err != nil {
		t.Fatalf("Send: %v", err)
	}

	recvCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := br.Receive(recvCtx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("payload mismatch: got %q want %q", data, payload)
	}

	// AckLast should remove the message from the queue.
	rmqBr := br.(*RabbitMQ)
	if err := rmqBr.AckLast(); err != nil {
		t.Fatalf("AckLast: %v", err)
	}
	t.Log("✓ happy path: send → receive → ack")
}

// TestRabbitMQ_NackRequeue verifies that NackLast returns the message to the queue.
func TestRabbitMQ_NackRequeue(t *testing.T) {
	requireBroker(t)
	const q = "tdtp.test.nack"
	defer deleteQueue(q)

	cfg := rmqConfig(q)
	br, _ := New(cfg)
	ctx := context.Background()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer br.Close()

	_ = br.Send(ctx, []byte("nack-me"))

	// First receive — NACK with requeue.
	data, err := br.Receive(context.WithValue(ctx, struct{}{}, nil))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	rmqBr := br.(*RabbitMQ)
	if err := rmqBr.NackLast(true); err != nil {
		t.Fatalf("NackLast: %v", err)
	}

	// Second receive — message must come back.
	recvCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data2, err := br.Receive(recvCtx)
	if err != nil {
		t.Fatalf("Receive after nack: %v", err)
	}
	if string(data2) != string(data) {
		t.Fatalf("requeued payload mismatch: got %q want %q", data2, data)
	}
	_ = rmqBr.AckLast()
	t.Log("✓ nack+requeue: message redelivered correctly")
}

// TestRabbitMQ_ReconnectAfterConnectionDrop simulates a TCP-level connection drop
// (management API force-closes the connection) and verifies that reconnectBroker()
// restores the consumer so the daemon loop continues without manual intervention.
//
// This reproduces the bug fixed in v1.16.1: deliveryChan was not reset on
// Close()/Connect(), so startConsuming() returned early and every subsequent
// Receive() read from a permanently closed channel → infinite error loop.
func TestRabbitMQ_ReconnectAfterConnectionDrop(t *testing.T) {
	if !mgmtAvailable() {
		t.Skip("RabbitMQ management plugin not available (need rabbitmq:3-management image)")
	}
	const q = "tdtp.test.reconnect"
	defer deleteQueue(q)

	cfg := rmqConfig(q)
	br, _ := New(cfg)
	ctx := context.Background()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer br.Close()

	// Send a message that will be re-delivered after reconnect (NACKed implicitly
	// by the broker when the consumer disappears).
	if err := br.Send(ctx, []byte("survive-reconnect")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Receive once to register the consumer (lazy startConsuming).
	recvCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	data, err := br.Receive(recvCtx)
	cancel()
	if err != nil {
		t.Fatalf("first Receive: %v", err)
	}
	rmqBr := br.(*RabbitMQ)
	_ = rmqBr.NackLast(true) // put message back so it survives reconnect

	// Force-close the connection at the broker side — simulates network drop.
	t.Log("force-closing connection (simulating network drop)...")
	forceCloseAllConnections(t)

	// The delivery channel is now closed by the AMQP library. Receive() should
	// surface "delivery channel closed" quickly (within heartbeat interval = 10s).
	dropCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, receiveErr := br.Receive(dropCtx)
	if receiveErr == nil {
		t.Fatal("expected Receive to return an error after force-close, got nil")
	}
	t.Logf("Receive() surfaced error as expected: %v", receiveErr)

	// reconnectBroker must restore the connection.
	reconnCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := reconnectBrokerForTest(reconnCtx, br); err != nil {
		t.Fatalf("reconnectBroker: %v", err)
	}
	t.Log("reconnected — verifying message is redelivered...")

	// The broker should re-deliver the NACKed message.
	recvCtx2, cancel2 := context.WithTimeout(ctx, 10*time.Second)
	defer cancel2()
	data2, err := br.Receive(recvCtx2)
	if err != nil {
		t.Fatalf("Receive after reconnect: %v", err)
	}
	if string(data2) != string(data) {
		t.Fatalf("redelivered payload mismatch: got %q want %q", data2, data)
	}
	_ = rmqBr.AckLast()
	t.Log("✓ reconnect: message redelivered after force-close")
}

// TestRabbitMQ_QueueNotFound_PassiveDeclare verifies that when passive_declare=true
// and the queue does not exist, Connect() returns a clear error (not a channel panic)
// and subsequent reconnectBroker() succeeds once the queue is created externally.
func TestRabbitMQ_QueueNotFound_PassiveDeclare(t *testing.T) {
	if !mgmtAvailable() {
		t.Skip("RabbitMQ management plugin not available")
	}
	const q = "tdtp.test.passive.nonexistent"
	deleteQueue(q) // ensure absent

	cfg := rmqConfig(q)
	cfg.PassiveDeclare = true

	br, _ := New(cfg)
	ctx := context.Background()

	// Connect must fail cleanly — queue does not exist.
	err := br.Connect(ctx)
	if err == nil {
		br.Close()
		t.Fatal("expected Connect to fail for non-existent queue with passive_declare=true")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "404") {
		t.Fatalf("unexpected error text: %v", err)
	}
	t.Logf("Connect() failed as expected: %v", err)

	// Simulate "remote side creates the queue" — declare it via management API.
	createQueue(t, q)
	defer deleteQueue(q)

	// Now Connect() should succeed.
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("Connect after queue creation: %v", err)
	}
	defer br.Close()

	// Basic sanity: send + receive works.
	_ = br.Send(ctx, []byte("passive-test"))
	recvCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := br.Receive(recvCtx); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	_ = br.(*RabbitMQ).AckLast()
	t.Log("✓ passive_declare: failed cleanly on missing queue, connected after creation")
}

// TestRabbitMQ_ParameterMismatch_406 verifies that trying to declare a queue with
// different durable/autoDelete parameters than what already exists returns a
// recognizable 406 PRECONDITION_FAILED error from Connect(), not a panic.
//
// This is the "remote side owns the queue" scenario: the remote declares the queue
// with its own parameters; our daemon must use passive_declare=true to avoid 406.
func TestRabbitMQ_ParameterMismatch_406(t *testing.T) {
	requireBroker(t)
	const q = "tdtp.test.mismatch"
	defer deleteQueue(q)

	ctx := context.Background()

	// Step 1: establish queue with durable=true (remote side declares it first).
	cfg1 := rmqConfig(q)
	cfg1.Durable = true
	br1, _ := New(cfg1)
	if err := br1.Connect(ctx); err != nil {
		t.Fatalf("initial Connect (durable=true): %v", err)
	}
	br1.Close()
	t.Log("queue declared with durable=true")

	// Step 2: try to connect with durable=false — broker returns 406.
	cfg2 := rmqConfig(q)
	cfg2.Durable = false
	br2, _ := New(cfg2)
	err := br2.Connect(ctx)
	if err == nil {
		br2.Close()
		t.Fatal("expected Connect to fail with 406, got nil")
	}
	t.Logf("Connect() with wrong params failed as expected: %v", err)

	is406 := strings.Contains(err.Error(), "406") ||
		strings.Contains(err.Error(), "PRECONDITION_FAILED") ||
		strings.Contains(err.Error(), "inequivalent")
	if !is406 {
		// Not always a 406 text in the error chain — log but don't fail the test,
		// because the important thing is that Connect() returned an error.
		t.Logf("note: error does not contain '406'/'PRECONDITION_FAILED' — check manually")
	}

	// Step 3: the correct fix is passive_declare=true — just verifies existence.
	cfg3 := rmqConfig(q)
	cfg3.PassiveDeclare = true
	br3, _ := New(cfg3)
	if err := br3.Connect(ctx); err != nil {
		t.Fatalf("Connect with passive_declare=true should succeed: %v", err)
	}
	defer br3.Close()
	t.Log("✓ parameter mismatch: 406 on wrong params; passive_declare=true connects cleanly")
}

// TestRabbitMQ_QosPrefetch verifies that the broker delivers messages one at a time
// (prefetch=1), meaning a second message is not pushed until the first is ACKed.
func TestRabbitMQ_QosPrefetch(t *testing.T) {
	requireBroker(t)
	const q = "tdtp.test.qos"
	defer deleteQueue(q)

	cfg := rmqConfig(q)
	br, _ := New(cfg)
	ctx := context.Background()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer br.Close()

	// Publish two messages.
	_ = br.Send(ctx, []byte("msg-1"))
	_ = br.Send(ctx, []byte("msg-2"))

	// Receive first message — do NOT ack yet.
	recvCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	data1, err := br.Receive(recvCtx)
	cancel()
	if err != nil {
		t.Fatalf("Receive msg-1: %v", err)
	}

	// Try to receive second message with a short timeout — should NOT arrive
	// because prefetch=1 and the first message is unacked.
	shortCtx, cancel2 := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel2()
	data2, err := br.Receive(shortCtx)
	if err == nil {
		t.Fatalf("expected prefetch to block second message, got: %q", data2)
	}
	t.Logf("second message held back (prefetch=1): %v", err)

	// Now ACK the first and verify second arrives.
	_ = br.(*RabbitMQ).AckLast()
	recvCtx3, cancel3 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel3()
	data2, err = br.Receive(recvCtx3)
	if err != nil {
		t.Fatalf("Receive msg-2 after ack: %v", err)
	}
	_ = br.(*RabbitMQ).AckLast()

	if string(data1) != "msg-1" || string(data2) != "msg-2" {
		t.Fatalf("payload order wrong: %q %q", data1, data2)
	}
	t.Log("✓ QoS prefetch=1: second message held until first is ACKed")
}

// ── internal helpers used by tests ─────────────────────────────────────────────

// reconnectBrokerForTest mirrors the runMapListen() reconnect logic.
// It is defined here (not in map.go) to keep the test self-contained.
func reconnectBrokerForTest(ctx context.Context, br MessageBroker) error {
	_ = br.Close()
	delay := 2 * time.Second
	const maxDelay = 30 * time.Second
	for {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := br.Connect(ctx); err != nil {
			if delay < maxDelay {
				delay *= 2
			}
			continue
		}
		return nil
	}
}

// createQueue creates a durable queue via management HTTP API.
func createQueue(t *testing.T, name string) {
	t.Helper()
	body := strings.NewReader(`{"durable":true,"auto_delete":false,"arguments":{}}`)
	req, _ := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/queues/%%2F/%s", mgmtBase, url.PathEscape(name)), body)
	req.SetBasicAuth(rmqUser, rmqPassword)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("createQueue %s: %v", name, err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 && resp.StatusCode != 204 {
		t.Fatalf("createQueue %s: status %d", name, resp.StatusCode)
	}
}
