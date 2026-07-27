package packet

import (
	"bytes"
	"encoding/xml"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/crypto"
)

// EncryptionAlgoAESGCM is the only algorithm value v1.5 currently writes to
// the encryption="..." attribute on QueryContext/Schema/Data.
const EncryptionAlgoAESGCM = "aes-256-gcm"

// IsEncrypted reports whether any section of pkt still carries opaque v1.5
// ciphertext (QueryContext/Schema/Data.Encryption non-empty). Callers that
// only understand plaintext/compressed packets — e.g. libtdtp's read path,
// which has no xZMercury key access — must check this before treating
// Schema.Fields or Data.Rows as real data: an encrypted packet parses as
// valid XML (Header stays plain) but Schema.Fields is empty and Data.Rows
// holds one opaque blob, not real rows.
func IsEncrypted(pkt *DataPacket) bool {
	if pkt == nil {
		return false
	}
	return pkt.Schema.Encryption != "" || pkt.Data.Encryption != "" ||
		(pkt.QueryContext != nil && pkt.QueryContext.Encryption != "")
}

// EncryptSections turns pkt into a TDTP v1.5 packet: QueryContext, Schema,
// and Data content are each replaced with opaque ciphertext, Header stays
// untouched. Sets pkt.Version = "1.5".
//
// key must be the 32-byte AES-256 key returned by one BindKey call for
// this packet's Header.MessageID — the same key encrypts every section
// (and, for multi-part packets, every part), each with its own random
// nonce. See docs/tdtp-protocol-schema.md → "v1.5" for the full design.
//
// Order matters and is fixed: call this AFTER ComputeIntegrity (xxh3 must
// be computed over plaintext) and AFTER compression (Data.Rows already
// reflects Compression, if any) — never before either.
//
// Calls pkt.MaterializeRows() itself, unconditionally, as the very first
// step. This is not optional: GenerateReference leaves rows sitting in the
// unexported rawRows field until something moves them onto Data.Rows, and
// if that never happens here, writePacketTo's rawRows fast-path silently
// writes the ORIGINAL PLAINTEXT rows into <Data> — right alongside the
// encryption="aes-256-gcm" attribute this function still sets on Data,
// producing a packet that LOOKS encrypted but leaks every row in the
// clear. Every caller of this function must be able to rely on that
// guarantee rather than remembering to materialize first — a single
// forgotten call site anywhere in the codebase would be a silent plaintext
// leak, not a build error.
func EncryptSections(pkt *DataPacket, key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("packet: EncryptSections: key must be 32 bytes, got %d", len(key))
	}

	pkt.MaterializeRows()

	if pkt.QueryContext != nil && pkt.QueryContext.Encryption == "" {
		plaintext, err := xml.Marshal(pkt.QueryContext)
		if err != nil {
			return fmt.Errorf("packet: marshal QueryContext for encryption: %w", err)
		}
		encoded, err := crypto.EncryptSection(key, plaintext)
		if err != nil {
			return fmt.Errorf("packet: encrypt QueryContext: %w", err)
		}
		pkt.QueryContext = &QueryContext{
			Encryption: EncryptionAlgoAESGCM,
			Encrypted:  encoded,
		}
	}

	{
		schemaCopy := pkt.Schema
		schemaCopy.Encryption = ""
		schemaCopy.Encrypted = ""
		plaintext, err := xml.Marshal(schemaCopy)
		if err != nil {
			return fmt.Errorf("packet: marshal Schema for encryption: %w", err)
		}
		encoded, err := crypto.EncryptSection(key, plaintext)
		if err != nil {
			return fmt.Errorf("packet: encrypt Schema: %w", err)
		}
		pkt.Schema = Schema{
			XXH3:       pkt.Schema.XXH3, // integrity attr stays visible — computed pre-encryption
			Encryption: EncryptionAlgoAESGCM,
			Encrypted:  encoded,
		}
	}

	{
		plaintext := renderDataRowsFragment(pkt.Data.Rows)
		encoded, err := crypto.EncryptSection(key, plaintext)
		if err != nil {
			return fmt.Errorf("packet: encrypt Data: %w", err)
		}
		pkt.Data.Encryption = EncryptionAlgoAESGCM
		pkt.Data.Rows = []Row{{Value: encoded}}
		// Compression/Checksum/XXH3 attrs stay visible — they describe the
		// plaintext this ciphertext decrypts to, needed to reverse it.
	}

	pkt.Version = "1.5"
	return nil
}

// DecryptSections reverses EncryptSections in place: for each of
// QueryContext/Schema/Data that carries a non-empty Encryption attribute,
// decrypts Encrypted (or Data's single opaque Row) with key and restores
// the structured fields. Sections without an Encryption attribute are left
// untouched (a v1.5-versioned packet may still carry a plain section if
// the producer chose not to encrypt everything — not the default, but not
// forbidden either).
//
// key must come from one RetrieveKey call for this packet's
// Header.MessageID (burn-on-read — call once, reuse the returned key for
// every section and every part of a multi-part message).
func DecryptSections(pkt *DataPacket, key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("packet: DecryptSections: key must be 32 bytes, got %d", len(key))
	}

	if pkt.QueryContext != nil && pkt.QueryContext.Encryption != "" {
		plaintext, err := crypto.DecryptSection(key, pkt.QueryContext.Encrypted)
		if err != nil {
			return fmt.Errorf("packet: decrypt QueryContext: %w", err)
		}
		var qc QueryContext
		if err := xml.Unmarshal(plaintext, &qc); err != nil {
			return fmt.Errorf("packet: unmarshal decrypted QueryContext: %w", err)
		}
		pkt.QueryContext = &qc
	}

	if pkt.Schema.Encryption != "" {
		plaintext, err := crypto.DecryptSection(key, pkt.Schema.Encrypted)
		if err != nil {
			return fmt.Errorf("packet: decrypt Schema: %w", err)
		}
		var s Schema
		if err := xml.Unmarshal(plaintext, &s); err != nil {
			return fmt.Errorf("packet: unmarshal decrypted Schema: %w", err)
		}
		s.XXH3 = pkt.Schema.XXH3 // preserve the never-encrypted integrity attr
		pkt.Schema = s
	}

	if pkt.Data.Encryption != "" {
		if len(pkt.Data.Rows) != 1 {
			return fmt.Errorf("packet: encrypted Data must carry exactly one opaque Row, got %d", len(pkt.Data.Rows))
		}
		plaintext, err := crypto.DecryptSection(key, pkt.Data.Rows[0].Value)
		if err != nil {
			return fmt.Errorf("packet: decrypt Data: %w", err)
		}
		rows, err := parseDataRowsFragment(plaintext)
		if err != nil {
			return fmt.Errorf("packet: unmarshal decrypted Data: %w", err)
		}
		pkt.Data.Encryption = ""
		pkt.Data.Rows = rows
		// Compression/Checksum/XXH3 attrs untouched — caller decompresses
		// and verifies integrity next, same as any non-encrypted packet.
	}

	return nil
}

// renderDataRowsFragment renders rows as the exact <R>...</R>* byte
// sequence writePacketTo would have written for them (one <R> per row,
// XML-chardata-escaped) — this is the plaintext EncryptSections seals for
// Data, so DecryptSections/parseDataRowsFragment must parse the identical
// shape back.
func renderDataRowsFragment(rows []Row) []byte {
	var buf bytes.Buffer
	for _, r := range rows {
		buf.WriteString("<R>")
		_ = xml.EscapeText(&buf, []byte(r.Value))
		buf.WriteString("</R>")
	}
	return buf.Bytes()
}

// parseDataRowsFragment parses bytes produced by renderDataRowsFragment
// back into []Row.
func parseDataRowsFragment(fragment []byte) ([]Row, error) {
	wrapped := append([]byte("<Rows>"), fragment...)
	wrapped = append(wrapped, []byte("</Rows>")...)

	var scratch struct {
		Rows []Row `xml:"R"`
	}
	if err := xml.Unmarshal(wrapped, &scratch); err != nil {
		return nil, err
	}
	return scratch.Rows, nil
}
