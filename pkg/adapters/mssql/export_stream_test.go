package mssql

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func streamSchema() packet.Schema {
	return packet.Schema{Fields: []packet.Field{
		{Name: "ID", Type: "INTEGER", ReadOnly: true},    // identity
		{Name: "Name", Type: "TEXT"},                     //
		{Name: "Computed", Type: "TEXT", ReadOnly: true}, // computed column
		{Name: "City", Type: "TEXT"},                     //
	}}
}

// ReadAllRowsStream строит SELECT и схему пакета из отфильтрованной схемы,
// а получает её вызовом filterReadOnlyFields с nil вместо строк. Тест
// закрепляет, что так делать можно: фильтр индексный и на пустых данных
// возвращает только схему.
//
// Если это сломается, поток начнёт запрашивать read-only колонки и объявлять
// их в заголовке — то есть разойдётся с обычным экспортом, который их режет.
func TestFilterReadOnlyFields_WorksWithoutRows(t *testing.T) {
	got, rows := filterReadOnlyFields(streamSchema(), nil, false)

	if len(rows) != 0 {
		t.Errorf("на nil-строках вернулось %d строк", len(rows))
	}
	want := []string{"Name", "City"}
	if len(got.Fields) != len(want) {
		t.Fatalf("полей %d, ожидалось %d: %+v", len(got.Fields), len(want), got.Fields)
	}
	for i, n := range want {
		if got.Fields[i].Name != n {
			t.Errorf("поле %d = %q, ожидалось %q", i, got.Fields[i].Name, n)
		}
	}
}

// С --readonly-fields схема остаётся полной — поток обязан вести себя так же.
func TestFilterReadOnlyFields_KeepsAllWhenAsked(t *testing.T) {
	got, _ := filterReadOnlyFields(streamSchema(), nil, true)
	if len(got.Fields) != 4 {
		t.Errorf("полей %d, ожидалось 4 — read-only не должны отсекаться", len(got.Fields))
	}
}

// Таблица целиком из read-only колонок: поток обязан отказаться внятно, а не
// строить SELECT без единой колонки.
func TestFilterReadOnlyFields_AllReadOnlyLeavesNothing(t *testing.T) {
	all := packet.Schema{Fields: []packet.Field{
		{Name: "ID", Type: "INTEGER", ReadOnly: true},
		{Name: "Ver", Type: "TEXT", ReadOnly: true},
	}}
	got, _ := filterReadOnlyFields(all, nil, false)
	if len(got.Fields) != 0 {
		t.Errorf("осталось %d полей, ожидалось 0", len(got.Fields))
	}
}
