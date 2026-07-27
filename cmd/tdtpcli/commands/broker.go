// Package commands provides functionality for the TDTP framework.
package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/brokers"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
	"github.com/ruslano69/tdtp-framework/pkg/pipeline"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// BrokerConfig holds broker configuration
type BrokerConfig struct {
	Type           string
	Host           string
	Port           int
	User           string
	Password       string
	Queue          string
	VHost          string
	UseTLS         bool
	TLSSkipVerify  bool
	Exchange       string
	RoutingKey     string
	Durable        bool
	AutoDelete     bool
	Exclusive      bool
	PassiveDeclare bool
	QueuePath      string   // MSMQ: полный путь к очереди (например: ".\private$\tdtp_in")
	Brokers        []string // Kafka: список брокеров (["localhost:9092"])
	ConsumerGroup  string   // Kafka: consumer group ID
}

// ExportToBroker exports table data to message broker.
//
// mercuryURL non-empty + encrypt=true enables encryption (mirrors --enc's
// export.go behavior, previously unsupported here — confirmed by grep this
// command pair had zero encryption support before this):
//   - encryptLegacy=false (default, v1.5): each packet's QueryContext/
//     Schema/Data go opaque via EncryptPacketV15, Header stays plain XML —
//     the broker message is still valid XML, keyed by that packet's own
//     Header.MessageID.
//   - encryptLegacy=true (--enc13): whole-packet binary blob via
//     EncryptPacket, same as --export --enc13 produces to a file.
func ExportToBroker(ctx context.Context, dbConfig *adapters.Config, brokerCfg *BrokerConfig, tableName string, query *packet.Query, compress bool, compressLevel int, compressAlgo string, procMgr ProcessorManager, packetSizeMB int, mercuryURL string, encrypt, encryptLegacy bool) error {
	// Create database adapter
	adapter, err := adapters.New(ctx, *dbConfig)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	fmt.Printf("Exporting table '%s' to broker...\n", tableName)

	// Configure packet size if requested
	type packetSizeSetter interface{ SetMaxMessageSize(int) }
	if packetSizeMB > 0 {
		if sizer, ok := adapter.(packetSizeSetter); ok {
			sizer.SetMaxMessageSize(packetSizeMB * 2 * 1024 * 1024)
			fmt.Printf("Packet size set to %dMB (internal estimate: %dMB)\n", packetSizeMB, packetSizeMB*2)
		}
	}

	// Export data
	var packets []*packet.DataPacket
	if query != nil {
		fmt.Printf("Applying filters...\n")
		packets, err = adapter.ExportTableWithQuery(ctx, tableName, query, "tdtpcli", "")
	} else {
		packets, err = adapter.ExportTable(ctx, tableName)
	}

	if err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	if len(packets) == 0 {
		fmt.Println("⚠ No data to export")
		return nil
	}

	fmt.Printf("✓ Exported %d packet(s)\n", len(packets))

	// Create broker (параллельно с подготовкой данных)
	broker, err := createBroker(brokerCfg)
	if err != nil {
		return fmt.Errorf("failed to create broker: %w", err)
	}
	defer func() { _ = broker.Close() }()

	// Параллельное сжатие + сериализация всех пакетов
	if compress {
		fmt.Printf("Compressing data (algo: %s, level %d)...\n", compressAlgo, compressLevel)
	}
	if encrypt {
		if mercuryURL == "" {
			return fmt.Errorf("--enc/--enc13 requires --mercury-url pointing at a running xZMercury instance")
		}
		if encryptLegacy {
			fmt.Println("Encrypting data (TDTP v1.3 whole-blob via xZMercury)...")
		} else {
			fmt.Println("Encrypting data (TDTP v1.5 section-level via xZMercury)...")
		}
	}

	xmlMsgs := make([][]byte, len(packets))
	errs := make([]error, len(packets))

	// v1.5 encryption needs a Mercury client shared across the per-packet
	// goroutines below for the mandatory integrity step — one instance,
	// not one per packet.
	var integrityClient *mercury.Client
	if encrypt && !encryptLegacy {
		integrityClient = mercury.NewClient(mercuryURL, 5000)
	}

	// Encryption calls xZMercury over HTTP per packet — keep this concurrent
	// like compression/marshal already are, not a reason to serialize.
	var wg sync.WaitGroup
	for i, pkt := range packets {
		wg.Add(1)
		go func(i int, pkt *packet.DataPacket) {
			defer wg.Done()

			// v1.4 integrity is mandatory ahead of v1.5 encryption, not
			// opt-in — see pkg/pipeline/produce.go's doc comment: without
			// this, VerifyAndPrepare's consumer-side pre-flight (which
			// always runs once --mercury-url is set, and v1.5 decryption
			// requires it) blocks the packet with HASH_NOT_REGISTERED.
			// Must run before compression (hashes cover plaintext).
			if encrypt && !encryptLegacy {
				if err := pipeline.ComputeAndRegisterIntegrity(ctx, pkt, integrityClient, tableName); err != nil {
					errs[i] = fmt.Errorf("packet %d integrity: %w", i+1, err)
					return
				}
			}

			if compress {
				if err := compressPacketData(pkt, compressLevel, compressAlgo, true); err != nil { // checksum always enabled with compression
					errs[i] = fmt.Errorf("packet %d compress: %w", i+1, err)
					return
				}
			}

			if encrypt && encryptLegacy {
				// EncryptPacket marshals pkt to XML internally before encrypting —
				// no separate marshal step needed here.
				blob, _, err := EncryptPacket(ctx, pkt, mercuryURL, tableName)
				if err != nil {
					errs[i] = fmt.Errorf("packet %d encrypt (v1.3): %w", i+1, err)
					return
				}
				xmlMsgs[i] = blob
				return
			}

			if encrypt {
				xml, _, err := EncryptPacketV15(ctx, pkt, mercuryURL, tableName)
				if err != nil {
					errs[i] = fmt.Errorf("packet %d encrypt (v1.5): %w", i+1, err)
					return
				}
				xmlMsgs[i] = xml
				return
			}

			gen := packet.NewGenerator()
			xml, err := gen.ToXML(pkt, true)
			if err != nil {
				errs[i] = fmt.Errorf("packet %d marshal: %w", i+1, err)
				return
			}
			xmlMsgs[i] = xml
		}(i, pkt)
	}
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			return e
		}
	}

	if compress {
		fmt.Printf("✓ Data compressed with %s\n", compressAlgo)
	}
	if encrypt {
		fmt.Printf("✓ Data encrypted (%s)\n", map[bool]string{true: "v1.3 whole-blob", false: "v1.5 section-level"}[encryptLegacy])
	}

	// Connect to broker
	if err := broker.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}

	// Если брокер поддерживает пакетную отправку — используем один roundtrip
	type batchSender interface {
		SendBatch(ctx context.Context, messages [][]byte) error
	}
	if bs, ok := broker.(batchSender); ok {
		if err := bs.SendBatch(ctx, xmlMsgs); err != nil {
			return fmt.Errorf("failed to send batch: %w", err)
		}
	} else {
		for i, msg := range xmlMsgs {
			if err := broker.Send(ctx, msg); err != nil {
				return fmt.Errorf("failed to send packet %d: %w", i+1, err)
			}
		}
	}

	fmt.Printf("✓ Sent %d packet(s)\n", len(packets))
	fmt.Println("✓ Export to broker complete!")

	return nil
}

// defaultIdleTimeout is how long --import-broker waits for the next message
// before deciding the queue is empty and stopping.
const defaultIdleTimeout = 5 * time.Second

// ImportBrokerOptions holds options for ImportFromBroker
type ImportBrokerOptions struct {
	Strategy    adapters.ImportStrategy
	TargetTable string // override table name from packet header (fixes name conflicts)
	OutputFile  string // if set, save packets to file(s) instead of importing to DB
	Raw         bool   // save raw bytes as-is, no parse/decompress/validate
	Keep        bool   // allow partial writes (non-atomic): import each part immediately.
	// Default (Keep=false): all parts committed in one transaction via ImportPackets — all-or-nothing.
	// Keep=true: each part committed individually; if a later part fails, earlier parts remain.
	// Use Keep for batches too large for a single DB transaction.
	IdleTimeout time.Duration // how long to wait for the next message before stopping (0 = default 5s)

	// PipelineContext precondition check (v1.4): --expect-var name=value.
	ExpectVars map[string]string

	// MercuryURL enables full executor verification for v1.4 packets.
	// Empty → local xxh3 integrity check only (FallbackDegrade policy).
	MercuryURL string
}

// ImportFromBroker imports one complete export batch from the broker queue.
//
// All packets of the same export share a common batch ID embedded in MessageID
// (e.g. "REF-20250319-ABCD1234-P1", "REF-20250319-ABCD1234-P2", ...).
// Packets from a different batch are Nack'd with requeue=true so they stay in
// the queue untouched. The function exits once all TotalParts are received or
// the idle timeout fires (queue empty).
func ImportFromBroker(ctx context.Context, dbConfig *adapters.Config, brokerCfg *BrokerConfig, opts ImportBrokerOptions) error {
	// Create and connect broker.
	broker, err := createBroker(brokerCfg)
	if err != nil {
		return fmt.Errorf("failed to create broker: %w", err)
	}
	defer func() { _ = broker.Close() }()

	if err := broker.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}

	// --raw mode: drain queue, write bytes as-is, no parse/decompress.
	if opts.Raw {
		return importBrokerRaw(ctx, broker, opts)
	}

	// In file-save mode we don't need a DB adapter.
	var adapter adapters.Adapter
	if opts.OutputFile == "" {
		adapter, err = adapters.New(ctx, *dbConfig)
		if err != nil {
			return fmt.Errorf("failed to create adapter: %w", err)
		}
		defer func() { _ = adapter.Close(ctx) }()
	}

	// --keep mode: streaming — receive → decompress → import immediately, no full buffer.
	if opts.Keep {
		return importBrokerKeep(ctx, broker, adapter, opts)
	}

	idleTimeout := opts.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = defaultIdleTimeout
	}

	// Helpers for manual ack/nack.
	ackLast := func() error {
		if acker, ok := broker.(interface{ AckLast() error }); ok {
			return acker.AckLast()
		}
		return nil
	}
	generator := packet.NewGenerator()

	receive := func() ([]byte, error) {
		recvCtx, cancel := context.WithTimeout(ctx, idleTimeout)
		defer cancel()
		data, err := broker.Receive(recvCtx)
		if err != nil && recvCtx.Err() != nil {
			return nil, fmt.Errorf("queue empty (no message in %s)", idleTimeout)
		}
		return data, err
	}

	parse := func(raw []byte) (*packet.DataPacket, error) {
		return parseAndDecryptBrokerMessage(ctx, raw, opts.MercuryURL)
	}

	// ── Step 1: read the first packet to learn batchID and TotalParts ──────
	xmlData, err := receive()
	if err != nil {
		fmt.Printf("Queue is empty: %v\n", err)
		return nil
	}

	firstPkt, err := parse(xmlData)
	if err != nil {
		return fmt.Errorf("failed to parse first packet: %w", err)
	}

	batchID := batchIDFromMessageID(firstPkt.Header.MessageID)
	totalParts := firstPkt.Header.TotalParts
	if totalParts == 0 {
		totalParts = 1 // single-packet export
	}

	if opts.OutputFile != "" {
		fmt.Printf("Saving batch '%s' (%d part(s)) from queue '%s'...\n", batchID, totalParts, brokerCfg.Queue)
	} else {
		fmt.Printf("Importing batch '%s' (%d part(s)) from queue '%s' (strategy: %s)...\n",
			batchID, totalParts, brokerCfg.Queue, opts.Strategy)
	}

	// ── Step 2: receive all remaining raw packets ────────────────────────────
	// Receive is inherently serial (one call per message); we buffer raw bytes
	// so that decompression can run in parallel in the next step.
	allRaw := make([][]byte, 1, totalParts)
	allRaw[0] = xmlData
	for received := 1; received < totalParts; received++ {
		raw, err := receive()
		if err != nil {
			fmt.Printf("Queue empty after %d/%d parts: %v\n", received, totalParts, err)
			break
		}
		allRaw = append(allRaw, raw)
	}
	actualParts := len(allRaw)

	// ── Step 3: decompress all packets in parallel ───────────────────────────
	// First packet is already parsed; remaining ones run in goroutines.
	parsedPackets := make([]*packet.DataPacket, actualParts)
	parseErrs := make([]error, actualParts)
	parsedPackets[0] = firstPkt

	if actualParts > 1 {
		var wg sync.WaitGroup
		for i := 1; i < actualParts; i++ {
			wg.Add(1)
			go func(i int, raw []byte) {
				defer wg.Done()
				pkt, err := parse(raw)
				if err != nil {
					parseErrs[i] = fmt.Errorf("packet %d: %w", i+1, err)
					return
				}
				if batchIDFromMessageID(pkt.Header.MessageID) != batchID {
					parseErrs[i] = fmt.Errorf("packet %d belongs to a different batch", i+1)
					return
				}
				parsedPackets[i] = pkt
			}(i, allRaw[i])
		}
		wg.Wait()
	}
	for _, e := range parseErrs {
		if e != nil {
			return e
		}
	}

	// ── Step 4: process all packets ─────────────────────────────────────────
	if opts.TargetTable != "" {
		for _, pkt := range parsedPackets {
			pkt.Header.TableName = opts.TargetTable
		}
	}

	// PipelineContext precondition check (v1.4): --expect-var name=value.
	if len(opts.ExpectVars) > 0 {
		if err := CheckPipelineVars(parsedPackets[0], opts.ExpectVars); err != nil {
			return err
		}
	}

	// ── Security gate (v1.4) — applied to every packet before any write ─────
	for i, pkt := range parsedPackets {
		if err := applyV14SecurityGate(ctx, pkt, opts.MercuryURL); err != nil {
			return fmt.Errorf("packet %d/%d: %w", i+1, actualParts, err)
		}
	}

	switch {
	case opts.OutputFile != "":
		// File-save mode: write each part to disk.
		for i, pkt := range parsedPackets {
			fmt.Printf("  Part %d/%d — table '%s' (%d row(s))\n",
				pkt.Header.PartNumber, totalParts, pkt.Header.TableName, len(pkt.Data.Rows))
			filename := brokerOutputFilename(opts.OutputFile, i+1, totalParts)
			xmlBytes, err := generator.ToXML(pkt, true)
			if err != nil {
				return fmt.Errorf("failed to marshal packet %d: %w", i+1, err)
			}
			if err := os.WriteFile(filename, xmlBytes, 0o600); err != nil {
				return fmt.Errorf("failed to write '%s': %w", filename, err)
			}
			fmt.Printf("  ✓ Saved to: %s\n", filename)
		}
	default:
		// Atomic mode (default): all parts in one transaction — all-or-nothing.
		// Mirrors the behavior of --import (file) which uses ImportPackets for multi-part.
		totalRows := 0
		for _, pkt := range parsedPackets {
			fmt.Printf("  Part %d/%d — table '%s' (%d row(s))\n",
				pkt.Header.PartNumber, totalParts, pkt.Header.TableName, len(pkt.Data.Rows))
			totalRows += len(pkt.Data.Rows)
		}
		var importErr error
		if len(parsedPackets) == 1 {
			importErr = adapter.ImportPacket(ctx, parsedPackets[0], opts.Strategy)
		} else {
			importErr = adapter.ImportPackets(ctx, parsedPackets, opts.Strategy)
		}
		if importErr != nil {
			return fmt.Errorf("import failed (all parts rolled back): %w", importErr)
		}
		fmt.Printf("  ✓ Imported %d row(s) into '%s'\n", totalRows, parsedPackets[0].Header.TableName)
	}

	// ACK: for Kafka — CommitMessages on the last offset commits all previous
	// offsets too (cumulative). For RabbitMQ — acks the last delivery tag only;
	// earlier messages are auto-acked when the channel closes normally.
	if err := ackLast(); err != nil {
		return fmt.Errorf("ack failed: %w", err)
	}

	fmt.Printf("✓ Done. %d/%d part(s) processed.\n", actualParts, totalParts)
	return nil
}

// batchIDFromMessageID extracts the export batch ID from a MessageID.
// importBrokerKeep is the streaming (non-atomic) import path used when --keep is set.
// Each packet is received, decompressed, and committed to the DB immediately —
// no full in-memory buffering of the whole batch. On failure the successfully
// committed parts remain in the table and can be inspected or rolled back manually.
func importBrokerKeep(ctx context.Context, broker brokers.MessageBroker, adapter adapters.Adapter, opts ImportBrokerOptions) error {
	idleTimeout := opts.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = defaultIdleTimeout
	}

	ackLast := func() error {
		if acker, ok := broker.(interface{ AckLast() error }); ok {
			return acker.AckLast()
		}
		return nil
	}

	receive := func() ([]byte, error) {
		recvCtx, cancel := context.WithTimeout(ctx, idleTimeout)
		defer cancel()
		data, err := broker.Receive(recvCtx)
		if err != nil && recvCtx.Err() != nil {
			return nil, fmt.Errorf("queue empty (no message in %s)", idleTimeout)
		}
		return data, err
	}

	parse := func(raw []byte) (*packet.DataPacket, error) {
		return parseAndDecryptBrokerMessage(ctx, raw, opts.MercuryURL)
	}

	// Read first packet to learn batchID and TotalParts.
	xmlData, err := receive()
	if err != nil {
		fmt.Printf("Queue is empty: %v\n", err)
		return nil
	}
	firstPkt, err := parse(xmlData)
	if err != nil {
		return fmt.Errorf("failed to parse first packet: %w", err)
	}
	if opts.TargetTable != "" {
		firstPkt.Header.TableName = opts.TargetTable
	}

	batchID := batchIDFromMessageID(firstPkt.Header.MessageID)
	totalParts := firstPkt.Header.TotalParts
	if totalParts == 0 {
		totalParts = 1
	}
	fmt.Printf("Importing batch '%s' (%d part(s)) from queue '%s' (strategy: %s, --keep)\n",
		batchID, totalParts, opts.IdleTimeout, opts.Strategy)

	importOne := func(pkt *packet.DataPacket, n int) error {
		// ── Security gate (v1.4) ─────────────────────────────────────────────
		if err := applyV14SecurityGate(ctx, pkt, opts.MercuryURL); err != nil {
			return fmt.Errorf("part %d: %w", n, err)
		}
		fmt.Printf("  Part %d/%d — table '%s' (%d row(s))\n",
			pkt.Header.PartNumber, totalParts, pkt.Header.TableName, len(pkt.Data.Rows))
		if err := adapter.ImportPacket(ctx, pkt, opts.Strategy); err != nil {
			return fmt.Errorf("import failed at part %d: %w", n, err)
		}
		fmt.Printf("  ✓ Committed %d row(s) into '%s'\n", len(pkt.Data.Rows), pkt.Header.TableName)
		return nil
	}

	if err := importOne(firstPkt, 1); err != nil {
		return err
	}
	if err := ackLast(); err != nil {
		return fmt.Errorf("ack failed: %w", err)
	}

	committed := 1
	for committed < totalParts {
		raw, err := receive()
		if err != nil {
			fmt.Printf("Queue empty after %d/%d committed: %v\n", committed, totalParts, err)
			break
		}
		pkt, err := parse(raw)
		if err != nil {
			return fmt.Errorf("failed to parse packet %d: %w", committed+1, err)
		}
		if batchIDFromMessageID(pkt.Header.MessageID) != batchID {
			fmt.Printf("  ⚠ Packet from different batch — stopping\n")
			break
		}
		if opts.TargetTable != "" {
			pkt.Header.TableName = opts.TargetTable
		}
		committed++
		if err := importOne(pkt, committed); err != nil {
			return err
		}
		if err := ackLast(); err != nil {
			return fmt.Errorf("ack failed: %w", err)
		}
	}

	fmt.Printf("✓ Done. %d/%d part(s) committed.\n", committed, totalParts)
	return nil
}

// Format: "REF-20250319-ABCD1234-P3" → "REF-20250319-ABCD1234"
func batchIDFromMessageID(messageID string) string {
	if idx := strings.LastIndex(messageID, "-P"); idx >= 0 {
		return messageID[:idx]
	}
	return messageID
}

// brokerOutputFilename returns output path for part N of total.
// Single-part: outputFile as-is.
// Multi-part: base_part_N_of_Total.ext  (compatible with --import multi-part convention)
// importBrokerRaw drains the queue and writes each message as raw bytes.
// No parsing, no decompression, no validation — exactly what the broker sent.
// Peeks at the first message header to learn TotalParts for _part_N_of_Total naming.
// Files: outputFile (single) or base_part_N_of_Total.ext (multi-part).
func importBrokerRaw(ctx context.Context, broker brokers.MessageBroker, opts ImportBrokerOptions) error {
	if opts.OutputFile == "" {
		return fmt.Errorf("--raw requires --output <file>")
	}

	idleTimeout := opts.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = defaultIdleTimeout
	}

	receive := func() ([]byte, error) {
		recvCtx, cancel := context.WithTimeout(ctx, idleTimeout)
		defer cancel()
		data, err := broker.Receive(recvCtx)
		if err != nil && recvCtx.Err() != nil {
			return nil, nil // idle timeout — queue empty
		}
		return data, err
	}

	// Read first message to peek TotalParts from header (no full parse needed).
	first, err := receive()
	if err != nil {
		return fmt.Errorf("receive error: %w", err)
	}
	if first == nil {
		fmt.Println("Queue is empty.")
		return nil
	}

	// Quick header peek — parse only, raw bytes saved as-is.
	totalParts := 0
	if pkt, peekErr := packet.NewParser().ParseBytes(first); peekErr == nil {
		totalParts = pkt.Header.TotalParts
	}
	if totalParts == 0 {
		totalParts = 1 // unknown or single-part
	}

	fmt.Printf("Raw drain → %s (%d part(s) expected, idle timeout: %s)...\n",
		opts.OutputFile, totalParts, idleTimeout)

	saveRaw := func(data []byte, n int) error {
		filename := brokerOutputFilename(opts.OutputFile, n, totalParts)
		if err := os.WriteFile(filename, data, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", filename, err)
		}
		fmt.Printf("  ✓ [%d/%d] %s (%d bytes)\n", n, totalParts, filename, len(data))
		return nil
	}

	totalBytes := len(first)
	if err := saveRaw(first, 1); err != nil {
		return err
	}

	n := 1
	for n < totalParts {
		data, err := receive()
		if err != nil {
			return fmt.Errorf("receive error: %w", err)
		}
		if data == nil {
			fmt.Printf("  ⚠ Queue empty after %d/%d parts (idle timeout)\n", n, totalParts)
			break
		}
		n++
		totalBytes += len(data)
		if err := saveRaw(data, n); err != nil {
			return err
		}
	}

	fmt.Printf("✓ Done. %d/%d message(s), %.1f MB total.\n", n, totalParts, float64(totalBytes)/1024/1024)
	return nil
}

func brokerOutputFilename(outputFile string, n, total int) string {
	if total == 1 {
		return outputFile
	}
	// Strip double extension: "users.tdtp.xml" → base="users", ext=".tdtp.xml"
	// to get "users_part_1_of_15.tdtp.xml"
	name := filepath.Base(outputFile)
	dir := filepath.Dir(outputFile)

	// Find first dot to preserve compound extension (.tdtp.xml)
	dotIdx := strings.Index(name, ".")
	var base, ext string
	if dotIdx >= 0 {
		base = name[:dotIdx]
		ext = name[dotIdx:]
	} else {
		base = name
		ext = ""
	}
	return filepath.Join(dir, fmt.Sprintf("%s_part_%d_of_%d%s", base, n, total, ext))
}

// createBroker creates a message broker based on configuration
func createBroker(cfg *BrokerConfig) (brokers.MessageBroker, error) {
	// Kafka brokers list: use explicit Brokers slice; fall back to Host:Port
	kafkaBrokers := cfg.Brokers
	if len(kafkaBrokers) == 0 && cfg.Host != "" {
		port := cfg.Port
		if port == 0 {
			port = 9092
		}
		kafkaBrokers = []string{fmt.Sprintf("%s:%d", cfg.Host, port)}
	}

	brokerConfig := brokers.Config{
		Type:           cfg.Type,
		Host:           cfg.Host,
		Port:           cfg.Port,
		User:           cfg.User,
		Password:       cfg.Password,
		Queue:          cfg.Queue,
		VHost:          cfg.VHost,
		UseTLS:         cfg.UseTLS,
		TLSSkipVerify:  cfg.TLSSkipVerify,
		Exchange:       cfg.Exchange,
		RoutingKey:     cfg.RoutingKey,
		Durable:        cfg.Durable,
		AutoDelete:     cfg.AutoDelete,
		Exclusive:      cfg.Exclusive,
		PassiveDeclare: cfg.PassiveDeclare,
		QueuePath:      cfg.QueuePath,
		Brokers:        kafkaBrokers,
		Topic:          cfg.Queue,
		ConsumerGroup:  cfg.ConsumerGroup,
	}

	return brokers.New(brokerConfig)
}

// decompressData decompresses compressed data using processors package
func decompressData(compressed, algo string) ([]string, error) {
	return processors.DecompressDataForTdtpWithAlgo(compressed, algo)
}

// decryptLegacyBlobIfNeeded returns raw as-is unless it's a legacy v1.3
// whole-packet binary blob (IsEncryptedBlob), in which case it decrypts
// via xZMercury and returns the recovered plaintext TDTP XML. Must run
// BEFORE any XML parse attempt — the legacy format isn't XML at all, so
// parsing it first would just fail. Shared between the broker and --map
// import paths, which both had their own copy of this exact check.
func decryptLegacyBlobIfNeeded(ctx context.Context, raw []byte, mercuryURL string) ([]byte, error) {
	if !IsEncryptedBlob(raw) {
		return raw, nil
	}
	plaintext, err := DecryptEncBlob(ctx, raw, mercuryURL)
	if err != nil {
		return nil, fmt.Errorf("decrypt v1.3 blob: %w", err)
	}
	return plaintext, nil
}

// decryptV15PacketIfNeeded decrypts pkt in place if it carries v1.5
// section-level encryption (IsEncryptedPacket); no-op otherwise. Checked
// after XML parsing, unlike the legacy blob check — v1.5's Header always
// stays plain, so parsing succeeds before this needs to run.
func decryptV15PacketIfNeeded(ctx context.Context, pkt *packet.DataPacket, mercuryURL string) error {
	if !IsEncryptedPacket(pkt) {
		return nil
	}
	if err := DecryptPacketV15(ctx, pkt, mercuryURL); err != nil {
		return fmt.Errorf("decrypt v1.5 packet: %w", err)
	}
	return nil
}

// parseAndDecryptBrokerMessage parses one broker message, transparently
// decrypting it first if it's encrypted (either format) and decompressing
// after — mirrors what --map/--import already do for file/S3 input,
// previously entirely missing from the broker path (confirmed by grep:
// zero encryption support before this).
//
// Order matters and must not change: decrypt legacy blob (pre-parse) →
// parse → decrypt v1.5 (post-parse) → decompress → expand compact.
// Decompression always runs last of the transform steps — a v1.5 packet
// that was also compressed still carries its Compression attribute after
// DecryptSections (that function deliberately leaves it alone, see
// docs/tdtp-protocol-schema.md → "v1.5" → "Wire-transform order":
// decrypt-then-decompress, ciphertext can't be decompressed first).
// Compact expansion runs unconditionally, independent of whether
// compression also happened — compact format and compression are
// orthogonal flags (ExpandCompactRows itself no-ops when !Data.Compact).
func parseAndDecryptBrokerMessage(ctx context.Context, raw []byte, mercuryURL string) (*packet.DataPacket, error) {
	xmlData, err := decryptLegacyBlobIfNeeded(ctx, raw, mercuryURL)
	if err != nil {
		return nil, err
	}

	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(xmlData)
	if err != nil {
		return nil, err
	}

	if err := decryptV15PacketIfNeeded(ctx, pkt, mercuryURL); err != nil {
		return nil, err
	}

	if parser.IsCompressed(pkt) {
		if err := parser.DecompressData(ctx, pkt, func(dCtx context.Context, compressed, algo string) ([]string, error) {
			return decompressData(compressed, algo)
		}); err != nil {
			return nil, err
		}
	}
	if pkt.Data.Compact {
		if err := packet.ExpandCompactRows(pkt); err != nil {
			return nil, fmt.Errorf("compact expansion failed: %w", err)
		}
	}

	return pkt, nil
}
