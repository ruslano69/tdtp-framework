package mssql

// encodeFromIndex encodes bytes from a given start index without leading zeros.
func encodeFromIndex(data []byte, start int) string {
	const hexChars = "0123456789ABCDEF"

	// Pre-calculate result size
	resultLen := 0
	if data[start] < 0x10 {
		resultLen = 1
	} else {
		resultLen = 2
	}
	resultLen += 2 * (len(data) - start - 1)

	result := make([]byte, resultLen)
	idx := 0

	// Handle first byte (may have leading zero nibble)
	firstByte := data[start]
	if firstByte >= 0x10 {
		result[idx] = hexChars[firstByte>>4]
		idx++
	}
	result[idx] = hexChars[firstByte&0x0F]
	idx++

	// Process remaining bytes (full 2 chars each)
	for i := start + 1; i < len(data); i++ {
		b := data[i]
		result[idx] = hexChars[b>>4]
		result[idx+1] = hexChars[b&0x0F]
		idx += 2
	}

	return string(result)
}

// bytesToHexWithoutLeadingZerosSQL is a specialized function for MS SQL Server
// timestamp values. It assumes 8-byte input and processes the data efficiently.
//
// Input format: MS SQL Server datetime2/timestamp (8 bytes, big-endian)
// Output: Hex string without leading zeros
//
// Examples:
//   - []byte{0x00, 0x00, 0x00, 0x00, 0x18, 0x7F, 0x86, 0x3C} → "187F863C"
//   - []byte{0x00, 0x00, 0x00, 0x00, 0x18, 0x7F, 0x86, 0x40} → "187F8640"
//   - []byte{0x00, 0x00, 0x00, 0x19, 0xA4, 0xAE, 0x7C, 0x00} → "19A4AE7C00"
func bytesToHexWithoutLeadingZerosSQL(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// For SQL Server timestamps, find the first non-zero byte
	// The first 4 bytes may be zero for recent timestamps,
	// but they can also contain non-zero values
	start := 0
	for start < 8 && start < len(data) && data[start] == 0 {
		start++
	}

	// All zeros
	if start >= len(data) {
		return "00"
	}

	return encodeFromIndex(data, start)
}
