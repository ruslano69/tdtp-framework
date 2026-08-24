// Package transform объявляет преобразования пакета и правила их сочетания.
//
// Здесь нет ни одной реализации — только имена шагов, порядок между ними и
// запреты. Реализации живут в packet, processors и командах CLI; этот пакет
// отвечает на два вопроса: можно ли включить такой набор и в каком порядке его
// выполнять.
//
// # Зачем отдельный пакет
//
// Порядок был закодирован очередью вызовов chain.Add и комментариями вида
// "MUST run before ComputeIntegrity". Это работает ровно до тех пор, пока
// каждый, кто добавляет шаг, прочитал все комментарии. За один месяц так
// набралось три бага, и все — про порядок, а не про сами преобразования:
//
//   - разложить по колонкам после ComputeIntegrity, но присвоив Data целиком,
//     значило выбросить уже проставленный xxh3;
//   - разложить по колонкам ДО хеширования значило бы, что писатель хеширует
//     колонки, а читатель строки, — расхождение на каждом пакете;
//   - materializeForWrite гасила намерение разложить, потому что к моменту её
//     вызова строки уже построчные, — и --columnar --integrity молча писал
//     обычный пакет.
//
// Ни один не виден на одиночном флаге, и ни один не виден на паре. Они живут
// в стыке между шагами.
//
// # Почему объявление, а не перечень сочетаний
//
// Перебор N флагов — это 2^N наборов, попарный — N². Написанная руками матрица
// отстанет от кода на первом же новом преобразовании, а дописывать в неё
// строку под каждый новый флаг — это и есть костыль, только оформленный
// таблицей. Ограничения объявляются у шага один раз; наборы для проверки
// порождаются из них (см. stages_test.go).
//
// # Правило для нового преобразования
//
// Добавляя формат или преобразование, объявите его здесь и ничего не
// добавляйте в вызывающий код руками:
//
//  1. Заведите Stage с именем и ограничениями: After/Before — если порядок
//     важен, Conflicts — если сочетание бессмысленно или опасно.
//  2. Reason пишите как ответ пользователю, а не как записку себе: он попадёт
//     в текст отказа.
//  3. Не ставьте ограничение "на всякий случай". Каждое из перечисленных
//     существует потому, что его нарушение уже ломало данные, и в комментарии
//     сказано как именно. Ограничение без такой причины запретит рабочее
//     сочетание.
//  4. Шаг обязан быть идемпотентным либо применяться ровно из одного места.
//     Раскладка по колонкам применялась из двух — писателем и шагом сжатия, —
//     и второй проход читал колонки как строки, отдавая значения чужих
//     записей. Молча: такой пакет конвертируется без замечаний.
package transform

import (
	"fmt"
	"sort"
	"strings"
)

// Имена шагов. Строки, а не iota: они попадают в сообщения об ошибках и в
// имена подтестов, и должны читаться человеком.
const (
	// StageRowProcessors — цепочка pre-export: маскирование, нормализация,
	// валидация. Идёт первой: всё дальнейшее обязано считать уже изменённые
	// значения, иначе хеш накроет то, чего в пакете не будет.
	StageRowProcessors = "row-processors"

	// StageCompact — carry-forward кодирование фиксированных полей (v1.3.1).
	StageCompact = "compact"

	// StageColumnar — колоночная раскладка Data (layout="columns").
	StageColumnar = "columnar"

	// StageIntegrity — xxh3-хеши Schema, Data и отпечаток пакета (v1.4).
	StageIntegrity = "integrity"

	// StageCompress — zstd или kanzi.
	StageCompress = "compress"

	// StageEncrypt — AES-256-GCM посекционно (v1.5).
	StageEncrypt = "encrypt"
)

// Stage — одно преобразование и правила его сочетания с другими.
type Stage struct {
	Name string

	// After перечисляет шаги, которые обязаны отработать РАНЬШЕ этого.
	After []string

	// Conflicts перечисляет шаги, с которыми этот несовместим. Отношение
	// симметрично: объявлять его достаточно с одной стороны.
	Conflicts []string

	// Reason объясняет ограничение словами, пригодными для показа
	// пользователю. Пустой Reason допустим только у шага без ограничений.
	Reason string
}

// registry — единственный источник правды о порядке и совместимости.
//
// Каждое After ниже подпёрто конкретной поломкой, а не осторожностью.
var registry = []Stage{
	{
		Name: StageRowProcessors,
		// Ограничений нет: идёт первым по построению.
	},
	{
		Name:  StageCompact,
		After: []string{StageRowProcessors},
		Reason: "compact кодирует те значения, которые останутся в пакете, " +
			"поэтому маскирование и нормализация обязаны отработать раньше",
	},
	{
		Name:  StageColumnar,
		After: []string{StageRowProcessors, StageCompact, StageIntegrity},
		Reason: "хеши v1.4 накрывают ПОСТРОЧНЫЕ значения до сжатия, а читатель " +
			"разворачивает колонки в строки перед проверкой; разложить раньше " +
			"хеширования — значит хешировать колонки против строк и получить " +
			"расхождение на каждом пакете",
	},
	{
		Name:  StageIntegrity,
		After: []string{StageRowProcessors, StageCompact},
		Reason: "хеш обязан накрывать окончательные значения строк: и " +
			"маскирование, и compact их меняют",
	},
	{
		Name:  StageCompress,
		After: []string{StageRowProcessors, StageCompact, StageIntegrity, StageColumnar},
		Reason: "после сжатия Data — один непрозрачный блоб; всё, что читает " +
			"или переставляет строки, обязано успеть до",
	},
	{
		Name:  StageEncrypt,
		After: []string{StageCompress, StageIntegrity},
		Reason: "шифруется то, что уже сжато и отхешировано; хеш считается по " +
			"открытому тексту, иначе получателю нечего с ним сверять",
	},
}

// byName даёт шаг по имени.
func byName(name string) (Stage, bool) {
	for _, s := range registry {
		if s.Name == name {
			return s, true
		}
	}
	return Stage{}, false
}

// All возвращает имена всех объявленных шагов в порядке объявления.
func All() []string {
	out := make([]string, len(registry))
	for i, s := range registry {
		out[i] = s.Name
	}
	return out
}

// Plan упорядочивает включённые шаги и отвергает несовместимый набор.
//
// Порядок детерминирован: при равных ограничениях шаги идут в порядке
// объявления в registry. Это не косметика — недетерминированный план означал
// бы, что один и тот же набор флагов даёт разные байты от запуска к запуску.
func Plan(enabled []string) ([]string, error) {
	want := make(map[string]bool, len(enabled))
	for _, name := range enabled {
		if _, ok := byName(name); !ok {
			return nil, fmt.Errorf("unknown transform %q (known: %s)",
				name, strings.Join(All(), ", "))
		}
		want[name] = true
	}

	if err := checkConflicts(want); err != nil {
		return nil, err
	}

	// Топологическая сортировка по порядку объявления. Граф крошечный
	// (единицы шагов), поэтому наивный проход дешевле и читается лучше.
	var out []string
	done := make(map[string]bool, len(want))
	for len(out) < len(want) {
		progress := false
		for _, s := range registry {
			if !want[s.Name] || done[s.Name] {
				continue
			}
			ready := true
			for _, dep := range s.After {
				if want[dep] && !done[dep] {
					ready = false
					break
				}
			}
			if ready {
				out = append(out, s.Name)
				done[s.Name] = true
				progress = true
			}
		}
		if !progress {
			var stuck []string
			for _, s := range registry {
				if want[s.Name] && !done[s.Name] {
					stuck = append(stuck, s.Name)
				}
			}
			sort.Strings(stuck)
			return nil, fmt.Errorf("circular ordering among transforms: %s",
				strings.Join(stuck, ", "))
		}
	}
	return out, nil
}

// checkConflicts отвергает набор с объявленной несовместимостью.
func checkConflicts(want map[string]bool) error {
	for _, s := range registry {
		if !want[s.Name] {
			continue
		}
		for _, other := range s.Conflicts {
			if want[other] {
				return fmt.Errorf("%s cannot be combined with %s: %s",
					s.Name, other, s.Reason)
			}
		}
	}
	return nil
}

// MustRunBefore сообщает, обязан ли a идти раньше b при обоих включённых.
// Нужен проверкам и диагностике; на горячем пути не используется.
func MustRunBefore(a, b string) bool {
	sb, ok := byName(b)
	if !ok {
		return false
	}
	for _, dep := range sb.After {
		if dep == a {
			return true
		}
	}
	return false
}
