package packet

import (
	"math/rand"
	"strings"
	"testing"
)

// referenceGetRowValues — прямолинейный побайтовый разбор, каким GetRowValues
// был до перехода на прогоны и IndexByte. Держится здесь как эталон: быстрый
// разбор обязан совпадать с ним на любом входе, иначе оптимизация меняет
// содержимое пакетов.
func referenceGetRowValues(s string) []string {
	values := make([]string, 0, 10)
	var buf strings.Builder
	escaped := false

	for i := 0; i < len(s); i++ {
		char := s[i]
		switch {
		case escaped:
			if char == 'n' {
				buf.WriteByte('\n')
			} else {
				buf.WriteByte(char)
			}
			escaped = false
		case char == '\\':
			escaped = true
		case char == '|':
			values = append(values, buf.String())
			buf.Reset()
		default:
			buf.WriteByte(char)
		}
	}

	if escaped {
		buf.WriteByte('\\')
	}

	return append(values, buf.String())
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestGetRowValues_MatchesReference(t *testing.T) {
	p := NewParser()

	for _, s := range []string{
		"",
		"a",
		"a|b|c",
		"|",
		"||",
		"a||b",
		"|a",
		"a|",
		`a\|b`,
		`a\|b|c`,
		`a\\b`,
		`a\nb`,
		`a\nb|c`,
		`\n`,
		`\|`,
		`\\`,
		`a\`,
		`\`,
		`a\||b`,
		`\n||x`,
		`a\\|b`,
		`|\|`,
		`\|\|\|`,
		`a|b\|c|d`,
		`\na\nb\n|\n`,
		"поле|значение|ещё",
		`поле\|со\|слэшем|обычное`,
	} {
		want := referenceGetRowValues(s)
		if got := p.GetRowValues(Row{Value: s}); !equalStringSlices(got, want) {
			t.Errorf("%q:\n  got       %q\n  reference %q", s, got, want)
		}
	}
}

// Случайные строки из «опасного» алфавита: разделители, слэши и готовые
// escape-последовательности вперемешку.
func TestGetRowValues_MatchesReference_Random(t *testing.T) {
	p := NewParser()
	rng := rand.New(rand.NewSource(20260821))
	alphabet := []string{"a", "b", "я", "|", `\`, `\|`, `\\`, `\n`, "", " ", "0"}

	for i := 0; i < 300000; i++ {
		var sb strings.Builder
		for j := rng.Intn(12); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		s := sb.String()

		want := referenceGetRowValues(s)
		if got := p.GetRowValues(Row{Value: s}); !equalStringSlices(got, want) {
			t.Fatalf("iter %d, %q:\n  got       %q\n  reference %q", i, s, got, want)
		}
	}
}

// GetRowValuesInto обязан давать тот же результат, что и GetRowValues, и с
// переданным буфером, и с nil.
func TestGetRowValuesInto_MatchesGetRowValues(t *testing.T) {
	p := NewParser()
	rng := rand.New(rand.NewSource(7))
	alphabet := []string{"a", "|", `\`, `\|`, `\\`, `\n`, ""}

	buf := make([]string, 0, 4)
	for i := 0; i < 100000; i++ {
		var sb strings.Builder
		for j := rng.Intn(10); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		s := sb.String()
		want := p.GetRowValues(Row{Value: s})

		buf = p.GetRowValuesInto(Row{Value: s}, buf)
		if !equalStringSlices(buf, want) {
			t.Fatalf("reused buffer, %q:\n  got  %q\n  want %q", s, buf, want)
		}

		if got := p.GetRowValuesInto(Row{Value: s}, nil); !equalStringSlices(got, want) {
			t.Fatalf("nil buffer, %q:\n  got  %q\n  want %q", s, got, want)
		}
	}
}

// Буфер, переданный с непустой длиной, должен быть перезаписан с нуля, а не
// дополнен.
func TestGetRowValuesInto_TruncatesDst(t *testing.T) {
	p := NewParser()
	dst := []string{"stale", "stale", "stale"}
	got := p.GetRowValuesInto(Row{Value: "a|b"}, dst)
	if !equalStringSlices(got, []string{"a", "b"}) {
		t.Errorf("got %q, want [a b]", got)
	}
}
