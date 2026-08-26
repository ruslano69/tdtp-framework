package main

import (
	"flag"
	"log"
	"sort"
	"strings"
)

// Флаг принят, но никем не прочитан.
//
// Флаги в Go глобальные: любой из них можно передать любой команде, и если
// команда его не читает, не происходит ничего — ни ошибки, ни предупреждения,
// ни следа в выводе. За месяц это дало четыре разных бага, и ни один не был
// виден по результату работы:
//
//   - `--columnar` на пути запросов: пакет писался построчно, флаг принят;
//   - `--packet-size` в файловом экспорте: части выходили умолчательные;
//   - `--limit` в `--to-compact`: писались все строки, в отчёте «10 row(s)»;
//   - `--fields` в `--to-html` и `--to-xlsx`: рендерились все колонки.
//
// Общее у них не тема, а форма: команда завершалась успехом, делая не то, что
// просили. Найдены они были поштучно и вручную — этот файл существует, чтобы
// пятый нашёлся сам.
//
// Механизм: flag.Visit перечисляет ровно те флаги, которые пользователь
// НАБРАЛ (в отличие от flag.VisitAll, который перечисляет объявленные). Из
// набранного вычитается то, что команда объявила читаемым, и остаток —
// предупреждение.
//
// Предупреждение, а не отказ. Лишний флаг чаще всего безвреден — привычка,
// копипаста из соседнего вызова, скрипт с общим набором аргументов, — и
// отказывать значило бы ломать работающие вызовы ради опечаток. Задача была
// убрать тишину, а не запретить сочетание.

// commandFlags объявляет, какие флаги читает каждая команда.
//
// Правило при добавлении флага: вписать его сюда хотя бы одной команде, иначе
// TestFlagScope_EveryFlagIsClaimed не пройдёт. Тест не проверяет, что команда
// флаг действительно читает — этого из Go не видно, — но проверяет, что о
// каждом флаге кто-то заявил и что все имена в таблице существуют. Забыть
// новый флаг молча нельзя.
//
// Собственный флаг команды входит в её список: `--export users --limit 3`
// набирает и export, и limit.
var commandFlags = map[string][]string{
	"diff":             {"case-sensitive", "diff", "ignore-fields", "key-fields"},
	"export":           {"columnar", "compact", "compact-tail", "compress", "compress-algo", "compress-level", "enc", "enc13", "export", "fallback-row-limit", "fast", "fields", "fixed-fields", "hash", "integrity", "l", "limit", "mask", "mercury-caller", "normalize", "offset", "order-by", "output", "packet-size", "readonly-fields", "stream", "validate", "w", "where"},
	"export-broker":    {"batch", "compress", "compress-algo", "compress-level", "enc", "enc13", "export-broker", "fields", "hash", "l", "limit", "mask", "mercury-caller", "normalize", "offset", "order-by", "packet-size", "validate", "w", "where"},
	"export-xlsx":      {"export-xlsx", "fields", "l", "limit", "mask", "normalize", "offset", "order-by", "output", "sheet", "translit", "validate", "w", "where"},
	"from-xlsx":        {"from-xlsx", "output", "sheet"},
	"import":           {"clear", "expect-var", "fields", "import", "strategy", "table", "translit"},
	"import-broker":    {"expect-var", "import-broker", "keep", "output", "raw", "strategy", "table"},
	"import-xlsx":      {"import-xlsx", "sheet", "strategy", "table"},
	"inspect":          {"inspect"},
	"inspect-table":    {"inspect-table"},
	"list":             {"list"},
	"list-views":       {"list-views"},
	"listen":           {"listen", "strategy", "table"},
	"map":              {"drain", "dry-run", "input", "listen", "map"},
	"merge":            {"compress", "key-fields", "merge", "merge-strategy", "output", "show-conflicts"},
	"pipeline":         {"enc", "enc13", "mask", "normalize", "pipeline", "unsafe-cert", "validate"},
	"process-request":  {"output", "process-request"},
	"steps":            {"steps"},
	"sync-incremental": {"batch-size", "checkpoint-file", "compress", "compress-algo", "compress-level", "enc", "enc13", "fields", "hash", "mask", "normalize", "output", "sync-incremental", "to-broker", "tracking-field", "validate"},
	"test":             {"test"},
	"to-compact":       {"compact-tail", "fields", "fixed-fields", "l", "limit", "offset", "order-by", "output", "to-compact", "w", "where"},
	"to-csv":           {"bom", "cp", "d", "delimiter", "fields", "l", "limit", "offset", "order-by", "output", "to-csv", "translit", "w", "where"},
	"to-html":          {"fields", "l", "limit", "offset", "open", "order-by", "output", "row", "to-html", "w", "where"},
	"to-tdtp":          {"fields", "l", "limit", "offset", "order-by", "output", "to-tdtp", "v1", "v13", "v14", "w", "where"},
	"to-xlsx":          {"fields", "l", "limit", "offset", "order-by", "output", "sheet", "to-xlsx", "translit", "w", "where"},
}

// globalFlags читаются вне цепочки диспетчеризации и уместны с любой командой.
var globalFlags = []string{
	"config", "quiet", "license", "unsafe", "mercury-url", "help", "h", "version",
}

// ownCommandFlags — флаги, которые сами являются командой: они выбирают ветку,
// а не настраивают её. Перечислены отдельно, чтобы TestFlagScope_EveryFlagIsClaimed
// не требовал для них записи в commandFlags.
var ownCommandFlags = []string{
	"create-config-pg", "create-config-mssql", "create-config-sqlite", "create-config-mysql",
}

// warnUnusedFlags сообщает о флагах, которые пользователь набрал, а команда не
// читает. Вызывается в начале каждой ветки routeCommand — до работы, а не
// после: узнать, что --limit ничего не сделал, нужно раньше, чем выгрузятся
// 24 млн строк, а не в конце.
func warnUnusedFlags(command string) {
	known, declared := commandFlags[command]
	if !declared {
		// Команда без записи в таблице. Молчим: лучше не сказать ничего, чем
		// обвинить пользователя в лишнем флаге из-за пробела в таблице.
		// Пробел ловит тест, а не пользователь.
		return
	}

	allowed := make(map[string]bool, len(known)+len(globalFlags)+len(ownCommandFlags))
	for _, n := range known {
		allowed[n] = true
	}
	for _, n := range globalFlags {
		allowed[n] = true
	}
	for _, n := range ownCommandFlags {
		allowed[n] = true
	}

	var unused []string
	flag.Visit(func(f *flag.Flag) {
		if !allowed[f.Name] {
			unused = append(unused, "--"+f.Name)
		}
	})
	if len(unused) == 0 {
		return
	}
	sort.Strings(unused)

	subject := "the flag was"
	if len(unused) > 1 {
		subject = "the flags were"
	}
	log.Printf("NOTICE: --%s does not read %s — %s accepted, nothing acted on it",
		command, strings.Join(unused, ", "), subject)
}
