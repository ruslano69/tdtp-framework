package packet

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"fmt"

	"github.com/zeebo/xxh3"
)

// IntegrityResult carries the three XXH3-128 hashes computed for a packet.
//
// The three-level model:
//
//	SchemaXXH3  = xxh3_128(canonical Schema XML)        — "паспорт данных"
//	DataXXH3    = xxh3_128(raw row values)               — "данные"
//	PacketXXH3  = xxh3_128(SchemaXXH3 + "|" + DataXXH3) — "odtp fingerprint"
//
// All values are 32-char lowercase hex strings (128 bits = 16 bytes).
type IntegrityResult struct {
	SchemaXXH3 string // xxh3_128 of canonical Schema bytes (without xxh3 attr)
	DataXXH3   string // xxh3_128 of raw row values (before compression)
	PacketXXH3 string // xxh3_128(SchemaXXH3 + "|" + DataXXH3) — packet fingerprint
}

// ComputeIntegrity calculates the three-level XXH3 integrity hashes for pkt
// and stamps them onto the packet in place:
//
//	pkt.Schema.XXH3  ← SchemaXXH3
//	pkt.Data.XXH3    ← DataXXH3
//	pkt.XXH3         ← PacketXXH3 (the "odtp" fingerprint)
//
// Call after rows are materialized and BEFORE compression/serialization.
// The existing pkt.Data.Checksum (xxh3_64 of compressed blob) is unaffected.
func ComputeIntegrity(pkt *DataPacket) (*IntegrityResult, error) {
	pkt.MaterializeRows()

	result, err := computeHashes(pkt)
	if err != nil {
		return nil, err
	}

	// Stamp onto packet
	pkt.Schema.XXH3 = result.SchemaXXH3
	pkt.Data.XXH3 = result.DataXXH3
	pkt.XXH3 = result.PacketXXH3

	return result, nil
}

// VerifyIntegrity checks that a parsed packet's XXH3 hashes match the payload.
// Returns nil if:
//   - The packet carries no XXH3 hashes (not a v1.4 integrity packet — silently OK)
//   - All three hashes match the recomputed values
//
// Returns a descriptive error naming which hash failed.
func VerifyIntegrity(pkt *DataPacket) error {
	if pkt.XXH3 == "" {
		return nil // packet was not stamped — nothing to verify
	}

	pkt.MaterializeRows()

	// Save the stored hashes before computeHashes clears Schema.XXH3 in its copy
	storedSchema := pkt.Schema.XXH3
	storedData := pkt.Data.XXH3
	storedPacket := pkt.XXH3

	result, err := computeHashes(pkt) // uses Schema copy with XXH3="" internally
	if err != nil {
		return fmt.Errorf("integrity verify: %w", err)
	}

	if result.SchemaXXH3 != storedSchema {
		return fmt.Errorf("integrity: schema hash mismatch\n  stored:   %s\n  computed: %s",
			storedSchema, result.SchemaXXH3)
	}
	if result.DataXXH3 != storedData {
		return fmt.Errorf("integrity: data hash mismatch\n  stored:   %s\n  computed: %s",
			storedData, result.DataXXH3)
	}
	if result.PacketXXH3 != storedPacket {
		return fmt.Errorf("integrity: packet hash mismatch\n  stored:   %s\n  computed: %s",
			storedPacket, result.PacketXXH3)
	}

	return nil
}

// HasIntegrity reports whether the packet carries XXH3 integrity hashes.
// Fast pre-flight check: reads only the root attribute — no rows needed.
func HasIntegrity(pkt *DataPacket) bool {
	return pkt.XXH3 != ""
}

// computeHashes does the actual hashing without modifying pkt.
//
// Salt strategy: the packet's MessageID (UUID) is written as the first
// bytes of every hash input. This means:
//   - Two packets with identical schema+data get different hashes.
//   - A captured hash cannot be replayed against a different packet.
//   - The salt is not secret — it lives in the plaintext Header.
//
// The Schema is copied and its XXH3 attr zeroed before marshaling
// to avoid circular dependency.
func computeHashes(pkt *DataPacket) (*IntegrityResult, error) {
	salt := []byte(pkt.Header.MessageID) // UUID, e.g. "550e8400-e29b-41d4-a716-446655440000"

	// ── 1. Schema hash ──────────────────────────────────────────────────────
	// Layout: [UUID bytes][canonical Schema XML bytes]
	// Schema is marshaled without its own xxh3 attr to avoid circularity.
	schemaCopy := pkt.Schema
	schemaCopy.XXH3 = ""
	schemaBytes, err := xml.Marshal(schemaCopy)
	if err != nil {
		return nil, fmt.Errorf("integrity: marshal schema: %w", err)
	}
	var schemaBuf bytes.Buffer
	schemaBuf.Grow(len(salt) + len(schemaBytes))
	schemaBuf.Write(salt)
	schemaBuf.Write(schemaBytes)
	schemaHash := xxh3.Hash128(schemaBuf.Bytes())
	schemaHex := uint128Hex(schemaHash)

	// ── 2. Data hash ────────────────────────────────────────────────────────
	// Layout: [UUID bytes][row₀\n][row₁\n]...[rowN\n]
	// Computed on raw row values before compression.
	var rowsBuf bytes.Buffer
	rowsBuf.Write(salt)
	for _, row := range pkt.Data.Rows {
		rowsBuf.WriteString(row.Value)
		rowsBuf.WriteByte('\n')
	}
	dataHash := xxh3.Hash128(rowsBuf.Bytes())
	dataHex := uint128Hex(dataHash)

	// ── 3. Packet fingerprint ───────────────────────────────────────────────
	// xxh3_128 of the two component hashes joined by "|".
	// The UUID salt is already baked into both component hashes,
	// so the fingerprint is implicitly salted without redundancy.
	combined := schemaHex + "|" + dataHex
	packetHash := xxh3.Hash128([]byte(combined))
	packetHex := uint128Hex(packetHash)

	return &IntegrityResult{
		SchemaXXH3: schemaHex,
		DataXXH3:   dataHex,
		PacketXXH3: packetHex,
	}, nil
}

// uint128Hex converts a 128-bit XXH3 hash to a 32-char lowercase hex string.
// Byte order: Hi (most significant 64 bits) first, Lo last — big-endian.
func uint128Hex(u xxh3.Uint128) string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[0:8], u.Hi)
	binary.BigEndian.PutUint64(b[8:16], u.Lo)
	return hex.EncodeToString(b[:])
}
