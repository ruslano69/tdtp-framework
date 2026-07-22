package commands

// encrypt.go — shared encryption / decryption helpers for standalone --enc tier.
//
// Encryption tier overview
// ─────────────────────────────────────────────────────────────────────────────
// Producer  (--export TABLE --enc --mercury-url http://... --output file.tdtp.enc):
//   1. Export table → TDTP XML (with optional compression / v1.4 integrity).
//   2. GenerateUUID → BindKey via xZMercury → AES-256-GCM encrypt.
//   3. Write binary blob: [2B ver][1B algo][16B uuid][12B nonce][ciphertext]
//
// Consumer  (--import file.tdtp.enc --mercury-url http://...):
//   1. Auto-detect .tdtp.enc extension.
//   2. ExtractUUID from blob header.
//   3. RetrieveKey via xZMercury (burn-on-read — key deleted after call).
//   4. Decrypt → TDTP XML plaintext → parse, import normally.
//
// Same decrypt step applies to --to-csv / --to-xlsx / --to-html.
//
// Server secret (HMAC verification):
//   The producer reads MERCURY_SERVER_SECRET from the environment.
//   Empty env var → HMAC verification is skipped (dev / internal-only setups).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	tdtpcrypto "github.com/ruslano69/tdtp-framework/pkg/crypto"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// IsEncryptedFile returns true when path ends with ".tdtp.enc" or ".enc".
// Used by import / converters to auto-detect encrypted input.
func IsEncryptedFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".tdtp.enc") || strings.HasSuffix(lower, ".enc")
}

// IsEncryptedBlob reports whether blob carries the encryption header
// ([2B ver][1B algo][16B uuid]…). Content-based detection, independent of the
// file extension — a pipeline may write an encrypted blob to a .tdtp.xml path.
func IsEncryptedBlob(blob []byte) bool {
	_, err := tdtpcrypto.ExtractUUID(blob)
	return err == nil
}

// DecryptEncBlob decrypts a binary blob produced by EncryptPacket / pipeline encryption.
// It extracts the package UUID from the blob header, retrieves the AES-256 key from
// xZMercury (burn-on-read), and returns the plaintext TDTP XML bytes.
//
// mercuryURL must be non-empty; this function does not support dev-local mode
// because burn-on-read requires a running Mercury instance.
func DecryptEncBlob(ctx context.Context, blob []byte, mercuryURL string) ([]byte, error) {
	if mercuryURL == "" {
		return nil, fmt.Errorf(
			"decryption requires --mercury-url: encrypted files use xZMercury burn-on-read key retrieval")
	}

	// Extract UUID from the binary header (no decryption needed yet).
	packageUUID, err := tdtpcrypto.ExtractUUID(blob)
	if err != nil {
		return nil, fmt.Errorf("extract uuid from enc blob: %w", err)
	}
	fmt.Printf("  Encrypted package UUID: %s\n", packageUUID)

	// Retrieve key from xZMercury (burn-on-read).
	mc := mercury.NewClient(mercuryURL, 5000)
	caller := os.Getenv("TDTPCLI_CALLER") // optional consumer identity for Mercury audit trail
	keyB64, err := mc.RetrieveKey(ctx, packageUUID, caller)
	if err != nil {
		var burnedErr *mercury.KeyBurnedError
		if errors.As(err, &burnedErr) {
			if burnedErr.Mode == "dev" {
				fmt.Fprintf(os.Stderr,
					"\n⚠  DEV-FAILOVER BURN: key for package %s was burned by a dev-mode Mercury instance.\n"+
						"   This is expected during Redis cluster outage failover — not a theft alert.\n"+
						"   ServerMode: dev  BurnedAt: %s\n\n",
					packageUUID, burnedErr.BurnedAt.Format(time.RFC3339))
			} else {
				fmt.Fprintf(os.Stderr,
					"\n🚨 SECURITY ALERT: key for package %s was already burned in PROD mode.\n"+
						"   The encrypted file may have been intercepted by another party.\n"+
						"   ServerMode: %s  BurnedAt: %s\n"+
						"   Check Mercury audit logs immediately.\n\n",
					packageUUID, burnedErr.Mode, burnedErr.BurnedAt.Format(time.RFC3339))
			}
		} else if errors.Is(err, mercury.ErrKeyExpired) {
			fmt.Fprintf(os.Stderr,
				"\n⚠  KEY EXPIRED: key for package %s not found (TTL expired or UUID never existed).\n\n",
				packageUUID)
		}
		return nil, fmt.Errorf("retrieve key from Mercury (uuid=%s): %w", packageUUID, err)
	}

	key, err := mercury.DecodeKey(keyB64)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}

	// Decrypt AES-256-GCM.
	_, plaintext, err := tdtpcrypto.Decrypt(key, blob)
	if err != nil {
		return nil, fmt.Errorf("AES-256-GCM decrypt (uuid=%s): %w", packageUUID, err)
	}

	return plaintext, nil
}

// DecryptEncFile reads path, detects encryption, and returns plaintext TDTP XML.
// Non-encrypted files are returned as-is (pass-through).
func DecryptEncFile(ctx context.Context, path, mercuryURL string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}

	if IsEncryptedFile(path) {
		fmt.Printf("  Encrypted file detected — decrypting via xZMercury...\n")
		return DecryptEncBlob(ctx, data, mercuryURL)
	}
	return data, nil
}

// WriteErrorPacket serializes a TDTP error packet to path.
// Used to produce a "receipt" when decryption fails so the pipeline receives
// a structured error instead of a missing output file.
// serverMode is the xZMercury server mode ("dev"/"prod") from the burn marker —
// empty string when not applicable. It populates AlarmDetails.ServerMode so that
// monitoring can filter dev-failover burns from prod-theft events.
// If path is empty or "-", writes to stdout.
func WriteErrorPacket(code, message, table, inReplyTo, serverMode, path string) error {
	pkt := packet.NewErrorPacket(code, message, table, inReplyTo, serverMode)
	gen := packet.NewGenerator()
	xmlData, err := gen.ToXML(pkt, false)
	if err != nil {
		return fmt.Errorf("serialize error packet: %w", err)
	}
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(xmlData)
		return err
	}
	return os.WriteFile(path, xmlData, 0o644)
}

// EncryptPacket serializes pkt to TDTP XML, then encrypts it via xZMercury BindKey.
// Returns the binary blob and the package UUID bound to the key.
//
// pipelineName is embedded in the BindKey request for ACL enforcement.
// serverSecret is read from MERCURY_SERVER_SECRET env var when empty.
func EncryptPacket(ctx context.Context, pkt *packet.DataPacket, mercuryURL, pipelineName string) (blob []byte, packageUUID string, err error) {
	if mercuryURL == "" {
		return nil, "", fmt.Errorf("--enc requires --mercury-url pointing at a running xZMercury instance")
	}

	// Serialize to TDTP XML first.
	gen := packet.NewGenerator()
	xmlData, err := gen.ToXML(pkt, true)
	if err != nil {
		return nil, "", fmt.Errorf("marshal packet to XML: %w", err)
	}

	// Generate package UUID.
	packageUUID = packet.GenerateUUID()

	// Build FileEncryptor with the real Mercury client.
	mc := mercury.NewClient(mercuryURL, 5000)
	serverSecret := os.Getenv("MERCURY_SERVER_SECRET")
	encryptor := processors.NewFileEncryptor(mc, serverSecret, packageUUID, pipelineName)

	result, errCode, encErr := encryptor.Encrypt(ctx, xmlData)
	if encErr != nil {
		return nil, "", fmt.Errorf("encrypt [%s]: %w", errCode, encErr)
	}

	return result.Encrypted, packageUUID, nil
}

// EncryptPacketV15 encrypts pkt in place using TDTP v1.5 section-level
// encryption (QueryContext/Schema/Data each turn opaque; Header stays
// plain XML) and returns the resulting XML bytes.
//
// Unlike EncryptPacket (--enc13, legacy whole-blob format), the package
// UUID bound at xZMercury is always pkt.Header.MessageID — never a freshly
// generated one — because a v1.5 consumer must be able to read the UUID
// straight from the plain Header before decrypting anything. For
// multi-part packets, this is simply called once per part: each part
// already carries its own distinct Header.MessageID ("{base}-P{n}", set
// by GenerateReference), so each part's BindKey call lands on its own
// Redis key — no shared identifier, no overwrite race, no special
// handling needed (see docs/tdtp-protocol-schema.md → "v1.5" →
// "Multi-part packets").
//
// pkt must already have ComputeIntegrity (and compression, if enabled)
// applied — this function does not run either; order is fixed
// (hash -> compress -> encrypt) and is the caller's responsibility.
func EncryptPacketV15(ctx context.Context, pkt *packet.DataPacket, mercuryURL, pipelineName string) (xmlData []byte, packageUUID string, err error) {
	if mercuryURL == "" {
		return nil, "", fmt.Errorf("--enc requires --mercury-url pointing at a running xZMercury instance")
	}

	packageUUID = pkt.Header.MessageID
	if packageUUID == "" {
		return nil, "", fmt.Errorf("encrypt v1.5: packet Header.MessageID is empty — cannot bind a key without it")
	}

	key, err := bindAndVerifyKey(ctx, mercuryURL, packageUUID, pipelineName)
	if err != nil {
		return nil, "", err
	}

	if err := packet.EncryptSections(pkt, key); err != nil {
		return nil, "", fmt.Errorf("encrypt sections: %w", err)
	}

	gen := packet.NewGenerator()
	xmlData, err = gen.ToXML(pkt, true)
	if err != nil {
		return nil, "", fmt.Errorf("marshal encrypted packet to XML: %w", err)
	}

	return xmlData, packageUUID, nil
}

// bindAndVerifyKey calls xZMercury BindKey and verifies the HMAC exactly
// like processors.FileEncryptor.Encrypt does for the legacy path — shared
// here so v1.5 gets the identical ACL/quota/HMAC guarantees without
// duplicating that logic's security-relevant details.
func bindAndVerifyKey(ctx context.Context, mercuryURL, packageUUID, pipelineName string) ([]byte, error) {
	mc := mercury.NewClient(mercuryURL, 5000)
	binding, err := mc.BindKey(ctx, packageUUID, pipelineName)
	if err != nil {
		return nil, fmt.Errorf("bind key: %w", err)
	}

	serverSecret := os.Getenv("MERCURY_SERVER_SECRET")
	if serverSecret == "" {
		return nil, fmt.Errorf("%w: MERCURY_SERVER_SECRET not set — "+
			"HMAC verification is mandatory; use serverSecret=\"dev-mode\" to opt out explicitly",
			mercury.ErrHMACVerificationFailed)
	}
	if serverSecret != "dev-mode" {
		if !mercury.VerifyHMAC(packageUUID, binding.HMAC, serverSecret, binding.Mode) {
			return nil, fmt.Errorf("%w: uuid=%s mode=%s", mercury.ErrHMACVerificationFailed, packageUUID, binding.Mode)
		}
	}

	key, err := mercury.DecodeKey(binding.KeyB64)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	return key, nil
}

// IsEncryptedPacket reports whether a parsed DataPacket carries v1.5
// section-level encryption (QueryContext/Schema/Data attribute-based),
// as opposed to IsEncryptedBlob's whole-packet binary envelope (v1.0-v1.4,
// --enc13). The two formats are mutually exclusive and auto-detected from
// different signals: IsEncryptedBlob inspects raw bytes before any XML
// parse; IsEncryptedPacket inspects an already-parsed packet's attributes.
func IsEncryptedPacket(pkt *packet.DataPacket) bool {
	if pkt == nil {
		return false
	}
	return pkt.Schema.Encryption != "" || pkt.Data.Encryption != "" ||
		(pkt.QueryContext != nil && pkt.QueryContext.Encryption != "")
}

// DecryptPacketV15 retrieves the AES-256 key for pkt.Header.MessageID from
// xZMercury (burn-on-read — call once, reuse for every part of a
// multi-part message) and decrypts every encrypted section in place.
func DecryptPacketV15(ctx context.Context, pkt *packet.DataPacket, mercuryURL string) error {
	if mercuryURL == "" {
		return fmt.Errorf(
			"decryption requires --mercury-url: v1.5 packets use xZMercury burn-on-read key retrieval")
	}

	packageUUID := pkt.Header.MessageID
	if packageUUID == "" {
		return fmt.Errorf("decrypt v1.5: packet Header.MessageID is empty — cannot retrieve a key without it")
	}
	fmt.Printf("  v1.5 encrypted packet UUID: %s\n", packageUUID)

	mc := mercury.NewClient(mercuryURL, 5000)
	caller := os.Getenv("TDTPCLI_CALLER")
	keyB64, err := mc.RetrieveKey(ctx, packageUUID, caller)
	if err != nil {
		var burnedErr *mercury.KeyBurnedError
		if errors.As(err, &burnedErr) {
			if burnedErr.Mode == "dev" {
				fmt.Fprintf(os.Stderr,
					"\n⚠  DEV-FAILOVER BURN: key for package %s was burned by a dev-mode Mercury instance.\n"+
						"   ServerMode: dev  BurnedAt: %s\n\n",
					packageUUID, burnedErr.BurnedAt.Format(time.RFC3339))
			} else {
				fmt.Fprintf(os.Stderr,
					"\n🚨 SECURITY ALERT: key for package %s was already burned in PROD mode.\n"+
						"   ServerMode: %s  BurnedAt: %s\n\n",
					packageUUID, burnedErr.Mode, burnedErr.BurnedAt.Format(time.RFC3339))
			}
		} else if errors.Is(err, mercury.ErrKeyExpired) {
			fmt.Fprintf(os.Stderr,
				"\n⚠  KEY EXPIRED: key for package %s not found (TTL expired or UUID never existed).\n\n",
				packageUUID)
		}
		return fmt.Errorf("retrieve key from Mercury (uuid=%s): %w", packageUUID, err)
	}

	key, err := mercury.DecodeKey(keyB64)
	if err != nil {
		return fmt.Errorf("decode key: %w", err)
	}

	if err := packet.DecryptSections(pkt, key); err != nil {
		return fmt.Errorf("decrypt sections (uuid=%s): %w", packageUUID, err)
	}
	return nil
}
