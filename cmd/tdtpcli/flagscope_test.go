package main

import (
	"flag"
	"os"
	"sort"
	"strings"
	"testing"
)

// registeredFlags — имена всех объявленных флагов, взятые у самого flag-пакета.
//
// Через VisitAll, а не грепом по flags.go: грепом легко промахнуться мимо
// flag.StringVar и flag.Var, а промах здесь означает флаг, который тест
// считает несуществующим и потому не проверяет.
func registeredFlags(t *testing.T) map[string]bool {
	t.Helper()
	fs := flag.NewFlagSet("tdtpcli", flag.ContinueOnError)
	fs.SetOutput(&strings.Builder{})

	oldFS, oldArgs := flag.CommandLine, os.Args
	flag.CommandLine = fs
	os.Args = []string{"tdtpcli"}
	defer func() { flag.CommandLine, os.Args = oldFS, oldArgs }()

	// ParseFlags регистрирует и сразу парсит; аргументов нет, поэтому парсинг
	// пустой, а нужен только побочный эффект — объявленные флаги.
	_ = ParseFlags()

	names := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) { names[f.Name] = true })
	if len(names) == 0 {
		t.Fatal("no flags registered — ParseFlags no longer defines flags on flag.CommandLine")
	}
	return names
}

// Каждый объявленный флаг должен быть кем-то заявлен.
//
// Это единственная проверка, которая ловит забытый флаг: команда, не
// прочитавшая флаг, выглядит из Go ровно так же, как команда, которой он не
// нужен, — и отличить их может только человек, вписавший строку в таблицу.
// Тест требует, чтобы строка была.
func TestFlagScope_EveryFlagIsClaimed(t *testing.T) {
	registered := registeredFlags(t)

	claimed := map[string]bool{}
	for _, names := range commandFlags {
		for _, n := range names {
			claimed[n] = true
		}
	}
	for _, n := range globalFlags {
		claimed[n] = true
	}
	for _, n := range ownCommandFlags {
		claimed[n] = true
	}

	var orphans []string
	for name := range registered {
		if !claimed[name] {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)

	if len(orphans) > 0 {
		t.Errorf("flags declared but claimed by no command: %s\n"+
			"Add each to the command that reads it in commandFlags, or to globalFlags "+
			"if it is read outside the dispatch chain. A flag no one claims is a flag "+
			"the user can pass to anything and have silently ignored.",
			strings.Join(orphans, ", "))
	}
}

// Обратная сторона: в таблице не должно быть имён, которых нет среди флагов.
//
// Ловит опечатку и переименование: без этой проверки строка "--limitt" в
// таблице просто не совпала бы ни с чем, warnUnusedFlags продолжил бы ругаться
// на настоящий --limit, и виноватым выглядел бы пользователь.
func TestFlagScope_NoPhantomFlags(t *testing.T) {
	registered := registeredFlags(t)

	seen := map[string][]string{}
	for cmd, names := range commandFlags {
		for _, n := range names {
			if !registered[n] {
				seen[n] = append(seen[n], cmd)
			}
		}
	}
	for _, n := range globalFlags {
		if !registered[n] {
			seen[n] = append(seen[n], "globalFlags")
		}
	}
	for _, n := range ownCommandFlags {
		if !registered[n] {
			seen[n] = append(seen[n], "ownCommandFlags")
		}
	}

	for name, where := range seen {
		sort.Strings(where)
		t.Errorf("commandFlags names %q, which is not a registered flag (declared in: %s)",
			name, strings.Join(where, ", "))
	}
}

// Команда обязана заявлять флаг, которым её вызывают.
//
// `--export` без "export" в собственном списке означало бы, что запуск
// команды ругается на саму команду.
func TestFlagScope_CommandClaimsItsOwnFlag(t *testing.T) {
	for cmd, names := range commandFlags {
		found := false
		for _, n := range names {
			if n == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("command %q does not list its own flag %q; running it would warn about itself",
				cmd, cmd)
		}
	}
}

// Каждая команда из routeCommand должна быть в таблице.
//
// warnUnusedFlags для неизвестной команды молчит — это правильный выбор для
// работающей программы (лучше не сказать ничего, чем обвинить пользователя из-за
// пробела в таблице), но означает, что пробел иначе никак не заметен.
func TestFlagScope_EveryDispatchedCommandIsDeclared(t *testing.T) {
	// Список ведётся вручную и намеренно: он сверяется с ветками routeCommand,
	// а не выводится из них, поэтому новая команда без записи в commandFlags
	// упирается в этот тест, а не проходит незамеченной.
	dispatched := []string{
		"steps", "map", "list", "list-views", "to-compact", "to-tdtp", "export",
		"import", "to-html", "to-csv", "to-xlsx", "from-xlsx", "export-xlsx",
		"import-xlsx", "export-broker", "import-broker", "sync-incremental",
		"pipeline", "process-request", "diff", "merge", "test", "inspect",
		"inspect-table", "listen",
	}
	for _, cmd := range dispatched {
		if _, ok := commandFlags[cmd]; !ok {
			t.Errorf("command %q is dispatched by routeCommand but absent from commandFlags", cmd)
		}
	}
	for cmd := range commandFlags {
		found := false
		for _, d := range dispatched {
			if d == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("commandFlags declares %q, which routeCommand does not dispatch", cmd)
		}
	}
}
