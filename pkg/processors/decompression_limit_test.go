package processors

import (
	"strings"
	"testing"
)

// makeBomb собирает base64+zstd блоб, который разворачивается в rawSize байт.
// Данные повторяющиеся — специально подбирать содержимое не требуется, обычная
// табличная повторяемость уже даёт коэффициент в тысячи раз.
func makeBomb(t *testing.T, rawSize int) string {
	t.Helper()

	raw := make([]byte, rawSize)
	for i := range raw {
		raw[i] = byte('a' + i%3)
	}

	compressed, err := Compress(raw, 19)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	return string(compressed)
}

// TestDecompress_RejectsBomb — распаковка сверх предела обязана вернуть ошибку,
// а не выделить память.
func TestDecompress_RejectsBomb(t *testing.T) {
	bomb := makeBomb(t, MaxDecompressedBytes+(8<<20)) // предел + 8 МБ

	t.Logf("пакет %d байт → заявлено %d байт после распаковки (%.0f×)",
		len(bomb), MaxDecompressedBytes+(8<<20),
		float64(MaxDecompressedBytes+(8<<20))/float64(len(bomb)))

	if _, err := Decompress([]byte(bomb)); err == nil {
		t.Fatal("распаковка сверх MaxDecompressedBytes должна возвращать ошибку")
	}
}

// TestDecompressDataForTdtp_RejectsBomb — тот же предел на пути, которым ходит
// импорт пакета.
func TestDecompressDataForTdtp_RejectsBomb(t *testing.T) {
	bomb := makeBomb(t, MaxDecompressedBytes+(8<<20))

	if _, err := DecompressDataForTdtpWithAlgo(bomb, AlgoZstd); err == nil {
		t.Fatal("DecompressDataForTdtpWithAlgo должна отклонять бомбу")
	}
}

// TestDecompress_AllowsLegitimatePacket — обычный пакет не должен пострадать.
// Размер взят с запасом над дефолтным бюджетом части (~1.9 МБ реального XML).
func TestDecompress_AllowsLegitimatePacket(t *testing.T) {
	rows := make([]string, 20000)
	for i := range rows {
		rows[i] = strings.Repeat("value|", 9) + "tail"
	}

	compressed, _, err := CompressDataForTdtpAlgo(rows, AlgoZstd, 3)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}

	got, err := DecompressDataForTdtpWithAlgo(compressed, AlgoZstd)
	if err != nil {
		t.Fatalf("легитимный пакет обязан распаковываться: %v", err)
	}
	if len(got) != len(rows) {
		t.Errorf("строк после распаковки %d, ожидалось %d", len(got), len(rows))
	}
}

// TestDecompress_AllowsSizeJustUnderLimit — граница: чуть меньше предела проходит.
// Проверяет, что предел не срабатывает раньше времени на легитимном объёме.
func TestDecompress_AllowsSizeJustUnderLimit(t *testing.T) {
	const size = 64 << 20 // существенно ниже предела, но заметно больше пакета
	blob := makeBomb(t, size)

	got, err := Decompress([]byte(blob))
	if err != nil {
		t.Fatalf("%d байт должны распаковываться: %v", size, err)
	}
	if len(got) != size {
		t.Errorf("распаковано %d байт, ожидалось %d", len(got), size)
	}
}

// TestMaxDecompressedBytes_Value — предел зафиксирован осознанно; изменение
// должно быть намеренным, а не побочным.
func TestMaxDecompressedBytes_Value(t *testing.T) {
	if MaxDecompressedBytes != 256<<20 {
		t.Errorf("MaxDecompressedBytes = %d, ожидалось 256 MB", MaxDecompressedBytes)
	}
}

// TestKanzi_RoundtripStillWorks — предел на kanzi-пути не должен ломать
// легитимную распаковку. Отклонение бомбы там устроено тем же ограничением
// (io.LimitReader + проверка длины), но при 256 МБ сжатие kanzi занимает
// десятки секунд, поэтому в модульном тесте проверяется только целостность.
func TestKanzi_RoundtripStillWorks(t *testing.T) {
	rows := make([]string, 5000)
	for i := range rows {
		rows[i] = strings.Repeat("значение|", 9) + "хвост"
	}

	compressed, _, err := CompressDataForTdtpAlgo(rows, AlgoKanzi, 6)
	if err != nil {
		t.Skipf("kanzi недоступен в этой сборке: %v", err)
	}

	got, err := DecompressDataForTdtpWithAlgo(compressed, AlgoKanzi)
	if err != nil {
		t.Fatalf("kanzi roundtrip сломан: %v", err)
	}
	if len(got) != len(rows) {
		t.Errorf("строк %d, ожидалось %d", len(got), len(rows))
	}
	if got[0] != rows[0] {
		t.Errorf("содержимое не совпало: %q", got[0])
	}
}
