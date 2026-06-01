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
		if errors.Is(err, mercury.ErrKeyAlreadyConsumed) {
			// 404 from Mercury: key was already burned — this may indicate
			// that an attacker retrieved the key before the legitimate consumer.
			// Print a prominent warning so operators can investigate.
			fmt.Fprintf(os.Stderr,
				"\n⚠  SECURITY WARNING: key for package %s was already consumed or expired.\n"+
					"   If you did not retrieve this package before, the encrypted file may have been\n"+
					"   intercepted. Contact your security team and check Mercury audit logs.\n\n",
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
// If path is empty or "-", writes to stdout.
func WriteErrorPacket(code, message, table, inReplyTo, path string) error {
	pkt := packet.NewErrorPacket(code, message, table, inReplyTo)
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
