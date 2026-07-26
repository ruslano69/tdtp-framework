package packet

import (
	"encoding/xml"
	"strconv"
	"strings"
	"testing"
)

// bigDictionary строит словарь заданного объёма. alphabet задаёт содержимое
// полных значений — от него зависит вес словаря в байтах.
func bigDictionary(entries int, alphabet string) *Dictionary {
	d := &Dictionary{}
	for i := 0; i < entries; i++ {
		d.Entries = append(d.Entries, DictEntry{
			Short: "@" + strconv.Itoa(i),
			Full:  strings.Repeat(alphabet, 20) + strconv.Itoa(i),
		})
	}
	return d
}

func simpleSchema(nFields int) Schema {
	fields := make([]Field, nFields)
	for i := range fields {
		fields[i] = Field{Name: "col" + strconv.Itoa(i), Type: "TEXT"}
	}
	return Schema{Fields: fields}
}

// TestMeasureEnvelopeSize_FloorForSmallSchema — обычная схема без словаря должна
// давать прежнюю константу, чтобы границы частей не поехали.
func TestMeasureEnvelopeSize_FloorForSmallSchema(t *testing.T) {
	got := measureEnvelopeSize(simpleSchema(10))
	if got != packetOverheadSize {
		t.Errorf("small schema should keep the %d floor, got %d", packetOverheadSize, got)
	}
}

// TestMeasureEnvelopeSize_GrowsWithDictionary — со словарём конверт должен
// измеряться, а не упираться в константу.
func TestMeasureEnvelopeSize_GrowsWithDictionary(t *testing.T) {
	schema := simpleSchema(3)
	schema.Dictionary = bigDictionary(500, "value")

	got := measureEnvelopeSize(schema)
	if got <= packetOverheadSize {
		t.Fatalf("dictionary schema should exceed the floor, got %d", got)
	}

	// Ожидаем ровно удвоенный размер сериализованной схемы (единицы estimateRowSize).
	b, err := xml.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if want := len(b) * 2; got != want {
		t.Errorf("envelope = %d, want %d (2x serialized schema)", got, want)
	}
}

// TestMeasureEnvelopeSize_MultibyteCostsMore — тот же словарь на многобайтовом
// алфавите весит больше. Это тот перекос, ради которого замер и вводится.
func TestMeasureEnvelopeSize_MultibyteCostsMore(t *testing.T) {
	ascii := simpleSchema(3)
	ascii.Dictionary = bigDictionary(300, "abcde")

	cjk := simpleSchema(3)
	cjk.Dictionary = bigDictionary(300, "日本語表示")

	a, c := measureEnvelopeSize(ascii), measureEnvelopeSize(cjk)
	if c <= a {
		t.Errorf("CJK dictionary should measure larger: ascii=%d cjk=%d", a, c)
	}
}

// contractedRows строит короткие строки — такие, какими они становятся после
// контракции словарём — общим объёмом не меньше targetUnits.
func contractedRows(targetUnits int) [][]string {
	rows := [][]string{}
	total := 0
	for i := 0; total < targetUnits; i++ {
		row := []string{"@a" + strconv.Itoa(i%50), "@b" + strconv.Itoa(i%50), "@c"}
		total += estimateRowSize(row)
		rows = append(rows, row)
	}
	return rows
}

// TestPartitionRows_DictionaryShrinksParts — ключевой сценарий: строки сжаты в
// короткие @-идентификаторы, а полные значения уехали в словарь. При том же
// бюджете словарь обязан давать больше частей, потому что теперь он занимает
// в этом бюджете место.
//
// Бюджет задаётся от измеренного конверта (вчетверо больше), чтобы конверт
// гарантированно оставался ниже половины и правило «не дробить» не включалось.
func TestPartitionRows_DictionaryShrinksParts(t *testing.T) {
	plain := simpleSchema(3)

	withDict := simpleSchema(3)
	withDict.Dictionary = bigDictionary(400, "достаточно длинное значение")

	envelope := measureEnvelopeSize(withDict)
	budget := envelope * 4

	g := NewGenerator()
	g.SetMaxMessageSize(budget)

	rows := contractedRows(budget * 10)

	plainParts := len(g.partitionRows(rows, plain))
	dictParts := len(g.partitionRows(rows, withDict))

	if dictParts <= plainParts {
		t.Errorf("a large dictionary must produce more (smaller) parts: plain=%d withDict=%d",
			plainParts, dictParts)
	}
	t.Logf("rows=%d budget=%d envelope=%d → parts plain=%d withDict=%d",
		len(rows), budget, envelope, plainParts, dictParts)
}

// TestPartitionRows_UnchangedWithoutDictionary — без словаря разбиение обязано
// остаться прежним (конверт упирается в тот же packetOverheadSize).
func TestPartitionRows_UnchangedWithoutDictionary(t *testing.T) {
	rows := make([][]string, 2000)
	for i := range rows {
		rows[i] = []string{"value" + strconv.Itoa(i), "another", "third"}
	}

	g := NewGenerator()
	g.SetMaxMessageSize(100_000)
	schema := simpleSchema(3)

	// Воспроизводим прежнюю формулу с фиксированной константой.
	wantParts := 0
	currentSize := 0
	inPart := 0
	for _, row := range rows {
		rowSize := estimateRowSize(row)
		if currentSize+rowSize+packetOverheadSize > 100_000 && inPart > 0 {
			wantParts++
			currentSize, inPart = 0, 0
		}
		inPart++
		currentSize += rowSize
	}
	if inPart > 0 {
		wantParts++
	}

	if got := len(g.partitionRows(rows, schema)); got != wantParts {
		t.Errorf("partitioning changed for a dictionary-free schema: got %d parts, want %d", got, wantParts)
	}
}

// TestPartitionRows_EnvelopeOverBudgetKeepsOnePart — если конверт не влезает в
// бюджет, дробить нельзя: Schema копируется в каждую часть, поэтому каждая новая
// часть только добавляет ещё одну копию словаря. Ожидаем одну часть.
func TestPartitionRows_EnvelopeOverBudgetKeepsOnePart(t *testing.T) {
	rows := [][]string{{"a"}, {"b"}, {"c"}}

	schema := simpleSchema(1)
	schema.Dictionary = bigDictionary(200, "заведомо больше бюджета")

	g := NewGenerator()
	g.SetMaxMessageSize(1000) // меньше конверта

	parts := g.partitionRows(rows, schema)
	if len(parts) != 1 {
		t.Fatalf("expected a single part when the envelope exceeds the budget, got %d", len(parts))
	}
	if len(parts[0]) != len(rows) {
		t.Errorf("the single part must hold all %d rows, got %d", len(rows), len(parts[0]))
	}
}

// TestPartitionRows_LargeEnvelopeDoesNotExplodePartCount — главный регресс,
// ради которого введён потолок резерва. При честном вычитании крупного конверта
// места под строки почти не остаётся и число частей взрывается, а каждая часть
// тащит полную копию словаря. Проверяем, что этого не происходит.
func TestPartitionRows_LargeEnvelopeDoesNotExplodePartCount(t *testing.T) {
	rows := make([][]string, 4000)
	for i := range rows {
		rows[i] = []string{"@a" + strconv.Itoa(i%50), "@b" + strconv.Itoa(i%50), "@c"}
	}

	schema := simpleSchema(3)
	schema.Dictionary = bigDictionary(400, "достаточно длинное значение")

	g := NewGenerator()
	g.SetMaxMessageSize(200_000)

	parts := g.partitionRows(rows, schema)

	// Наивное «бюджет минус конверт» дало бы по строке на часть (4000 штук).
	if len(parts) >= len(rows) {
		t.Fatalf("part count exploded: %d parts for %d rows", len(parts), len(rows))
	}

	// Резерв ограничен половиной бюджета, значит частей не больше чем
	// ceil(totalRowBytes / (budget/2)) плюс единица на округление.
	total := 0
	for _, r := range rows {
		total += estimateRowSize(r)
	}
	maxExpected := total/(200_000/2) + 2
	if len(parts) > maxExpected {
		t.Errorf("got %d parts, expected at most %d", len(parts), maxExpected)
	}
	t.Logf("parts=%d envelope=%d rowBudget=%d", len(parts), measureEnvelopeSize(schema), g.rowBudget(schema))
}

// TestPartitionRows_TinySchemaSmallBudgetStillSplits — консервативный пол
// packetOverheadSize не должен запрещать дробление: решение принимается по
// фактическому размеру схемы. Крошечная схема при маленьком бюджете обязана
// дробиться, как и до введения замера.
func TestPartitionRows_TinySchemaSmallBudgetStillSplits(t *testing.T) {
	schema := simpleSchema(2)

	// Схема заведомо меньше бюджета, а пол — заведомо больше.
	if serializedSchemaSize(schema) >= 1000 {
		t.Fatalf("test premise broken: schema is not tiny (%d)", serializedSchemaSize(schema))
	}
	if measureEnvelopeSize(schema) <= 1000 {
		t.Fatalf("test premise broken: floor is not above the budget (%d)", measureEnvelopeSize(schema))
	}

	rows := make([][]string, 100)
	for i := range rows {
		rows[i] = []string{strconv.Itoa(i), strings.Repeat("x", 50)}
	}

	g := NewGenerator()
	g.SetMaxMessageSize(1000)

	if parts := len(g.partitionRows(rows, schema)); parts <= 1 {
		t.Errorf("a tiny schema with a small budget must still split, got %d part(s)", parts)
	}
}

// ─── Предел размера схемы ───────────────────────────────────────────────────

// schemaBytes — размер сериализованной схемы в байтах (не в единицах бюджета).
func schemaBytes(t *testing.T, s Schema) int {
	t.Helper()
	b, err := xml.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return len(b)
}

// TestValidateSchemaSize_AllowsNormalSchemas — обычные схемы, включая очень
// широкие таблицы, обязаны проходить: предел рассчитан на словарь, а не на поля.
func TestValidateSchemaSize_AllowsNormalSchemas(t *testing.T) {
	for _, n := range []int{1, 100, 1600} {
		s := simpleSchema(n)
		if err := validateSchemaSize(s); err != nil {
			t.Errorf("%d fields (%d bytes) must be allowed: %v", n, schemaBytes(t, s), err)
		}
	}
}

// TestValidateSchemaSize_RejectsOversizedDictionary — превысить предел можно
// практически только словарём.
func TestValidateSchemaSize_RejectsOversizedDictionary(t *testing.T) {
	s := simpleSchema(3)
	s.Dictionary = bigDictionary(3000, "достаточно длинное значение")

	if got := schemaBytes(t, s); got <= MaxSchemaBytes {
		t.Fatalf("test premise broken: schema is only %d bytes", got)
	}

	err := validateSchemaSize(s)
	if err == nil {
		t.Fatal("oversized schema must be rejected")
	}
	if !strings.Contains(err.Error(), "Dictionary entries") {
		t.Errorf("error should point at the dictionary, got: %v", err)
	}
}

// TestValidateSchemaSize_BoundaryIsInclusive — ровно на пределе схема ещё валидна.
func TestValidateSchemaSize_BoundaryIsInclusive(t *testing.T) {
	// Подбираем словарь чуть ниже предела.
	s := simpleSchema(1)
	for n := 100; ; n += 100 {
		s.Dictionary = bigDictionary(n, "value")
		if schemaBytes(t, s) > MaxSchemaBytes {
			s.Dictionary = bigDictionary(n-100, "value")
			break
		}
	}

	if got := schemaBytes(t, s); got > MaxSchemaBytes {
		t.Fatalf("premise broken: %d > %d", got, MaxSchemaBytes)
	}
	if err := validateSchemaSize(s); err != nil {
		t.Errorf("schema just under the limit must pass: %v", err)
	}
}

// TestGenerateReference_RejectsOversizedSchema — предел действует на входе
// генерации, а не где-то в глубине.
func TestGenerateReference_RejectsOversizedSchema(t *testing.T) {
	s := simpleSchema(3)
	s.Dictionary = bigDictionary(3000, "достаточно длинное значение")

	rows := [][]string{{"a", "b", "c"}}

	if _, err := NewGenerator().GenerateReference("t", s, rows); err == nil {
		t.Error("GenerateReference must reject an oversized schema")
	}
}

// TestGenerateResponse_RejectsOversizedSchema — то же для response.
func TestGenerateResponse_RejectsOversizedSchema(t *testing.T) {
	s := simpleSchema(3)
	s.Dictionary = bigDictionary(3000, "достаточно длинное значение")

	rows := [][]string{{"a", "b", "c"}}

	_, err := NewGenerator().GenerateResponse("t", "REQ-1", s, rows, nil, "a", "b")
	if err == nil {
		t.Error("GenerateResponse must reject an oversized schema")
	}
}

// TestGenerateReference_AcceptsMercuryDictionary — служебная запись @MRC, ради
// которой словарь реально используется сегодня, обязана проходить свободно.
func TestGenerateReference_AcceptsMercuryDictionary(t *testing.T) {
	s := simpleSchema(5)
	s.Dictionary = &Dictionary{Entries: []DictEntry{
		{Short: "@MRC", Full: "https://mercury.example.com/api"},
	}}

	if _, err := NewGenerator().GenerateReference("t", s, [][]string{{"1", "2", "3", "4", "5"}}); err != nil {
		t.Errorf("the @MRC dictionary must be accepted: %v", err)
	}
}
