# TODO NEXT: pkg/storage — Object Storage (S3/SeaweedFS)

## Цель

Добавить слой абстракции для работы с объектными хранилищами.
Первичная цель — поддержка **SeaweedFS** через протокол **S3**.
Модуль опциональный (build tags), поддерживает потоковую передачу без промежуточных файлов.

---

## Архитектурные решения (согласованы)

### io.Pipe() + manager.Uploader
TDTP-пакет ограничен ~3.8 МБ. Это меньше минимального S3 Multipart порога (5 МБ).
`manager.Uploader` буферизует поток из `PipeReader` до `PartSize=5MB`, видит EOF раньше порога
и отправляет пакет **одним `PutObject`** — без multipart, без temp-файлов.

**Гарантия памяти на воркер:**
- ~5 МБ буфер SDK + ~4 МБ XML/zstd pipeline ≈ 9 МБ
- 1 ГБ RAM → ~100 параллельных воркеров экспорта

**Инициализация Uploader:**
```go
uploader := manager.NewUploader(client, func(u *manager.Uploader) {
    u.PartSize    = 5 * 1024 * 1024  // 5 МБ — минимальный S3 Multipart порог
    u.Concurrency = 1                 // параллелизм — на уровне воркеров, не внутри пакета
})
```

`ContentLength` не передаём явно — SDK ставит его сам после EOF.

---

## Шаг 1 — Интерфейс и фабрика: `pkg/storage/`

**Новые файлы:**
```
pkg/storage/
├── storage.go      # ObjectStorage interface + ObjectInfo
└── factory.go      # Register / New
```

**`storage.go`:**
```go
type ObjectInfo struct {
    Key      string
    Size     int64
    ModTime  time.Time
    Metadata map[string]string  // tdtp-table, tdtp-protocol, tdtp-checksum
}

type ObjectStorage interface {
    Put(ctx context.Context, key string, reader io.Reader, meta map[string]string) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Stat(ctx context.Context, key string) (*ObjectInfo, error)
    List(ctx context.Context, prefix string) ([]ObjectInfo, error)
    Delete(ctx context.Context, key string) error
    Close() error
}
```

**`factory.go`** — паттерн идентичен `pkg/adapters/factory.go`:
```go
type Config struct {
    Type string   // "s3"
    S3   S3Config
}

type StorageConstructor func(cfg Config) (ObjectStorage, error)

func Register(storageType string, fn StorageConstructor)
func New(cfg Config) (ObjectStorage, error)
```

---

## Шаг 2 — S3-драйвер: `pkg/storage/s3/`

**Новые файлы:**
```
pkg/storage/s3/
├── s3.go           # //go:build !nos3  — реализация
├── s3_stub.go      # //go:build nos3   — заглушка
└── s3_test.go      # unit + mock тесты
```

**Зависимости (AWS SDK v2, Apache 2.0):**
```
github.com/aws/aws-sdk-go-v2
github.com/aws/aws-sdk-go-v2/config
github.com/aws/aws-sdk-go-v2/credentials
github.com/aws/aws-sdk-go-v2/service/s3
github.com/aws/aws-sdk-go-v2/feature/s3/manager
```

**`s3.go`** (`//go:build !nos3`):
```go
type S3Config struct {
    Endpoint   string
    Region     string
    Bucket     string
    AccessKey  string
    SecretKey  string
    DisableSSL bool
}

type Driver struct {
    client   *s3.Client
    uploader *manager.Uploader
    bucket   string
}
```

- `ForcePathStyle: true` — обязательно для SeaweedFS/MinIO
- `Put()` пишет metadata как `x-amz-meta-tdtp-*` заголовки
- `init()` регистрирует: `storage.Register("s3", NewDriver)`

**`s3_stub.go`** (`//go:build nos3`):
```go
func init() {
    storage.Register("s3", func(cfg storage.Config) (storage.ObjectStorage, error) {
        return nil, errors.New("S3 support is disabled in this build (nos3)")
    })
}
```

**`Put()` реализация:**
```go
func (d *Driver) Put(ctx context.Context, key string, r io.Reader, meta map[string]string) error {
    s3meta := make(map[string]string, len(meta))
    for k, v := range meta {
        s3meta["tdtp-"+k] = v  // x-amz-meta-tdtp-table, x-amz-meta-tdtp-rows, ...
    }
    _, err := d.uploader.Upload(ctx, &s3.PutObjectInput{
        Bucket:   aws.String(d.bucket),
        Key:      aws.String(key),
        Body:     r,
        Metadata: s3meta,
    })
    return err
}
```

---

## Шаг 3 — go.mod

```bash
GOPROXY=https://goproxy.io GONOSUMDB='*' go get \
    github.com/aws/aws-sdk-go-v2/config \
    github.com/aws/aws-sdk-go-v2/credentials \
    github.com/aws/aws-sdk-go-v2/service/s3 \
    github.com/aws/aws-sdk-go-v2/feature/s3/manager
```

---

## Шаг 4 — Config: `cmd/tdtpcli/config.go`

Добавить в `Config`:
```go
type Config struct {
    Database   DatabaseConfig   `yaml:"database"`
    Storage    StorageConfig    `yaml:"storage,omitempty"`   // NEW
    Export     ExportConfig     `yaml:"export,omitempty"`
    // ... без изменений
}

type StorageConfig struct {
    Type string   `yaml:"type"`
    S3   S3Config `yaml:"s3,omitempty"`
}

type S3Config struct {
    Endpoint   string `yaml:"endpoint"`
    Region     string `yaml:"region"`
    Bucket     string `yaml:"bucket"`
    AccessKey  string `yaml:"access_key"`
    SecretKey  string `yaml:"secret_key"`
    DisableSSL bool   `yaml:"disable_ssl,omitempty"`
}
```

**Пример config.yaml:**
```yaml
storage:
  type: s3
  s3:
    endpoint: "http://localhost:8888"   # SeaweedFS Filer
    region: "us-east-1"                 # заглушка для S3
    bucket: "reports"
    access_key: "admin"
    secret_key: "123"
    disable_ssl: true
```

---

## Шаг 5 — CLI build tag: `cmd/tdtpcli/drivers_s3.go`

По аналогии с `drivers_sqlite.go`:
```go
//go:build !nos3

package main

import _ "github.com/ruslano69/tdtp-framework/pkg/storage/s3"
```

---

## Шаг 6 — URI парсинг: `cmd/tdtpcli/storage_uri.go`

```go
// ParseStorageURI разбирает "s3://bucket/path/to/file.tdtp"
// Возвращает (scheme, bucket, key, isRemote)
func ParseStorageURI(uri string) (scheme, bucket, key string, remote bool)

// IsRemoteURI проверяет наличие префикса s3://
func IsRemoteURI(path string) bool
```

Формат: `s3://bucket_name/path/to/file.xml.zstd`
Используется в существующих флагах `--output` и `--import` — новых флагов не добавляем.

---

## Шаг 7 — Streaming экспорт: `cmd/tdtpcli/commands/export.go`

Изменить существующий файл, добавить ветку:
```
if IsRemoteURI(outputPath):
    store := openStorageFromConfig(cfg)
    StreamExportToStorage(ctx, adapter, store, tableName, key, opts)
else:
    существующий путь записи в файл
```

**StreamExportToStorage — zero-copy pipeline:**
```
DB cursor
  → packet.Generator (XML serialize)
  → zstd compress (если --compress)
  → io.Pipe
  → storage.Put(ctx, key, pipeReader, meta)
```

Metadata: `table`, `protocol: TDTP 1.0`, `rows`, `checksum`.

---

## Шаг 8 — Streaming импорт: `cmd/tdtpcli/commands/import.go`

Изменить существующий файл, добавить ветку:
```
if IsRemoteURI(inputPath):
    reader, _ := store.Get(ctx, key)
    defer reader.Close()
    → передать reader в существующий parser pipeline
```

---

## Шаг 9 — Тесты

**`pkg/storage/s3/s3_test.go`** (`//go:build !nos3`):
- `TestParseS3URI` — unit, без зависимостей
- `TestS3DriverMock` — мок через `httptest.NewServer`, проверяет Put/Get/List/Delete

**`tests/manual/test_seaweedfs.sh`:**
```bash
# Поднять SeaweedFS в Docker
# Экспорт:
tdtpcli --config config.yaml --export Users --output s3://tdtp-test/users.tdtp
# Импорт:
tdtpcli --config config.yaml --import s3://tdtp-test/users.tdtp
```

---

## Порядок коммитов

| # | Сообщение коммита | Файлы |
|---|-------------------|-------|
| 1 | `feat(storage): add ObjectStorage interface and factory` | `pkg/storage/storage.go`, `pkg/storage/factory.go` |
| 2 | `feat(storage/s3): add S3 driver with SeaweedFS support` | `pkg/storage/s3/s3.go`, `s3_stub.go` |
| 3 | `chore(deps): add AWS SDK v2 for S3` | `go.mod`, `go.sum` |
| 4 | `feat(config): add StorageConfig for object storage` | `cmd/tdtpcli/config.go` |
| 5 | `feat(cli): add s3:// URI parsing and drivers_s3 build tag` | `storage_uri.go`, `drivers_s3.go` |
| 6 | `feat(cli): streaming export to S3 via io.Pipe zero-copy` | `commands/export.go` |
| 7 | `feat(cli): streaming import from S3` | `commands/import.go` |
| 8 | `test(storage/s3): unit + mock tests` | `pkg/storage/s3/s3_test.go` |
| 9 | `test(manual): SeaweedFS integration test script` | `tests/manual/test_seaweedfs.sh` |

---

## Риски и решения

| Риск | Решение |
|------|---------|
| AWS SDK v2 транзитивные зависимости | `go mod tidy` после добавления |
| `manager.Uploader` и неизвестный размер | PartSize=5MB > пакет ~3.8MB → один PutObject, SDK решает сам |
| SeaweedFS совместимость | `ForcePathStyle: true` + кастомный `EndpointResolver` |
| `go get` через proxy.golang.org заблокирован | `GOPROXY=https://goproxy.io GONOSUMDB='*'` (см. CLAUDE.md) |
