package packet

import (
	"encoding/binary"
	"math"
	"time"
)

// Эталонные кодеры/декодеры MSSQL TDS, повторяющие поведение
// github.com/denisenkom/go-mssqldb (decodeDateTime / decodeDateTime2).
// Нужны только тестам и бенчмаркам: они дают «честный» вход (сырые байты
// провода) и «честный» эталон (тот путь, которым значение проходит сейчас).

// epochDaysDatetime — дни между 1900-01-01 и 1970-01-01.
const epochDaysDatetime = 25567

// encodeDatetime кодирует момент времени в 8 байт MSSQL DATETIME.
func encodeDatetime(t time.Time) []byte {
	t = t.UTC()
	days := int32(t.Unix()/86400) + epochDaysDatetime
	secOfDay := t.Unix() % 86400
	if secOfDay < 0 {
		secOfDay += 86400
		days--
	}
	// Тики 1/300 секунды, как их пишет сам SQL Server.
	ticks := uint32(secOfDay)*300 + uint32(math.Round(float64(t.Nanosecond())/1e9*300))
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], uint32(days))
	binary.LittleEndian.PutUint32(b[4:8], ticks)
	return b
}

// refDecodeDatetime — эталон go-mssqldb для DATETIME.
func refDecodeDatetime(buf []byte) time.Time {
	days := int32(binary.LittleEndian.Uint32(buf))
	tm := binary.LittleEndian.Uint32(buf[4:])
	ns := int(math.Trunc(float64(tm%300)/0.3+0.5)) * 1000000
	secs := int(tm / 300)
	return time.Date(1900, 1, 1, 0, 0, secs, ns, time.UTC).AddDate(0, 0, int(days))
}

// datetime2TimeLen — длина временной части DATETIME2(scale) на проводе.
func datetime2TimeLen(scale int) int {
	switch {
	case scale <= 2:
		return 3
	case scale <= 4:
		return 4
	default:
		return 5
	}
}

// encodeDatetime2 кодирует момент времени в байты MSSQL DATETIME2(scale).
func encodeDatetime2(t time.Time, scale int) []byte {
	t = t.UTC()
	// Дни от 0001-01-01.
	days := int64(t.Unix()/86400) + 719162
	secOfDay := t.Unix() % 86400
	if secOfDay < 0 {
		secOfDay += 86400
		days--
	}
	frac := int64(t.Nanosecond()) / int64(math.Pow10(9-scale))
	raw := uint64(secOfDay)*uint64(math.Pow10(scale)) + uint64(frac)

	timeLen := datetime2TimeLen(scale)
	b := make([]byte, timeLen+3)
	for i := 0; i < timeLen; i++ {
		b[i] = byte(raw >> (8 * i))
	}
	b[timeLen] = byte(days)
	b[timeLen+1] = byte(days >> 8)
	b[timeLen+2] = byte(days >> 16)
	return b
}

// refDecodeDatetime2 — эталон go-mssqldb для DATETIME2(scale).
func refDecodeDatetime2(buf []byte, scale int) time.Time {
	timeLen := len(buf) - 3
	var raw uint64
	for i := 0; i < timeLen; i++ {
		raw |= uint64(buf[i]) << (8 * i)
	}
	ns := int64(raw) * int64(math.Pow10(9-scale))
	days := int(buf[timeLen]) | int(buf[timeLen+1])<<8 | int(buf[timeLen+2])<<16
	return time.Date(1, 1, 1, 0, 0, 0, int(ns), time.UTC).AddDate(0, 0, days)
}

// formatTimestampCurrent повторяет schema.FormatTimestamp — то, чем сейчас
// сериализуются все даты в TDTP. Дублируется здесь, а не импортируется,
// потому что pkg/core/schema импортирует pkg/core/packet (цикл).
func formatTimestampCurrent(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
