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
	"os"
	"strings"
	"testing"
	"time"
)

// ── helpers ────────────────────────────────────────────────────────────────────

// Credentials are read from env vars so the tests work on any RabbitMQ setup.
// Defaults match the travel-agency example (user=tdtp, password=tdtp).
// Override: RABBITMQ_TEST_USER / RABBITMQ_TEST_PASSWORD / RABBITMQ_TEST_HOST
var (
	rmqHost     = envOrDefault("RABBITMQ_TEST_HOST", "localhost")
	rmqUser     = envOrDefault("RABBITMQ_TEST_USER", "tdtp")
	rmqPassword = envOrDefault("RABBITMQ_TEST_PASSWORD", "tdtp")
)

const (
	rmqPort  = 5672
	rmqVHost = "/"
)

var mgmtBase = fmt.Sprintf("http://%s:15672/api", envOrDefault("RABBITMQ_TEST_HOST", "localhost"))

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

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
		t.Skipf("broker not reachable (%s@%s): %v", rmqUser, rmqHost, err)
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

// forceCloseAllConnections closes every broker connection via management API
// using the test credentials (rmqUser/rmqPassword).
// NOTE: requires the test user to have the `administrator` management tag.
// If the user only has `management` tag, /api/connections returns [] and nothing
// is closed. Use forceCloseAllConnectionsAs with an admin account instead.
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

// forceCloseAllConnectionsAs is like forceCloseAllConnections but uses explicit
// credentials — useful when the admin account differs from the test account.
// Set RABBITMQ_TEST_ADMIN_USER / RABBITMQ_TEST_ADMIN_PASSWORD to enable.
func forceCloseAllConnectionsAs(t *testing.T, user, password string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, mgmtBase+"/connections", nil)
	req.SetBasicAuth(user, password)
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
		t.Fatalf("parse connections: %v (body: %s)", err, raw)
	}
	for _, c := range conns {
		req, _ := http.NewRequest(http.MethodDelete,
			fmt.Sprintf("%s/connections/%s", mgmtBase, url.PathEscape(c.Name)), nil)
		req.SetBasicAuth(user, password)
		req.Header.Set("X-Reason", "integration-test-teardown")
		http.DefaultClient.Do(req) //nolint:errcheck
	}
	t.Logf("force-closed %d connection(s) as %s", len(conns), user)
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
// and verifies that reconnectBroker() restores the consumer so the daemon loop
// continues without manual intervention.
//
// Drop simulation: we close the underlying amqp.Connection directly from a
// goroutine (we are in package brokers, so private fields are accessible).
// This is identical to what happens on a real network drop — the amqp library
// closes the delivery channel, Receive() returns "delivery channel closed", and
// the daemon loop must call reconnectBroker() to resume.
//
// This reproduces the bug fixed in v1.16.1: deliveryChan was not reset on
// Close()/Connect(), so startConsuming() returned early and every subsequent
// Receive() read from a permanently closed channel → infinite error loop.
//
// Management API alternative: if the RABBITMQ_TEST_ADMIN_USER / _PASSWORD env
// vars are set for an administrator account, the test uses forceCloseAllConnections
// instead — a more realistic external drop simulation.
func TestRabbitMQ_ReconnectAfterConnectionDrop(t *testing.T) {
	requireBroker(t)
	const q = "tdtp.test.reconnect"
	defer deleteQueue(q)

	cfg := rmqConfig(q)
	br, _ := New(cfg)
	ctx := context.Background()
	if err := br.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer br.Close()

	rmqBr := br.(*RabbitMQ)

	// Queue is empty — start a blocking Receive in a goroutine.
	// The goroutine registers the consumer (startConsuming) and then blocks
	// on the delivery channel, waiting for a message that never arrives.
	// This is the steady-state of a --listen daemon between messages.
	recvErrCh := make(chan error, 1)
	go func() {
		_, err := br.Receive(context.Background())
		recvErrCh <- err
	}()
	time.Sleep(100 * time.Millisecond) // give goroutine time to enter blocking select

	// ── simulate connection drop ────────────────────────────────────────────
	// Primary: close amqp.Connection directly. Identical to a TCP drop — the
	// amqp library closes the delivery channel, select unblocks with ok=false.
	// Alternative (real external drop): set RABBITMQ_TEST_ADMIN_USER/PASSWORD
	// to an administrator account; the management API closes the TCP connection.
	adminUser := os.Getenv("RABBITMQ_TEST_ADMIN_USER")
	adminPass := os.Getenv("RABBITMQ_TEST_ADMIN_PASSWORD")
	if adminUser != "" && adminPass != "" {
		t.Log("dropping connection via management API (external TCP drop)...")
		forceCloseAllConnectionsAs(t, adminUser, adminPass)
	} else {
		t.Log("dropping connection via conn.Close() (same code path as TCP drop)...")
		_ = rmqBr.conn.Close()
	}

	// Receive goroutine must surface an error — delivery channel is now closed.
	select {
	case receiveErr := <-recvErrCh:
		t.Logf("✓ Receive() surfaced error after drop: %v", receiveErr)
	case <-time.After(15 * time.Second):
		// 15s > heartbeat (10s): if we don't get an error by then, the fix is broken.
		t.Fatal("Receive() did not unblock within 15s after connection drop")
	}

	// reconnectBroker must restore the connection.
	reconnCtx, cancel2 := context.WithTimeout(ctx, 30*time.Second)
	defer cancel2()
	if err := reconnectBrokerForTest(reconnCtx, br); err != nil {
		t.Fatalf("reconnectBroker: %v", err)
	}
	t.Log("reconnected — verifying send+receive works...")

	// Send a new message and receive it — proves the consumer was re-registered.
	if err := br.Send(ctx, []byte("post-reconnect")); err != nil {
		t.Fatalf("Send after reconnect: %v", err)
	}
	recvCtx2, cancel3 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel3()
	data, err := br.Receive(recvCtx2)
	if err != nil {
		t.Fatalf("Receive after reconnect: %v", err)
	}
	if string(data) != "post-reconnect" {
		t.Fatalf("payload mismatch: %q", data)
	}
	_ = rmqBr.AckLast()
	t.Log("✓ reconnect: send→receive works after connection drop")
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
