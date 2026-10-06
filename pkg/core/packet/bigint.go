package packet

import (
	"strconv"
	"strings"
)

// BigintDecimal reports a DECIMAL field with scale 0 carrying the "bigint"
// subtype: a 64-bit integer column that travels as DECIMAL.
//
// The case it exists for is Oracle NUMBER(19,0) — what Oracle creates for a
// TDTP INTEGER. Reading it back as INTEGER would be wrong for a native
// NUMBER(19,0) holding a value above int64 (it parses as int64 downstream);
// reading it as plain DECIMAL loses the fact that it is an integer, so a
// round trip turned BIGINT into NUMERIC(19,…) elsewhere. DECIMAL keeps every
// value exact; the subtype tells an importer it may create BIGINT. "bigint"
// is the subtype PostgreSQL and MSSQL already use for INTEGER fields.
func BigintDecimal(f Field) bool {
	return strings.EqualFold(f.Type, "DECIMAL") && f.Scale == 0 && strings.EqualFold(f.Subtype, "bigint")
}

// BigintValue converts a BigintDecimal value for a driver: the exact int64
// when it fits, otherwise the decimal text unchanged — so a value above
// int64 makes a BIGINT column refuse the row loudly instead of being
// rounded. Never through float64, which keeps ~16 digits (9007199254740993
// would become …992). Empty is NULL.
func BigintValue(v string) any {
	if v == "" {
		return nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	return v
}
