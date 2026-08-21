package packet

import (
	"encoding/binary"
	"errors"
	"math"
	"time"
	"unsafe"
)

var (
	errDatetimeSize  = errors.New("datetime requires 8 bytes")
	errInvalidScale  = errors.New("invalid datetime2 scale (must be 0..7)")
	errDatetime2Size = errors.New("invalid datetime2 byte length")
)

// Двухзначная таблица для мгновенной конвертации 0..99 в 2 ASCII-символа.
const digitsTable = "" +
	"00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

// pow10[i] == 10^i.
var pow10 = [8]uint64{
	1,
	10,
	100,
	1000,
	10000,
	100000,
	1000000,
	10000000,
}

// put2 записывает 2 ASCII-цифры в buf по индексу v (0..99).
func put2(buf []byte, v int) {
	i := v << 1
	buf[0] = digitsTable[i]
	buf[1] = digitsTable[i+1]
}

// put4 записывает 4 ASCII-цифры (год) в buf по индексу v (0..9999).
func put4(buf []byte, v int) {
	hi := v / 100
	lo := v - hi*100

	i := hi << 1
	buf[0] = digitsTable[i]
	buf[1] = digitsTable[i+1]

	i = lo << 1
	buf[2] = digitsTable[i]
	buf[3] = digitsTable[i+1]
}

// civilFromDaysUnix переводит дни (от 1970-01-01) в (year, month, day).
// Алгоритм Howard Hinnant (0 циклов, 0 ветвлений).
func civilFromDaysUnix(z int32) (year, month, day int) {
	z += 719468

	era := z / 146097
	if z < 0 {
		era = (z - 146096) / 146097
	}

	doe := uint32(z - era*146097)
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365

	y := int32(yoe) + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)

	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1

	m := mp + 3
	if mp >= 10 {
		m = mp - 9
	}

	if m <= 2 {
		y++
	}

	return int(y), int(m), int(d)
}

// -----------------------------------------------------------------------------
// MSSQL DATETIME (8 байт: 4 байта дни от 1900-01-01, 4 байта тики 1/300 сек)
// -----------------------------------------------------------------------------

// AppendMSSQLDatetime форматирует MSSQL DATETIME в dst без аллокаций памяти.
//
// ВНИМАНИЕ: границы не проверяются. b должен быть длиной ровно 8 байт, а поле
// дней — давать год 0001..9999. На битых байтах (год за 9999) put4 уходит за
// пределы digitsTable и функция паникует. Вход обязан быть проверен вызывающим
// кодом — либо используйте FormatMSSQLDatetime, который проверяет длину.
func AppendMSSQLDatetime(dst []byte, b []byte) []byte {
	days := int32(binary.LittleEndian.Uint32(b[0:4]))
	ticks := binary.LittleEndian.Uint32(b[4:8])

	// 25567 — разница в днях между 1900-01-01 и 1970-01-01.
	year, month, day := civilFromDaysUnix(days - 25567)

	totalSec := ticks / 300
	remTicks := ticks - totalSec*300

	hour := int(totalSec / 3600)
	min := int((totalSec / 60) % 60)
	sec := int(totalSec % 60)

	n := len(dst)

	if remTicks == 0 {
		// Гарантируем емкость на 20 байт
		if cap(dst)-n < 20 {
			dst = append(dst, make([]byte, 20)...)[:n]
		}
		dst = dst[:n+20]
		out := dst[n:]

		put4(out[0:4], year)
		out[4] = '-'
		put2(out[5:7], month)
		out[7] = '-'
		put2(out[8:10], day)
		out[10] = 'T'
		put2(out[11:13], hour)
		out[13] = ':'
		put2(out[14:16], min)
		out[16] = ':'
		put2(out[17:19], sec)
		out[19] = 'Z'

		return dst
	}

	// 300 тиков = 1 секунда => переводим в миллисекунды.
	ms := int((remTicks*1000 + 150) / 300)

	// Гарантируем емкость на 24 байта
	if cap(dst)-n < 24 {
		dst = append(dst, make([]byte, 24)...)[:n]
	}
	dst = dst[:n+24]
	out := dst[n:]

	put4(out[0:4], year)
	out[4] = '-'
	put2(out[5:7], month)
	out[7] = '-'
	put2(out[8:10], day)
	out[10] = 'T'
	put2(out[11:13], hour)
	out[13] = ':'
	put2(out[14:16], min)
	out[16] = ':'
	put2(out[17:19], sec)
	out[19] = '.'
	put2(out[20:22], ms/10)
	out[22] = byte('0' + ms%10)
	out[23] = 'Z'

	return dst
}

// -----------------------------------------------------------------------------
// MSSQL DATETIME2 (3..5 байт время, 3 байта дата от 0001-01-01)
// -----------------------------------------------------------------------------

// AppendMSSQLDatetime2 форматирует MSSQL DATETIME2 в dst без аллокаций памяти.
//
// ВНИМАНИЕ: границы не проверяются — см. предупреждение у AppendMSSQLDatetime.
// scale обязан быть 0..7, len(b) — timeLen(scale)+3.
func AppendMSSQLDatetime2(dst []byte, b []byte, scale int) []byte {
	timeLen := 5
	if scale <= 2 {
		timeLen = 3
	} else if scale <= 4 {
		timeLen = 4
	}

	var timeVal uint64
	switch timeLen {
	case 3:
		timeVal = uint64(b[0]) |
			uint64(b[1])<<8 |
			uint64(b[2])<<16
	case 4:
		timeVal = uint64(b[0]) |
			uint64(b[1])<<8 |
			uint64(b[2])<<16 |
			uint64(b[3])<<24
	case 5:
		timeVal = uint64(b[0]) |
			uint64(b[1])<<8 |
			uint64(b[2])<<16 |
			uint64(b[3])<<24 |
			uint64(b[4])<<32
	}

	datePos := timeLen
	days := uint32(b[datePos]) |
		uint32(b[datePos+1])<<8 |
		uint32(b[datePos+2])<<16

	// 719162 — разница в днях между 0001-01-01 и 1970-01-01.
	year, month, day := civilFromDaysUnix(int32(days) - 719162)

	divisor := pow10[scale]
	totalSec := timeVal / divisor
	frac := timeVal - totalSec*divisor

	hour := int(totalSec / 3600)
	min := int((totalSec / 60) % 60)
	sec := int(totalSec % 60)

	n := len(dst)

	if scale == 0 {
		if cap(dst)-n < 20 {
			dst = append(dst, make([]byte, 20)...)[:n]
		}
		dst = dst[:n+20]
		out := dst[n:]

		put4(out[0:4], year)
		out[4] = '-'
		put2(out[5:7], month)
		out[7] = '-'
		put2(out[8:10], day)
		out[10] = 'T'
		put2(out[11:13], hour)
		out[13] = ':'
		put2(out[14:16], min)
		out[16] = ':'
		put2(out[17:19], sec)
		out[19] = 'Z'

		return dst
	}

	reqLen := n + 21 + scale
	if cap(dst) < reqLen {
		dst = append(dst, make([]byte, 21+scale)...)[:n]
	}
	dst = dst[:reqLen]
	out := dst[n:]

	put4(out[0:4], year)
	out[4] = '-'
	put2(out[5:7], month)
	out[7] = '-'
	put2(out[8:10], day)
	out[10] = 'T'
	put2(out[11:13], hour)
	out[13] = ':'
	put2(out[14:16], min)
	out[16] = ':'
	put2(out[17:19], sec)
	out[19] = '.'

	// Полный unroll форматирования дробной части
	switch scale {
	case 1:
		out[20] = byte('0' + frac)
	case 2:
		put2(out[20:22], int(frac))
	case 3:
		v := int(frac)
		out[20] = byte('0' + v/100)
		v -= (v / 100) * 100
		i := v << 1
		out[21] = digitsTable[i]
		out[22] = digitsTable[i+1]
	case 4:
		put4(out[20:24], int(frac))
	case 5:
		v := int(frac)
		a := v / 100
		b := v - a*100
		out[20] = byte('0' + a/100)
		a -= (a / 100) * 100
		i := a << 1
		out[21] = digitsTable[i]
		out[22] = digitsTable[i+1]
		i = b << 1
		out[23] = digitsTable[i]
		out[24] = digitsTable[i+1]
	case 6:
		v := int(frac)
		a := v / 10000
		b := v - a*10000
		put2(out[20:22], a)
		put4(out[22:26], b)
	case 7:
		v := int(frac)
		a := v / 10000
		b := v - a*10000
		out[20] = byte('0' + a/100)
		a -= (a / 100) * 100
		i := a << 1
		out[21] = digitsTable[i]
		out[22] = digitsTable[i+1]
		put4(out[23:27], b)
	}

	out[20+scale] = 'Z'
	return dst
}

// -----------------------------------------------------------------------------
// Safe Public Wrappers (когда нужен именно string)
// -----------------------------------------------------------------------------

// FormatMSSQLDatetime возвращает строку ISO-8601 для 8-байтового DATETIME.
func FormatMSSQLDatetime(b []byte) (string, error) {
	if len(b) != 8 {
		return "", errDatetimeSize
	}
	var buf [24]byte
	out := AppendMSSQLDatetime(buf[:0], b)
	return unsafe.String(&out[0], len(out)), nil
}

// FormatMSSQLDatetime2 возвращает строку ISO-8601 для DATETIME2(scale).
func FormatMSSQLDatetime2(b []byte, scale int) (string, error) {
	if scale < 0 || scale > 7 {
		return "", errInvalidScale
	}

	timeLen := 5
	if scale <= 2 {
		timeLen = 3
	} else if scale <= 4 {
		timeLen = 4
	}

	if len(b) != timeLen+3 {
		return "", errDatetime2Size
	}

	var buf [28]byte
	out := AppendMSSQLDatetime2(buf[:0], b, scale)
	return unsafe.String(&out[0], len(out)), nil
}

// -----------------------------------------------------------------------------
// time.Time → RFC3339Nano без stdlib-форматтера
// -----------------------------------------------------------------------------
//
// Это тот вход, который на самом деле есть у фреймворка. Драйверы БД
// (go-mssqldb, pgx, modernc/sqlite) отдают через database/sql уже готовый
// time.Time — сырых байтов провода в ScanSQLRows не бывает, поэтому
// AppendMSSQLDatetime туда не подключить без форка драйвера.
//
// Вывод байт-в-байт совпадает с schema.FormatTimestamp
// (t.UTC().Format(time.RFC3339Nano)), включая срез хвостовых нулей дробной
// части — иначе поменялись бы и строки в пакетах, и их контрольные суммы.

// AppendTimestampRFC3339Nano дописывает t в dst в том же виде, что и
// t.UTC().Format(time.RFC3339Nano). Для года вне 0001..9999 отдаёт работу
// stdlib.
func AppendTimestampRFC3339Nano(dst []byte, t time.Time) []byte {
	t = t.UTC()

	sec := t.Unix()
	days := sec / 86400
	rem := sec - days*86400
	if rem < 0 {
		rem += 86400
		days--
	}

	if days < math.MinInt32 || days > math.MaxInt32 {
		return t.AppendFormat(dst, time.RFC3339Nano)
	}

	year, month, day := civilFromDaysUnix(int32(days))
	if year < 0 || year > 9999 {
		return t.AppendFormat(dst, time.RFC3339Nano)
	}

	hour := int(rem / 3600)
	min := int((rem / 60) % 60)
	ss := int(rem % 60)
	nsec := t.Nanosecond()

	n := len(dst)
	// 20 байт на "2006-01-02T15:04:05Z" + до 10 на ".123456789".
	if cap(dst)-n < 30 {
		dst = append(dst, make([]byte, 30)...)[:n]
	}
	dst = dst[:n+20]
	out := dst[n:]

	put4(out[0:4], year)
	out[4] = '-'
	put2(out[5:7], month)
	out[7] = '-'
	put2(out[8:10], day)
	out[10] = 'T'
	put2(out[11:13], hour)
	out[13] = ':'
	put2(out[14:16], min)
	out[16] = ':'
	put2(out[17:19], ss)

	if nsec == 0 {
		out[19] = 'Z'
		return dst
	}

	// Дробная часть: 9 цифр, хвостовые нули срезаются — как в RFC3339Nano.
	var frac [9]byte
	v := nsec
	for i := 8; i >= 0; i-- {
		frac[i] = byte('0' + v%10)
		v /= 10
	}
	w := 9
	for w > 0 && frac[w-1] == '0' {
		w--
	}

	dst = dst[:n+20+w+1]
	out = dst[n:]
	out[19] = '.'
	copy(out[20:20+w], frac[:w])
	out[20+w] = 'Z'

	return dst
}

// FormatTimestampFast — замена schema.FormatTimestamp с тем же результатом.
func FormatTimestampFast(t time.Time) string {
	var buf [30]byte
	out := AppendTimestampRFC3339Nano(buf[:0], t)
	return string(out)
}
