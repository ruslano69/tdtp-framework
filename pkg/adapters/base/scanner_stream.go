package base

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// StreamingDataReader — необязательное расширение DataReader: отдаёт строки
// потоком, не собирая их в память.
//
// Необязательное намеренно. DataReader реализуют пять адаптеров плюс моки в
// тестах; добавление метода в сам интерфейс сломало бы всех разом ради того,
// что пока умеет один. Вызывающий проверяет приведением типа и откатывается на
// ReadAllRows, если потока нет.
//
// Канал строк закрывается по исчерпании данных. Ошибки приходят в отдельный
// канал: читать его нужно ПОСЛЕ того, как канал строк закрыт, иначе ошибку,
// случившуюся в середине чтения, легко принять за нормальный конец.
//
// Возвращает схему, которую РЕАЛЬНО будет отдавать, а не ту, что получил.
// Они расходятся: MSSQL выкидывает read-only колонки (identity, computed,
// rowversion), и обычный путь делает это хуком PostProcessRows уже после
// чтения. Потоку постфактум фильтровать нечего — строки уже ушли
// потребителю, — поэтому схема согласовывается заранее, и вызывающий обязан
// строить пакеты по возвращённой, а не по переданной.
type StreamingDataReader interface {
	ReadAllRowsStream(ctx context.Context, tableName string, schema packet.Schema) (packet.Schema, <-chan []string, <-chan error, error)
}

// StreamSQLRows перекладывает sql.Rows в канал, преобразуя ячейки тем же
// cellToTDTP, что и построчный ScanSQLRows.
//
// Общий cellToTDTP здесь не для экономии строк: потоковый и обычный экспорт
// обязаны давать одинаковые байты, иначе выбор режима менял бы данные — и
// менял бы xxh3, который по ним считается.
func StreamSQLRows(ctx context.Context, rows *sql.Rows, sch packet.Schema,
	converter *UniversalTypeConverter, dbType string, out chan<- []string) error {

	columnCount := len(sch.Fields)
	values := make([]any, columnCount)
	valuePtrs := make([]any, columnCount)
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		// Срез на строку выделяется заново: он уходит потребителю и живёт
		// столько, сколько нужно ему. Переиспользовать буфер нельзя — часть
		// накапливает строки до заполнения бюджета, и общий буфер сделал бы
		// их все алиасами последней прочитанной.
		row := make([]string, columnCount)
		for i, field := range sch.Fields {
			row[i] = cellToTDTP(values[i], field, converter, dbType)
		}
		select {
		case out <- row:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return rows.Err()
}
