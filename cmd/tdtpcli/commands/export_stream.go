package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// streamExportTable экспортирует таблицу, не собирая её в памяти.
//
// Обычный путь материализует всё трижды: ReadAllRows строит [][]string на всю
// таблицу, GenerateReference собирает все пакеты до начала записи, а очередь
// задач держит на каждый пакет вторую ссылку. Пик равен таблице плюс пакетам,
// и не падает до конца работы. На 24 млн строк это порядка 17-23 ГБ — цифра не
// теоретическая, она уже стоит в комментарии к SetMaxFallbackRows, где ту же
// проблему закрыли лимитом.
//
// Здесь часть живёт от появления до записи и дальше не удерживается: пиковая
// память равна одной части плюс буферу канала строк.
//
// ЦЕНА: число частей неизвестно, пока не прочитана последняя строка, а оно
// входит и в заголовок (TotalParts), и в имя файла (_part_N_of_M). Поэтому
// части пишутся под временными именами с TotalParts=0, а по завершении идёт
// финальный проход: проставить настоящее число и переименовать. Проход дешёвый
// — переписывается одна строка заголовка в каждом файле, — но он существует, и
// прерванный экспорт оставит на диске файлы с нулём в TotalParts.
func streamExportTable(
	ctx context.Context,
	reader base.StreamingDataReader,
	schema packet.Schema,
	tableName string,
	chain *processors.PacketChain,
	opts ExportOptions,
) error {
	if opts.OutputFile == "" || opts.OutputFile == "-" {
		return fmt.Errorf("--stream requires --output <file>: parts are renamed once the total is known, which stdout cannot do")
	}
	if opts.StorageCfg != nil {
		return fmt.Errorf("--stream does not support object storage yet: use --output <file>")
	}

	rowsChan, errChan, err := reader.ReadAllRowsStream(ctx, tableName, schema)
	if err != nil {
		return fmt.Errorf("failed to start streaming read: %w", err)
	}

	// Размер части — умолчание генератора: --packet-size до файлового экспорта
	// не доходит вовсе, он есть только у брокерного пути.
	gen := packet.NewStreamingGenerator()
	partsChan, summaryChan := gen.GeneratePartsStream(ctx, rowsChan, schema, tableName, packet.TypeReference)

	ext := filepath.Ext(opts.OutputFile)
	stem := opts.OutputFile[:len(opts.OutputFile)-len(ext)]

	var written []string
	var totalRows int

	for part := range partsChan {
		if part.Error != nil {
			return fmt.Errorf("streaming generate part %d: %w", part.PartNum, part.Error)
		}

		if err := chain.ProcessPacket(ctx, part.Packet); err != nil {
			return fmt.Errorf("processing part %d: %w", part.PartNum, err)
		}

		tmp := fmt.Sprintf("%s_part_%d%s.streaming", stem, part.PartNum, ext)
		if err := writePacketToFile(part.Packet, tmp); err != nil {
			return fmt.Errorf("writing part %d: %w", part.PartNum, err)
		}
		written = append(written, tmp)
		totalRows += part.RowsCount

		// Единственное место, где часть отпускается. Без этого поток
		// отличался бы от обычного пути только формой кода.
		part.Packet = nil

		fmt.Printf("  ✓ part %d — %d row(s)\n", part.PartNum, part.RowsCount)
	}

	// Ошибку чтения проверяем ПОСЛЕ закрытия канала строк: закрытие означает
	// «строк больше нет», а не «всё прошло хорошо».
	if err := <-errChan; err != nil {
		cleanupStreamParts(written)
		return fmt.Errorf("streaming read failed: %w", err)
	}

	summary := <-summaryChan
	if summary == nil {
		cleanupStreamParts(written)
		return fmt.Errorf("streaming export produced no summary")
	}

	if err := finalizeStreamParts(written, stem, ext, summary.TotalParts); err != nil {
		return err
	}

	fmt.Printf("✓ Total rows: %d in %d part(s)\n", totalRows, summary.TotalParts)
	return nil
}

// finalizeStreamParts проставляет настоящий TotalParts и даёт файлам штатные
// имена.
//
// TotalParts правится текстом, а не перечитыванием пакета: разбор части ради
// одного числа вернул бы в память ровно то, чего поток избегает.
//
// Элемент именно ВСТАВЛЯЕТСЯ, а не заменяется: поле объявлено с omitempty, и
// при TotalParts=0 писатель не выводит его вовсе. Первая версия искала
// <TotalParts>0</TotalParts> и падала на каждом файле — в них этой строки нет
// и быть не может. Место вставки — сразу после PartNumber, как в обычном
// заголовке, чтобы поток и обычный экспорт давали одинаковый порядок полей.
func finalizeStreamParts(tmpFiles []string, stem, ext string, totalParts int) error {
	elem := fmt.Sprintf("<TotalParts>%d</TotalParts>", totalParts)

	for i, tmp := range tmpFiles {
		data, err := os.ReadFile(tmp) //nolint:gosec // path built by this function
		if err != nil {
			return fmt.Errorf("finalize part %d: %w", i+1, err)
		}
		text := string(data)

		const afterPart = "</PartNumber>"
		at := strings.Index(text, afterPart)
		if at < 0 {
			return fmt.Errorf("finalize part %d: no %s — not a packet written by the streaming path", i+1, afterPart)
		}
		at += len(afterPart)
		fixed := text[:at] + elem + text[at:]

		final := fmt.Sprintf("%s_part_%d_of_%d%s", stem, i+1, totalParts, ext)
		if totalParts == 1 {
			final = stem + ext
		}
		if err := os.WriteFile(final, []byte(fixed), 0o600); err != nil {
			return fmt.Errorf("finalize part %d: %w", i+1, err)
		}
		if err := os.Remove(tmp); err != nil {
			return fmt.Errorf("finalize part %d: removing %s: %w", i+1, tmp, err)
		}
		fmt.Printf("  ✓ Written part %d/%d to: %s\n", i+1, totalParts, final)
	}
	return nil
}

// cleanupStreamParts убирает временные части после неудачи: файл с TotalParts=0
// и суффиксом .streaming — не результат, и оставлять его как результат нельзя.
func cleanupStreamParts(tmpFiles []string) {
	for _, f := range tmpFiles {
		_ = os.Remove(f)
	}
}
