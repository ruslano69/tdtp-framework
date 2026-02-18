package etl

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// multiPartRe matches filenames like: base_part_N_of_Total.ext
var multiPartRe = regexp.MustCompile(`^(.+)_part_(\d+)_of_(\d+)(\..+)$`)

// tdtpMultiPartFiles возвращает все части multi-part набора, если DSN указывает
// на одну из частей или на базовое имя файла.  Возвращает nil для одиночных файлов.
func tdtpMultiPartFiles(filePath string) []string {
	var base, ext string
	var total int

	if m := multiPartRe.FindStringSubmatch(filePath); m != nil {
		base = m[1]
		ext = m[4]
		total, _ = strconv.Atoi(m[3])
	} else {
		ext = filepath.Ext(filePath)
		base = filePath[:len(filePath)-len(ext)]
		matches, err := filepath.Glob(fmt.Sprintf("%s_part_1_of_*%s", base, ext))
		if err == nil && len(matches) == 1 {
			if m := multiPartRe.FindStringSubmatch(matches[0]); m != nil {
				total, _ = strconv.Atoi(m[3])
			}
		}
	}

	if total < 2 {
		return nil
	}

	parts := make([]string, total)
	for i := range parts {
		parts[i] = fmt.Sprintf("%s_part_%d_of_%d%s", base, i+1, total, ext)
	}
	return parts
}

// decompressTDTPPacket распаковывает строки пакета если они сжаты (zstd).
// Алгоритм идентичен ImportFile: checksum → decompress → замена rows.
func decompressTDTPPacket(pkt *packet.DataPacket) error {
	if pkt.Data.Compression == "" {
		return nil
	}
	if len(pkt.Data.Rows) != 1 {
		return fmt.Errorf("compressed TDTP packet must have exactly 1 row, got %d", len(pkt.Data.Rows))
	}
	compressed := pkt.Data.Rows[0].Value
	if pkt.Data.Checksum != "" {
		if err := processors.ValidateChecksum([]byte(compressed), pkt.Data.Checksum); err != nil {
			return fmt.Errorf("checksum mismatch: %w", err)
		}
	}
	rows, err := processors.DecompressDataForTdtp(compressed)
	if err != nil {
		return fmt.Errorf("failed to decompress: %w", err)
	}
	pkt.Data.Compression = ""
	pkt.Data.Checksum = ""
	pkt.Data.Rows = make([]packet.Row, len(rows))
	for i, r := range rows {
		pkt.Data.Rows[i] = packet.Row{Value: r}
	}
	return nil
}

// loadTDTPFile читает TDTP XML-файл напрямую, минуя адаптерный слой.
// DSN для tdtp-источника — это путь к файлу.
// Поведение совпадает с ImportFile: multi-part-набор объединяется в один пакет,
// сжатые данные распаковываются до загрузки в SQLite workspace.
func loadTDTPFile(source SourceConfig) (*packet.DataPacket, error) {
	if source.DSN == "" {
		return nil, fmt.Errorf("tdtp source requires 'dsn' to be the file path")
	}

	var filePaths []string
	if source.MultiPart {
		filePaths = tdtpMultiPartFiles(source.DSN)
	}
	if filePaths == nil {
		filePaths = []string{source.DSN}
	}

	parser := packet.NewParser()
	var merged *packet.DataPacket

	for _, fp := range filePaths {
		pkt, err := parser.ParseFile(fp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse TDTP file '%s': %w", fp, err)
		}
		if err := decompressTDTPPacket(pkt); err != nil {
			return nil, fmt.Errorf("file '%s': %w", fp, err)
		}
		if merged == nil {
			merged = pkt
		} else {
			// Склеиваем строки последующих частей в первый пакет.
			merged.Data.Rows = append(merged.Data.Rows, pkt.Data.Rows...)
			merged.Header.RecordsInPart += pkt.Header.RecordsInPart
		}
	}

	// Переименовываем таблицу в alias источника — workspace использует это
	// имя как имя SQLite-таблицы, и именно это имя используется в transform SQL.
	merged.Header.TableName = source.Name
	return merged, nil
}

// SourceData представляет загруженные данные из одного источника
type SourceData struct {
	SourceName string
	TableName  string
	Packet     *packet.DataPacket
	Error      error
}

// Loader отвечает за загрузку данных из источников
type Loader struct {
	sources       []SourceConfig
	errorHandling ErrorHandlingConfig
}

// NewLoader создает новый загрузчик данных
func NewLoader(sources []SourceConfig, errorHandling ErrorHandlingConfig) *Loader {
	return &Loader{
		sources:       sources,
		errorHandling: errorHandling,
	}
}

// LoadAll загружает данные из всех источников параллельно
func (l *Loader) LoadAll(ctx context.Context) ([]SourceData, error) {
	if len(l.sources) == 0 {
		return nil, fmt.Errorf("no sources configured")
	}

	// Канал для результатов
	results := make(chan SourceData, len(l.sources))

	// WaitGroup для ожидания завершения всех горутин
	var wg sync.WaitGroup

	// Запускаем загрузку из каждого источника параллельно
	for _, source := range l.sources {
		wg.Add(1)
		go func(src SourceConfig) {
			defer wg.Done()

			result := SourceData{
				SourceName: src.Name,
				TableName:  src.Name,
			}

			// Загружаем данные из источника
			packet, err := l.loadFromSource(ctx, src)
			if err != nil {
				result.Error = err
			} else {
				result.Packet = packet
			}

			results <- result
		}(source)
	}

	// Ждем завершения всех горутин и закрываем канал
	go func() {
		wg.Wait()
		close(results)
	}()

	// Собираем результаты
	var allResults []SourceData
	var sourceErrors []error

	for result := range results {
		allResults = append(allResults, result)
		if result.Error != nil {
			sourceErrors = append(sourceErrors, fmt.Errorf("source '%s': %w", result.SourceName, result.Error))
		}
	}

	// Обработка ошибок согласно on_source_error стратегии
	if len(sourceErrors) > 0 {
		switch l.errorHandling.OnSourceError {
		case "continue":
			// Continue: возвращаем все результаты (включая ошибочные) и все ошибки
			// Processor решит что делать с источниками где Error != nil
			return allResults, errors.Join(sourceErrors...)

		case "fail":
			// Fail: останавливаемся на первой ошибке
			return allResults, sourceErrors[0]

		default:
			// По умолчанию fail
			return allResults, sourceErrors[0]
		}
	}

	return allResults, nil
}

// LoadOne загружает данные из одного источника
func (l *Loader) LoadOne(ctx context.Context, sourceName string) (*SourceData, error) {
	// Ищем источник по имени
	var source *SourceConfig
	for _, src := range l.sources {
		if src.Name == sourceName {
			source = &src
			break
		}
	}

	if source == nil {
		return nil, fmt.Errorf("source '%s' not found", sourceName)
	}

	// Загружаем данные
	packet, err := l.loadFromSource(ctx, *source)
	if err != nil {
		return &SourceData{
			SourceName: source.Name,
			TableName:  source.Name,
			Error:      err,
		}, err
	}

	return &SourceData{
		SourceName: source.Name,
		TableName:  source.Name,
		Packet:     packet,
	}, nil
}

// loadFromSource загружает данные из конкретного источника
func (l *Loader) loadFromSource(ctx context.Context, source SourceConfig) (*packet.DataPacket, error) {
	// Применяем timeout из конфигурации источника
	var timeoutCtx context.Context
	var cancel context.CancelFunc

	if source.Timeout > 0 {
		timeoutCtx, cancel = context.WithTimeout(ctx, time.Duration(source.Timeout)*time.Second)
		defer cancel()
	} else {
		timeoutCtx = ctx
	}

	// TDTP-файл не требует адаптера — данные уже в TDTP-формате, читаем напрямую.
	if source.Type == "tdtp" {
		return loadTDTPFile(source)
	}
	_ = timeoutCtx // используется далее

	// Создаем адаптер для источника
	adapter, err := adapters.New(timeoutCtx, adapters.Config{
		Type: source.Type,
		DSN:  source.DSN,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(timeoutCtx)

	// Проверяем соединение
	if err := adapter.Ping(timeoutCtx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Выполняем SQL запрос источника с учетом timeout
	// Используем ExecuteRawSQL для выполнения произвольного SELECT
	packet, err := l.executeSourceQuery(timeoutCtx, adapter, source)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Обновляем имя таблицы в пакете на alias
	packet.Header.TableName = source.Name

	return packet, nil
}

// executeSourceQuery выполняет SQL запрос источника и возвращает DataPacket
func (l *Loader) executeSourceQuery(ctx context.Context, adapter adapters.Adapter, source SourceConfig) (*packet.DataPacket, error) {
	// Для выполнения произвольного SQL нам нужно получить прямой доступ к *sql.DB
	// Используем интерфейс RawQueryExecutor если адаптер его поддерживает

	type RawQueryExecutor interface {
		ExecuteRawQuery(ctx context.Context, query string) (*packet.DataPacket, error)
	}

	executor, ok := adapter.(RawQueryExecutor)
	if !ok {
		// Если адаптер не поддерживает ExecuteRawQuery, используем обходной путь
		// Это временное решение - в Phase 2 мы добавим ExecuteRawQuery во все адаптеры
		return nil, fmt.Errorf("adapter does not support ExecuteRawQuery (will be implemented in next step)")
	}

	return executor.ExecuteRawQuery(ctx, source.Query)
}

// GetSourceCount возвращает количество сконфигурированных источников
func (l *Loader) GetSourceCount() int {
	return len(l.sources)
}

// GetSourceNames возвращает имена всех источников
func (l *Loader) GetSourceNames() []string {
	names := make([]string, len(l.sources))
	for i, src := range l.sources {
		names[i] = src.Name
	}
	return names
}
