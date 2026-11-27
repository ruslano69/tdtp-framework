package etl

// Кеш рабочей БД: SQLite с таблицами и индексами, сжатый zstd уровня 3.
//
// Повторный запуск пайплайна над теми же TDTP-файлами пропускает разбор,
// вставку строк и построение индексов.
//
// Формат файла:
//
//	[8]  magic "TDTPWSC1"
//	[4]  длина заголовка, little-endian
//	[16] xxh3_128 заголовка
//	[..] заголовок: JSON (workspaceCacheHeader)
//	[..] zstd-кадр с образом базы SQLite (кадр несёт свою контрольную сумму)
//
// Ключ и схемы лежат вне базы: ключ проверяется без распаковки, а SQL из
// transform не видит служебных таблиц.
//
// Совпавший ключ означает «собрано из этих же входов этой же программой». Он не
// доказывает, что содержимое базы не подменили: это локальный кеш, доверие к
// нему равно доверию к каталогу, где он лежит. Данные в нём открытые.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/zeebo/xxh3"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

const (
	workspaceCacheMagic  = "TDTPWSC1"
	workspaceCacheLevel  = 3
	maxCacheHeaderBytes  = 16 << 20
	workspaceCachePrefix = ".tdtp-wscache-" // временные файлы рядом с кешем
)

// errCacheStale — кеша нет или он собран из других входов. Это обычный промах,
// а не неисправность.
var errCacheStale = errors.New("workspace cache is stale")

type workspaceCacheHeader struct {
	Key     string                  `json:"key"`
	DBSize  int64                   `json:"db_size"`
	Sources []workspaceCachedSource `json:"sources"`
}

type workspaceCachedSource struct {
	Name   string        `json:"name"`
	Table  string        `json:"table"`
	Rows   int           `json:"rows"`
	Schema packet.Schema `json:"schema"`
}

// programHash — хеш исполняемого файла. Он входит в ключ: правила загрузки
// (типы колонок, разбор значений) меняются вместе с кодом, а номер версии,
// который надо не забыть поднять, рано или поздно забудут.
var programHash = sync.OnceValues(func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	h := xxh3.New()
	if err := hashFile(h, exe); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum128().Bytes()), nil
})

// hashFile добавляет в h размер и содержимое файла.
func hashFile(h io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(h, "%d\x00", info.Size()); err != nil {
		return err
	}
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if n != info.Size() {
		return fmt.Errorf("%s changed while being read", path)
	}
	return nil
}

// workspaceCacheKey описывает всё, от чего зависит содержимое рабочей БД:
// программу, настройки источников, содержимое их файлов и список индексов.
func workspaceCacheKey(sources []SourceConfig, indexes []WorkspaceIndexConfig) (string, error) {
	program, err := programHash()
	if err != nil {
		return "", err
	}

	h := xxh3.New()
	enc := json.NewEncoder(h)
	if err := enc.Encode([]any{program, workspaceCacheMagic, sources, indexes}); err != nil {
		return "", err
	}

	for _, source := range sources {
		var files []string
		if source.MultiPart {
			files = tdtpMultiPartFiles(source.DSN)
		}
		if files == nil {
			files = []string{source.DSN}
		}
		for _, path := range files {
			if err := enc.Encode(path); err != nil {
				return "", err
			}
			if err := hashFile(h, path); err != nil {
				return "", fmt.Errorf("source '%s': %w", source.Name, err)
			}
		}
	}

	return fmt.Sprintf("%x", h.Sum128().Bytes()), nil
}

// CreateIndexes строит индексы, перечисленные в конфиге кеша.
func (w *Workspace) CreateIndexes(ctx context.Context, indexes []WorkspaceIndexConfig) error {
	for i, index := range indexes {
		// SQLite принимает неизвестное имя в двойных кавычках за строковый
		// литерал и молча индексирует константу, поэтому колонки проверяются.
		existing, err := w.columnNames(ctx, index.Table)
		if err != nil {
			return err
		}
		if len(existing) == 0 {
			return fmt.Errorf("index %d: table %q does not exist in workspace", i+1, index.Table)
		}

		quoted := make([]string, len(index.Columns))
		for j, column := range index.Columns {
			if !existing[strings.ToLower(column)] {
				return fmt.Errorf("index %d: table %q has no column %q", i+1, index.Table, column)
			}
			quoted[j] = quoteIdent(column)
		}

		ddl := fmt.Sprintf("CREATE INDEX %s ON %s (%s)",
			quoteIdent(fmt.Sprintf("tdtp_cache_ix_%d", i+1)), quoteIdent(index.Table), strings.Join(quoted, ", "))
		if _, err := w.db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("index %d on table %q: %w", i+1, index.Table, err)
		}
	}
	return nil
}

func (w *Workspace) columnNames(ctx context.Context, table string) (map[string]bool, error) {
	rows, err := w.db.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, fmt.Errorf("read columns of %q: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	names := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[strings.ToLower(name)] = true
	}
	return names, rows.Err()
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// checkCacheTarget не даёт записать кеш поверх чужого файла: опечатка в
// cache.path не должна стоить исходных данных или результата.
func checkCacheTarget(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	magic := make([]byte, len(workspaceCacheMagic))
	if _, err := io.ReadFull(f, magic); err != nil || string(magic) != workspaceCacheMagic {
		return fmt.Errorf("%s exists and is not a workspace cache; refusing to overwrite it", path)
	}
	return nil
}

// saveWorkspaceCache пишет рабочую БД в path. Файл появляется целиком или не
// появляется вовсе. Несжатая копия базы на диск обычно не пишется (см.
// workspaceImage).
func (w *Workspace) saveWorkspaceCache(ctx context.Context, path string, header workspaceCacheHeader) (err error) {
	if err := checkCacheTarget(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	// Каталог создаётся доступным только владельцу: в нём открытые данные.
	tmp, err := os.MkdirTemp(dir, workspaceCachePrefix)
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := os.RemoveAll(tmp); removeErr != nil && err == nil {
			err = fmt.Errorf("remove temporary files %s: %w", tmp, removeErr)
		}
	}()

	database, size, err := w.workspaceImage(ctx, filepath.Join(tmp, "workspace.sqlite"))
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }() // до RemoveAll: defer идут в обратном порядке
	header.DBSize = size

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return err
	}
	if len(headerJSON) > maxCacheHeaderBytes {
		return fmt.Errorf("cache header is too large: %d bytes", len(headerJSON))
	}

	var prefix bytes.Buffer
	prefix.WriteString(workspaceCacheMagic)
	_ = binary.Write(&prefix, binary.LittleEndian, uint32(len(headerJSON)))
	sum := xxh3.Hash128(headerJSON).Bytes()
	prefix.Write(sum[:])
	prefix.Write(headerJSON)

	packedPath := filepath.Join(tmp, "cache.zst")
	packed, err := os.OpenFile(packedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = packed.Close() }()

	if _, err := packed.Write(prefix.Bytes()); err != nil {
		return err
	}
	encoder, err := zstd.NewWriter(packed,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(workspaceCacheLevel)),
		zstd.WithEncoderCRC(true))
	if err != nil {
		return err
	}
	if _, err := io.Copy(encoder, database); err != nil {
		_ = encoder.Close()
		return fmt.Errorf("compress cache: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("compress cache: %w", err)
	}
	if err := packed.Close(); err != nil {
		return err
	}

	return os.Rename(packedPath, path)
}

// serializeWorkspace подменяется в тестах, чтобы проверить запасной путь.
var serializeWorkspace = (*Workspace).serialize

// workspaceImage отдаёт образ рабочей БД. Обычно это копия в памяти: на замере
// с базой в 242 МБ она заняла 0,2 с против 4–13 с у копирования в файл.
// Serialize выделяет образ одним куском, и SQLite ограничивает такой кусок
// примерно 2 ГБ; тогда база копируется постранично в fallbackPath.
func (w *Workspace) workspaceImage(ctx context.Context, fallbackPath string) (io.ReadCloser, int64, error) {
	if image, err := serializeWorkspace(w, ctx); err == nil && len(image) > 0 {
		return io.NopCloser(bytes.NewReader(image)), int64(len(image)), nil
	}

	if err := w.backupToFile(ctx, fallbackPath); err != nil {
		return nil, 0, fmt.Errorf("copy workspace to file: %w", err)
	}
	f, err := os.Open(fallbackPath)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// loadWorkspaceCache восстанавливает рабочую БД из path в память.
// errCacheStale — обычный промах; любая другая ошибка означает, что кеш есть,
// но прочитать его не удалось. После такой ошибки workspace надо пересоздать.
func (w *Workspace) loadWorkspaceCache(ctx context.Context, path, key string) (header workspaceCacheHeader, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return header, errCacheStale
	}
	if err != nil {
		return header, err
	}
	defer func() { _ = f.Close() }()

	fixed := make([]byte, len(workspaceCacheMagic)+4+16)
	if _, err := io.ReadFull(f, fixed); err != nil || string(fixed[:len(workspaceCacheMagic)]) != workspaceCacheMagic {
		return header, fmt.Errorf("%s is not a workspace cache", path)
	}
	size := binary.LittleEndian.Uint32(fixed[len(workspaceCacheMagic):])
	if size > maxCacheHeaderBytes {
		return header, fmt.Errorf("cache header is too large: %d bytes", size)
	}
	headerJSON := make([]byte, size)
	if _, err := io.ReadFull(f, headerJSON); err != nil {
		return header, fmt.Errorf("read cache header: %w", err)
	}
	if sum := xxh3.Hash128(headerJSON).Bytes(); !bytes.Equal(sum[:], fixed[len(fixed)-16:]) {
		return header, fmt.Errorf("cache header checksum mismatch")
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return header, fmt.Errorf("parse cache header: %w", err)
	}
	if header.Key != key {
		return header, errCacheStale
	}

	tmp, err := os.MkdirTemp(filepath.Dir(path), workspaceCachePrefix)
	if err != nil {
		return header, err
	}
	defer func() {
		if removeErr := os.RemoveAll(tmp); removeErr != nil && err == nil {
			err = fmt.Errorf("remove temporary files %s: %w", tmp, removeErr)
		}
	}()

	dbPath := filepath.Join(tmp, "workspace.sqlite")
	if err := decompressCache(f, dbPath, header.DBSize); err != nil {
		return header, err
	}
	if err := w.restoreFromFile(ctx, dbPath); err != nil {
		return header, fmt.Errorf("restore workspace: %w", err)
	}

	for _, source := range header.Sources {
		w.tables[source.Table] = true
	}
	return header, nil
}

func decompressCache(r io.Reader, path string, size int64) error {
	decoder, err := zstd.NewReader(r)
	if err != nil {
		return err
	}
	defer decoder.Close()

	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	// +1 байт, чтобы заметить кадр длиннее объявленного и не писать его дальше.
	n, err := io.Copy(out, io.LimitReader(decoder, size+1))
	if err != nil {
		return fmt.Errorf("decompress cache: %w", err)
	}
	if n != size {
		return fmt.Errorf("cache size mismatch: header declares %d bytes", size)
	}
	return out.Close()
}
