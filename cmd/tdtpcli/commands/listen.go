package commands

// BETA: Streaming consumer daemon for Kafka only.
//
// Broker tier selection:
//
//	MSMQ     — Legacy     (Windows-only, no partition ordering; batch mode only)
//	RabbitMQ — Stability  (reliable delivery, acknowledgements; batch mode only)
//	Kafka    — Speed      (ordered partitions, offset commit; batch + streaming)
//
// Only Kafka guarantees strict per-partition ordering required to assemble
// stream sessions from sequentially numbered parts (PartNumber 1…N).
//
// Design notes:
//   - Runs as a long-lived daemon process; terminated by SIGTERM/SIGINT.
//   - Uses "Variant A" import strategy: each received part is imported immediately
//     into the target table. Suitable for stable, high-availability channels only.
//   - Recommended for: LAN, dedicated WAN links, 99.99%+ uptime channels.
//   - NOT recommended for: VPN, mobile networks, unreliable WAN. Use batch
//     mode (--export-broker / --import-broker) for unreliable connections.
//
// Stream session lifecycle:
//   - Part with TotalParts=0  → active session, import rows immediately
//   - Part with TotalParts=N  → final part, import rows, close session
//   - New MessageID (same base) on reconnect → session restart (retry)

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ListenConfig holds configuration for the streaming consumer daemon.
type ListenConfig struct {
	BrokerCfg *BrokerConfig
	Strategy  adapters.ImportStrategy
}

// streamSession tracks an active streaming session by MessageID base.
type streamSession struct {
	MessageIDBase string
	TableName     string
	PartsReceived int
	RowsTotal     int
	StartedAt     time.Time
}

// extractStreamBase extracts the base MessageID from a streaming part MessageID.
// Streaming parts use the format: "{base}-S{partNum}", e.g. "MSG-20260310-001-S1".
// Falls back to the full MessageID if no "-S" suffix is found (single-part or batch).
func extractStreamBase(messageID string) string {
	for i := len(messageID) - 2; i >= 0; i-- {
		if messageID[i:i+2] == "-S" {
			return messageID[:i]
		}
	}
	return messageID
}

// ListenKafkaStream runs the streaming consumer daemon.
// It blocks until SIGTERM/SIGINT is received or a fatal error occurs.
func ListenKafkaStream(ctx context.Context, dbConfig *adapters.Config, cfg ListenConfig) error {
	if !strings.EqualFold(cfg.BrokerCfg.Type, "kafka") {
		return fmt.Errorf(
			"--listen supports Kafka only (got: %q)\n\n"+
				"  Streaming mode requires strict message ordering, which is guaranteed\n"+
				"  by Kafka partitions but not by RabbitMQ or MSMQ.\n\n"+
				"  For RabbitMQ/MSMQ use batch mode: --import-broker",
			cfg.BrokerCfg.Type,
		)
	}

	// Create DB adapter
	adapter, err := adapters.New(ctx, *dbConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	// Create and connect Kafka broker
	broker, err := createBroker(cfg.BrokerCfg)
	if err != nil {
		return fmt.Errorf("failed to create broker: %w", err)
	}
	defer func() { _ = broker.Close() }()

	if err := broker.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to Kafka: %w", err)
	}

	topic := cfg.BrokerCfg.Queue
	fmt.Printf("[listen] BETA streaming consumer started\n")
	fmt.Printf("[listen] Kafka topic : %s\n", topic)
	fmt.Printf("[listen] DB strategy : %s\n", cfg.Strategy)
	fmt.Printf("[listen] WARNING: requires stable channel (99.99%% uptime recommended)\n")
	fmt.Printf("[listen] Press Ctrl+C to stop\n\n")

	// Trap shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	listenCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-sigCh
		fmt.Printf("\n[listen] Shutdown signal received, draining...\n")
		cancel()
	}()

	parser := packet.NewParser()
	sessions := make(map[string]*streamSession)

	for {
		// Receive next message; blocks until available or context canceled
		xmlData, err := broker.Receive(listenCtx)
		if err != nil {
			if listenCtx.Err() != nil {
				break // clean shutdown
			}
			fmt.Printf("[listen] receive error: %v — retrying in 2s\n", err)
			select {
			case <-time.After(2 * time.Second):
			case <-listenCtx.Done():
				break
			}
			continue
		}

		// Parse packet (handles optional decompression)
		pkt, err := parser.ParseBytesWithDecompression(xmlData, func(ctx context.Context, compressed string, algo string) ([]string, error) {
			return decompressData(compressed, algo)
		})
		if err != nil {
			fmt.Printf("[listen] parse error (skipping message): %v\n", err)
			continue
		}

		h := pkt.Header
		sessionKey := extractStreamBase(h.MessageID)
		isStreaming := h.TotalParts == 0
		isFinal := h.TotalParts > 0

		// Resolve or create session
		sess, exists := sessions[sessionKey]
		if !exists {
			sess = &streamSession{
				MessageIDBase: sessionKey,
				TableName:     h.TableName,
				StartedAt:     time.Now(),
			}
			sessions[sessionKey] = sess
			fmt.Printf("[listen] new session  : %s → table '%s'\n", sessionKey, h.TableName)
		}

		// Import rows immediately (Variant A)
		rowCount := len(pkt.Data.Rows)
		if err := adapter.ImportPacket(listenCtx, pkt, cfg.Strategy); err != nil {
			fmt.Printf("[listen] import error (session %s, part %d): %v\n",
				sessionKey, h.PartNumber, err)
			// Do NOT commit offset — Kafka will redeliver on reconnect
			continue
		}

		sess.PartsReceived++
		sess.RowsTotal += rowCount

		status := "streaming"
		if isFinal {
			status = "final"
		}

		fmt.Printf("[listen] %-10s part=%d rows=%-6d table='%s' session=%s\n",
			status, h.PartNumber, rowCount, h.TableName, sessionKey)

		// Commit offset only after successful import
		if committer, ok := broker.(interface{ CommitLast(context.Context) error }); ok {
			if err := committer.CommitLast(listenCtx); err != nil {
				fmt.Printf("[listen] offset commit error: %v\n", err)
			}
		}

		// Close session on final part
		if isFinal || (!isStreaming && !isFinal) {
			elapsed := time.Since(sess.StartedAt).Round(time.Millisecond)
			fmt.Printf("[listen] session done : %s — %d parts, %d rows, %s\n",
				sessionKey, sess.PartsReceived, sess.RowsTotal, elapsed)
			delete(sessions, sessionKey)
		}
	}

	// Report any sessions that were interrupted by shutdown
	if len(sessions) > 0 {
		fmt.Printf("[listen] WARNING: %d incomplete session(s) at shutdown:\n", len(sessions))
		for key, s := range sessions {
			fmt.Printf("         - %s (table=%s parts=%d rows=%d)\n",
				key, s.TableName, s.PartsReceived, s.RowsTotal)
		}
		fmt.Printf("[listen] Partial data may exist in target tables (Variant A mode).\n")
	}

	fmt.Printf("[listen] stopped\n")
	return nil
}
