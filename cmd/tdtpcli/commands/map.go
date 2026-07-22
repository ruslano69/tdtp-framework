package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/brokers"
	"github.com/ruslano69/tdtp-framework/pkg/core/mapping"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

// MapOptions holds parameters for the --map command.
type MapOptions struct {
	MappingFile string // path to mapping.yaml
	InputFile   string // path to source .tdtp.xml (or .tdtp.enc) file
	DryRun      bool   // print what would happen without writing to DB
	MercuryURL  string // xZMercury base URL for decrypting .enc input (burn-on-read)
	Listen      bool   // daemon mode: loop on broker queue until SIGTERM
}

// RunMap executes a cross-system field mapping: reads a TDTP packet, applies
// the field/enum remap from mappingFile, and upserts rows into the target DB.
// With opts.Listen=true it enters daemon mode, continuously consuming from the
// broker queue until SIGTERM/SIGINT.
func RunMap(ctx context.Context, opts MapOptions) error {
	// Parse mapping config
	cfg, err := mapping.ParseFile(opts.MappingFile)
	if err != nil {
		return fmt.Errorf("--map: %w", err)
	}

	fmt.Printf("Mapping: %s\n", cfg.ID)

	// Extract broker/S3 config from mapping YAML input_source section
	var s3cfg *storage.S3Config
	var brokercfg *brokers.Config
	if cfg.InputSource != nil {
		s3cfg = cfg.InputSource.S3
		brokercfg = cfg.InputSource.Broker
	}

	// Daemon mode: hand off to the listen loop (no loop guard — broker regulates rate)
	if opts.Listen {
		if !isBrokerURI(opts.InputFile) {
			return fmt.Errorf("--listen requires a broker:// URI in --input")
		}
		if brokercfg == nil {
			return fmt.Errorf("--listen: mapping YAML has no input_source.broker section")
		}
		return runMapListen(ctx, cfg, opts, brokercfg)
	}

	// One-shot mode — loop guard (Layers 2+4): skip entirely for dry-runs so
	// validation runs do not consume the min_interval cooldown and block a
	// subsequent real sync.
	var (
		correlationID = "dry-run"
		markDone      func(bool)
	)
	if opts.DryRun {
		fmt.Println("  [dry-run mode — no data will be written]")
		markDone = func(bool) {} // no-op
	} else {
		id, done, err := mapping.CheckAndRecord(cfg)
		if err != nil {
			return fmt.Errorf("--map loop guard: %w", err)
		}
		correlationID = id
		markDone = done
	}
	success := false
	defer func() { markDone(success) }()
	fmt.Printf("  correlation_id: %s\n", correlationID)
	fmt.Printf("  source: %s → target: %s\n", cfg.LoopGuard.SourceSystem, cfg.LoopGuard.TargetSystem)

	// Parse input TDTP packet — local file, S3 URI, or broker URI
	pkt, err := loadPacket(ctx, opts.InputFile, opts.MercuryURL, s3cfg, brokercfg)
	if err != nil {
		return fmt.Errorf("--map: load input %q: %w", opts.InputFile, err)
	}
	fmt.Printf("  input: %s (%d rows, %d fields)\n",
		pkt.Header.TableName, pkt.Header.RecordsInPart, len(pkt.Schema.Fields))

	// Execute mapping
	if err := mapping.Execute(ctx, cfg, pkt, opts.DryRun); err != nil {
		return fmt.Errorf("--map execute: %w", err)
	}

	success = true // deferred done(success) marks the run completed
	return nil
}

// runMapListen is the daemon implementation of --map --listen broker://queue.
// It keeps a single broker connection open and processes messages in a loop,
// ACKing each packet only after a successful upsert into the target DB.
// Loop guard is intentionally skipped — the broker queue regulates the rate.
func runMapListen(ctx context.Context, cfg *mapping.MappingConfig,
	opts MapOptions, brokercfg *brokers.Config) error {

	// Queue name in the URI overrides brokercfg.Queue without mutating the original.
	bcfg := *brokercfg
	if q := strings.TrimPrefix(opts.InputFile, "broker://"); q != "" {
		bcfg.Queue = q
	}

	// Connect once — keep the connection open for the daemon lifetime.
	br, err := brokers.New(bcfg)
	if err != nil {
		return fmt.Errorf("broker driver: %w", err)
	}
	defer func() { _ = br.Close() }()
	if err := br.Connect(ctx); err != nil {
		return fmt.Errorf("broker connect: %w", err)
	}

	fmt.Printf("[map:listen] started  mapping=%s  queue=%s\n", cfg.ID, bcfg.Queue)
	fmt.Printf("[map:listen] source: %s → target: %s\n",
		cfg.LoopGuard.SourceSystem, cfg.LoopGuard.TargetSystem)
	if opts.DryRun {
		fmt.Println("[map:listen] dry-run mode — no data will be written")
	}
	fmt.Printf("[map:listen] Press Ctrl+C to stop\n\n")

	// Graceful shutdown: SIGTERM/SIGINT → cancel listenCtx → Receive unblocks.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	listenCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-sigCh
		fmt.Printf("\n[map:listen] shutdown signal received, finishing current message...\n")
		cancel()
	}()

	parser := packet.NewParser()
	var total int

	for {
		data, err := br.Receive(listenCtx)
		if err != nil {
			if listenCtx.Err() != nil {
				break // clean shutdown
			}
			fmt.Printf("[map:listen] receive error: %v — reconnecting\n", err)
			if reconnectErr := reconnectBroker(listenCtx, br); reconnectErr != nil {
				break // context cancelled during reconnect
			}
			continue
		}

		t0 := time.Now()

		// Decrypt (either format) → parse → decompress → expand (mirrors
		// loadPacket's pipeline, and parseAndDecryptBrokerMessage's — same
		// three shared steps, see docs/tdtp-protocol-schema.md → "v1.5" →
		// "Consumer: dual-format detection").
		data, err = decryptLegacyBlobIfNeeded(listenCtx, data, opts.MercuryURL)
		if err != nil {
			fmt.Printf("[map:listen] decrypt error (skipping): %v\n", err)
			nackIfAble(br)
			continue
		}
		pkt, err := parser.ParseBytes(data)
		if err != nil {
			fmt.Printf("[map:listen] parse error (skipping): %v\n", err)
			nackIfAble(br)
			continue
		}
		if err := decryptV15PacketIfNeeded(listenCtx, pkt, opts.MercuryURL); err != nil {
			fmt.Printf("[map:listen] decrypt error (skipping): %v\n", err)
			nackIfAble(br)
			continue
		}
		if err := decompressPacketData(pkt); err != nil {
			fmt.Printf("[map:listen] decompress error (skipping): %v\n", err)
			nackIfAble(br)
			continue
		}
		if err := parser.ExpandCompactRows(pkt); err != nil {
			fmt.Printf("[map:listen] expand error (skipping): %v\n", err)
			nackIfAble(br)
			continue
		}

		rows := len(pkt.Data.Rows)
		if err := mapping.Execute(listenCtx, cfg, pkt, opts.DryRun); err != nil {
			fmt.Printf("[map:listen] execute error: %v\n", err)
			nackIfAble(br)
			continue
		}

		// ACK / commit offset only after successful upsert.
		if a, ok := br.(acker); ok {
			if err := a.AckLast(); err != nil {
				fmt.Printf("[map:listen] ack error: %v\n", err)
			}
		}
		if committer, ok := br.(interface{ CommitLast(context.Context) error }); ok {
			_ = committer.CommitLast(listenCtx)
		}

		total += rows
		elapsed := time.Since(t0).Round(time.Millisecond)
		fmt.Printf("[map:listen] ✓  rows=%-6d  total=%-6d  %s\n", rows, total, elapsed)
	}

	fmt.Printf("[map:listen] stopped. total rows upserted: %d\n", total)
	return nil
}

// nackIfAble sends NACK with requeue=true when the broker supports it.
// Used on parse/execute errors so the message returns to the queue.
func nackIfAble(br brokers.MessageBroker) {
	type nacker interface{ NackLast(requeue bool) error }
	if n, ok := br.(nacker); ok {
		_ = n.NackLast(true)
	}
}

// reconnectBroker closes the broker and re-connects with exponential back-off (2s→4s→…→30s).
// Returns nil when the connection is restored, ctx.Err() if the context is cancelled.
// This is the same pattern used in the production QueueBridge: log once on disconnect,
// then reconnect silently — no error-per-second spam during extended outages.
func reconnectBroker(ctx context.Context, br brokers.MessageBroker) error {
	_ = br.Close() // ignore: connection may already be gone at the TCP level
	delay := 2 * time.Second
	const maxDelay = 30 * time.Second
	for {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := br.Connect(ctx); err != nil {
			fmt.Printf("[map:listen] reconnect failed: %v — retry in %v\n", err, delay)
			if delay < maxDelay {
				delay *= 2
			}
			continue
		}
		fmt.Printf("[map:listen] ✓ reconnected to broker\n")
		return nil
	}
}

// isBrokerURI reports whether path is a broker URI (broker://queue-name).
func isBrokerURI(path string) bool {
	return strings.HasPrefix(path, "broker://")
}

// acker is satisfied by broker implementations that support explicit ACK (e.g. RabbitMQ).
type acker interface {
	AckLast() error
}

// loadPacket reads a TDTP packet from a local path, an S3 URI (s3://bucket/key),
// or a broker URI (broker://queue-name), transparently handling the encryption →
// compression → compact layers in that order:
//   - .tdtp.enc input is decrypted via xZMercury (burn-on-read key retrieval)
//   - a compressed Data section (zstd/kanzi) is expanded
//   - a compact v1.3.1 packet is unfolded
//
// For S3 URIs, s3cfg must be non-nil (credentials from mapping YAML input_source.s3).
// For broker URIs, brokercfg must be non-nil (credentials from input_source.broker);
// the queue name in the URI overrides brokercfg.Queue when present.
// One message is consumed and ACKed; the broker connection is closed before returning.
func loadPacket(ctx context.Context, path, mercuryURL string,
	s3cfg *storage.S3Config, brokercfg *brokers.Config) (*packet.DataPacket, error) {
	var data []byte

	switch {
	case isBrokerURI(path):
		if brokercfg == nil {
			return nil, fmt.Errorf("--input is a broker URI but mapping YAML has no input_source.broker section")
		}
		cfg := *brokercfg // copy: URI queue overrides config without mutating the original
		if q := strings.TrimPrefix(path, "broker://"); q != "" {
			cfg.Queue = q
		}
		br, err := brokers.New(cfg)
		if err != nil {
			return nil, fmt.Errorf("broker driver: %w", err)
		}
		defer func() { _ = br.Close() }()
		if err := br.Connect(ctx); err != nil {
			return nil, fmt.Errorf("broker connect: %w", err)
		}
		recvCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		data, err = br.Receive(recvCtx)
		if err != nil {
			return nil, fmt.Errorf("broker receive: %w", err)
		}
		if a, ok := br.(acker); ok {
			if err := a.AckLast(); err != nil {
				return nil, fmt.Errorf("broker ack: %w", err)
			}
		}

	case storage.IsRemote(path):
		if s3cfg == nil {
			return nil, fmt.Errorf("--input is an S3 URI but mapping YAML has no input_source.s3 section")
		}
		_, bucket, key, _ := storage.ParseURI(path)
		cfg := *s3cfg // copy: URI bucket overrides config without mutating the original
		if bucket != "" {
			cfg.Bucket = bucket
		}
		store, err := storage.New(storage.Config{Type: "s3", S3: cfg})
		if err != nil {
			return nil, fmt.Errorf("s3 driver: %w", err)
		}
		defer func() { _ = store.Close() }()
		rc, err := store.Get(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("s3 get %q: %w", key, err)
		}
		defer func() { _ = rc.Close() }()
		data, err = io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("s3 read: %w", err)
		}

	default:
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read input: %w", err)
		}
	}

	// Decrypt first when the input is a legacy v1.3 encrypted blob. Detected
	// by content (binary header) or by the .enc extension — a pipeline may
	// write the encrypted blob to the YAML destination path (often
	// .tdtp.xml). v1.5 packets are valid XML and need no pre-parse check;
	// they're detected after parsing below instead.
	if IsEncryptedFile(path) || IsEncryptedBlob(data) {
		plaintext, derr := DecryptEncBlob(ctx, data, mercuryURL)
		if derr != nil {
			return nil, fmt.Errorf("decrypt: %w", derr)
		}
		data = plaintext
	}

	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(data)
	if err != nil {
		return nil, err
	}

	if err := decryptV15PacketIfNeeded(ctx, pkt, mercuryURL); err != nil {
		return nil, err
	}

	// Decompress (zstd/kanzi) before anything reads the rows — a compressed
	// packet stores all data as a single blob until expanded here.
	if err := decompressPacketData(pkt); err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	// Expand compact rows if needed (must run after decompression).
	if err := parser.ExpandCompactRows(pkt); err != nil {
		return nil, fmt.Errorf("expand compact rows: %w", err)
	}
	return pkt, nil
}
