package packet

import "time"

// NewErrorPacket creates a minimal valid TDTP error packet (Type="error") that
// can be written to a file and imported by any TDTP consumer.
//
// This is the "receipt" mechanism: when an operation fails (e.g. key-not-found
// on decrypt), the caller writes this packet to the output path instead of
// producing nothing. The receiving side imports it as a normal TDTP packet and
// sees the structured error rather than a missing file / silent failure.
//
// Fields:
//   - code       — machine-readable error code (e.g. "KEY_BURNED_BY_OTHER")
//   - message    — human-readable description
//   - table      — table name the operation was attempted on (may be empty)
//   - inReplyTo  — original MessageID of the request (may be empty)
//   - serverMode — xZMercury mode ("dev"/"prod") from burn marker; empty if not applicable.
//     "dev" = dev-failover burn (expected during Redis outage, not a theft alert).
//     "prod" = prod burn by unknown party (requires investigation).
func NewErrorPacket(code, message, table, inReplyTo, serverMode string) *DataPacket {
	pkt := NewDataPacket(TypeError, table)
	pkt.Version = "1.4"
	pkt.Header.InReplyTo = inReplyTo
	pkt.Header.Timestamp = time.Now().UTC()
	pkt.AlarmDetails = &AlarmDetails{
		Severity:   "error",
		Code:       code,
		Message:    message,
		ServerMode: serverMode,
	}
	// Empty schema and data — error packets carry no rows.
	pkt.Schema = Schema{}
	pkt.Data = Data{}
	return pkt
}

// NeedsRowCountCheck reports whether a packet with the given version string
// requires RecordsInPart to be validated against the actual row count.
//
// Starting with v1.4, packets carry XXH3-128 hashes that guarantee integrity
// end-to-end, making the RecordsInPart counter redundant as a safety check.
//
// This same predicate also gates pkg/pipeline's VerifyAndPrepare pre-flight
// (via the inverse: !NeedsRowCountCheck(version) → run the Mercury pre-flight)
// — meaning it doubles as "does this packet's version REQUIRE a registered
// xxh3 hash to be importable." That's a real assumption, not just a naming
// coincidence: any producer of a version >= "1.4" packet (this includes
// v1.5 encryption, whose packets always carry Version="1.5") MUST have
// called ComputeIntegrity + RegisterHash, or the consumer-side gate blocks
// it with HASH_NOT_REGISTERED — found live via a v1.5 packet that skipped
// this. See pkg/pipeline/produce.go's ComputeAndRegisterIntegrity, which
// every v1.5 encryption call site now calls for exactly this reason.
func NeedsRowCountCheck(version string) bool {
	return version <= "1.3.1"
}

// ExtractKeyFields извлекает ключевые поля из схемы
func ExtractKeyFields(schema Schema) []string {
	var keys []string
	for _, field := range schema.Fields {
		if field.Key {
			keys = append(keys, field.Name)
		}
	}
	return keys
}

// GetFieldIndices возвращает индексы полей по их именам
func GetFieldIndices(schema Schema, fieldNames []string) []int {
	var indices []int
	for _, name := range fieldNames {
		for i, field := range schema.Fields {
			if field.Name == name {
				indices = append(indices, i)
				break
			}
		}
	}
	return indices
}

// ParseRows парсит TDTP строки в [][]string
func ParseRows(rows []Row, parser *Parser) [][]string {
	result := make([][]string, len(rows))
	for i, row := range rows {
		result[i] = parser.GetRowValues(row)
	}
	return result
}
