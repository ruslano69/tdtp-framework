package base

import (
	"math"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Большое DECIMAL уезжало в пакет экспонентой, и это не косметика, а поломка
// на стыке трёх мест:
//
//  1. значение печаталось через 'g', который на больших числах даёт
//     "9.99999999999e+09";
//  2. parseDecimal проверяет scale, разрезая строку по точке, и принимает
//     мантиссу за дробную часть — пятнадцать знаков против объявленных двух;
//  3. ConvertValueToTDTP на ошибке разбора возвращает значение КАК ЕСТЬ.
//
// Поэтому проверка идёт по всем dbType сразу: путь один и тот же во всех
// адаптерах, и чинился он тоже во всех. Access попадает сюда единственным
// доступным способом — живого сервера для него на большинстве машин нет.
func TestDBValueToString_NoScientificNotation(t *testing.T) {
	c := NewUniversalTypeConverter()

	dbTypes := []string{"postgres", "mssql", "sqlite", "mysql", "access"}

	fields := []packet.Field{
		{Name: "bal", Type: "DECIMAL", Precision: 12, Scale: 2},
		{Name: "big", Type: "DECIMAL", Precision: 20, Scale: 4},
		{Name: "plain", Type: "DECIMAL"},
		{Name: "r", Type: "REAL"},
	}

	values := []struct {
		name string
		v    any
	}{
		{"NUMERIC(12,2) limit", float64(9999999999.99)},
		{"negative limit", float64(-9999999999.99)},
		{"integer past 1e8", float64(486789500)},
		{"past exact integers", float64(1234567890123456.7891)},
		{"1e20", float64(1e20)},
		{"1e-7", float64(0.0000001)},
		{"float32 large", float32(4867895)},
		{"float32 max", float32(math.MaxFloat32)},
		{"simple", float64(1500.5)},
		{"zero", float64(0)},
	}

	for _, db := range dbTypes {
		for _, f := range fields {
			for _, tc := range values {
				t.Run(db+"/"+f.Name+"/"+tc.name, func(t *testing.T) {
					raw := c.DBValueToString(tc.v, f, db)
					if strings.ContainsAny(raw, "eE") {
						t.Errorf("scientific notation reached the packet: %q", raw)
					}
					// Раз экспоненты нет, проверка scale видит настоящую дробную
					// часть, разбор проходит, и round-trip возвращает ту же
					// строку. Это и есть условие, при котором его можно
					// пропускать.
					if got := c.ConvertValueToTDTP(f, raw); got != raw {
						t.Errorf("round trip is not a no-op: raw=%q, after=%q", raw, got)
					}
				})
			}
		}
	}
}

// Значения без экспоненты обязаны остаться байт в байт теми же, какими были до
// смены формата: правка меняет вывод только там, где он был испорчен.
func TestDBValueToString_UnchangedForOrdinaryValues(t *testing.T) {
	c := NewUniversalTypeConverter()
	field := packet.Field{Name: "v", Type: "REAL"}

	cases := []struct {
		v    any
		want string
	}{
		{float64(0), "0"},
		{float64(1), "1"},
		{float64(-1), "-1"},
		{float64(1500.5), "1500.5"},
		{float64(0.000000123), "0.000000123"},
		{float32(1500.5), "1500.5"},
		{float32(0), "0"},
		{float64(math.Inf(1)), "+Inf"},
		{float64(math.Inf(-1)), "-Inf"},
	}

	for _, db := range []string{"postgres", "mssql", "sqlite", "mysql", "access"} {
		for _, tc := range cases {
			if got := c.DBValueToString(tc.v, field, db); got != tc.want {
				t.Errorf("%s: %v → %q, want %q", db, tc.v, got, tc.want)
			}
		}
	}
}
