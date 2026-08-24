package transform

import (
	"fmt"
	"strings"
	"testing"
)

// subsets порождает все 2^N подмножеств. Именно порождает, а не перечисляет:
// написанный руками список отстанет от registry на первом же новом шаге, и
// отставание будет молчаливым — тест продолжит проходить, просто перестанет
// проверять новое преобразование.
func subsets(names []string) [][]string {
	out := make([][]string, 0, 1<<len(names))
	for mask := 0; mask < 1<<len(names); mask++ {
		var set []string
		for i, n := range names {
			if mask&(1<<i) != 0 {
				set = append(set, n)
			}
		}
		out = append(out, set)
	}
	return out
}

// Каждое подмножество либо планируется, либо отвергается с внятной причиной.
// Молчаливого третьего исхода быть не должно.
func TestPlan_EverySubsetIsDecided(t *testing.T) {
	all := All()
	if len(all) > 12 {
		t.Fatalf("registry вырос до %d шагов: 2^N подмножеств больше не бесплатны, "+
			"перебор пора заменить попарным плюс полный набор", len(all))
	}

	for _, set := range subsets(all) {
		name := "пусто"
		if len(set) > 0 {
			name = strings.Join(set, "+")
		}
		t.Run(name, func(t *testing.T) {
			plan, err := Plan(set)
			if err != nil {
				if !strings.Contains(err.Error(), "cannot be combined") {
					t.Fatalf("отказ без объявленного конфликта: %v", err)
				}
				return
			}
			if len(plan) != len(set) {
				t.Fatalf("план из %d шагов на набор из %d: %v", len(plan), len(set), plan)
			}

			pos := make(map[string]int, len(plan))
			for i, s := range plan {
				pos[s] = i
			}
			// Каждое объявленное After обязано выполняться в плане.
			for _, s := range registry {
				if _, ok := pos[s.Name]; !ok {
					continue
				}
				for _, dep := range s.After {
					if _, ok := pos[dep]; !ok {
						continue
					}
					if pos[dep] > pos[s.Name] {
						t.Errorf("%s стоит после %s, хотя объявлен как After: %s",
							dep, s.Name, s.Reason)
					}
				}
			}
		})
	}
}

// План обязан быть одинаковым от вызова к вызову: иначе один и тот же набор
// флагов даёт разные байты, и расследовать расхождение будет нечем.
func TestPlan_IsDeterministic(t *testing.T) {
	set := All()
	first, err := Plan(set)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		got, err := Plan(set)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("план разъехался:\n%v\n%v", first, got)
		}
	}
}

// Порядок входа не должен влиять на план — иначе он зависел бы от того, в
// каком порядке командная строка перечислила флаги.
func TestPlan_IgnoresInputOrder(t *testing.T) {
	forward := All()
	backward := make([]string, len(forward))
	for i, n := range forward {
		backward[len(forward)-1-i] = n
	}

	a, err := Plan(forward)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Plan(backward)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Errorf("план зависит от порядка флагов:\n прямой:  %v\n обратный: %v", a, b)
	}
}

// Ограничения, ради которых пакет и существует. Проверяются явно, потому что
// каждое подпёрто конкретной поломкой, и «случайно ослабить» их нельзя.
func TestPlan_PinsTheConstraintsThatBrokeData(t *testing.T) {
	cases := []struct{ before, after, why string }{
		{StageIntegrity, StageColumnar,
			"разложить по колонкам до хеширования — хешировать колонки против строк у читателя"},
		{StageIntegrity, StageCompress,
			"хеш v1.4 накрывает открытые строки до сжатия"},
		{StageColumnar, StageCompress,
			"после сжатия Data — блоб, переставлять в нём нечего"},
		{StageCompact, StageIntegrity,
			"compact меняет значения строк, хеш обязан накрыть окончательные"},
		{StageRowProcessors, StageIntegrity,
			"маскирование меняет значения строк, хеш обязан накрыть окончательные"},
		{StageCompress, StageEncrypt,
			"шифруется уже сжатое"},
	}
	for _, c := range cases {
		t.Run(c.before+"→"+c.after, func(t *testing.T) {
			if !MustRunBefore(c.before, c.after) {
				t.Fatalf("ограничение потеряно: %s обязан идти раньше %s — %s",
					c.before, c.after, c.why)
			}
			plan, err := Plan([]string{c.after, c.before}) // намеренно наоборот
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if plan[0] != c.before {
				t.Errorf("план %v нарушает порядок: %s", plan, c.why)
			}
		})
	}
}

// Реестр обязан быть связным: ссылки на несуществующие шаги превратятся в
// молча проигнорированное ограничение.
func TestRegistry_IsWellFormed(t *testing.T) {
	for _, s := range registry {
		for _, dep := range s.After {
			if _, ok := byName(dep); !ok {
				t.Errorf("%s.After ссылается на несуществующий шаг %q", s.Name, dep)
			}
			if dep == s.Name {
				t.Errorf("%s объявлен After самого себя", s.Name)
			}
		}
		for _, c := range s.Conflicts {
			if _, ok := byName(c); !ok {
				t.Errorf("%s.Conflicts ссылается на несуществующий шаг %q", s.Name, c)
			}
		}
		if (len(s.After) > 0 || len(s.Conflicts) > 0) && strings.TrimSpace(s.Reason) == "" {
			t.Errorf("%s объявляет ограничение без Reason — текст отказа будет пуст", s.Name)
		}
	}
}

func TestPlan_RejectsUnknownStage(t *testing.T) {
	_, err := Plan([]string{StageCompress, "teleport"})
	if err == nil {
		t.Fatal("неизвестное преобразование принято")
	}
	if !strings.Contains(err.Error(), "teleport") {
		t.Errorf("в отказе нет имени шага: %v", err)
	}
}

func ExamplePlan() {
	plan, _ := Plan([]string{StageCompress, StageColumnar, StageIntegrity, StageCompact})
	fmt.Println(strings.Join(plan, " → "))
	// Output: compact → integrity → columnar → compress
}
