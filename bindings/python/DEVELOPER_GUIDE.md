# libtdtp Python Bindings — Developer Guide

**Версия:** 1.0
**Репозиторий:** https://github.com/ruslano69/tdtp-framework
**Расположение:** `bindings/python/`

---

## Содержание

1. [Архитектура](#архитектура)
2. [Быстрый старт](#быстрый-старт)
3. [Сборка библиотеки](#сборка-библиотеки)
4. [JSON API (J_*)](#json-api-j_)
5. [Direct API (D_*)](#direct-api-d_)
6. [Pandas-интеграция](#pandas-интеграция)
7. [Обработка ошибок](#обработка-ошибок)
8. [Тестирование](#тестирование)
9. [Структура кода](#структура-кода)
10. [Разработка и расширение](#разработка-и-расширение)

---

## Архитектура

```
┌─────────────────────────────────────────────┐
│              Python код                      │
│  TDTPClientJSON    TDTPClientDirect          │
│       api_j.py          api_d.py            │
└──────────────┬────────────────┬─────────────┘
               │                │
               ▼                ▼
┌─────────────────────────────────────────────┐
│         ctypes слой (_loader.py)             │
│  argtypes / restype для всех экспортов       │
└──────────────────────┬──────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────┐
│       libtdtp.so / .dll / .dylib            │
│         Go CGo shared library               │
│                                             │
│  J_* (JSON boundary)   D_* (struct boundary)│
│  exports_j.go          exports_d.go         │
│  exports_j_compress.go exports_d_compress.go│
│  exports_j_serialize.go                     │
└─────────────────────────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────────┐
│         Go core (pkg/core/...)               │
│  packet.Parser  tdtql.Executor  processors  │
└─────────────────────────────────────────────┘
```

### Два API-семейства

| | **J_*** (JSON) | **D_*** (Direct) |
|--|--|--|
| Граница | JSON-строки | C-структуры (`ctypes`) |
| Память | Auto (Go alloc/free) | Явная (`D_FreePacket`) |
| Накладные расходы | Сериализация JSON | Нет |
| Удобство | Высокое | Требует осторожности |
| Рекомендован для | Общего использования | Критичных по скорости задач |

Оба семейства используют один и тот же Go-движок: один `packet.DataPacket`,
один `tdtql.Executor`, одни процессоры.

---

## Быстрый старт

```bash
cd bindings/python
make build-lib          # собрать libtdtp.so (базовая версия)
make install-dev        # pip install -e ".[dev]"
make test               # убедиться, что всё работает
```

```python
from tdtp import TDTPClientJSON

client = TDTPClientJSON()

# Прочитать .tdtp файл
data = client.J_read("users.tdtp.xml")
print(f"Строк: {len(data['data'])}")
print(f"Поля: {[f['Name'] for f in data['schema']['Fields']]}")

# Фильтрация через TDTQL
result = client.J_filter(data, "Balance > 1000 AND City = 'Moscow'")
print(f"Найдено: {len(result['data'])}")

# Сохранить
client.J_write(result, "filtered.tdtp.xml")
```

---

## Сборка библиотеки

### Требования

- Go 1.21+ с включённым CGO
- Python 3.9+
- gcc/clang (Linux/macOS) или MSVC (Windows)

### Команды сборки

```bash
cd bindings/python

# Базовая сборка (чтение/запись/фильтрация/diff)
make build-lib

# Полная сборка (+ сжатие, процессоры, цепочки)
make build-lib-full
```

Под капотом `build-lib-full`:
```bash
cd ../../  # корень репозитория
go build -tags compress -buildmode=c-shared \
    -o bindings/python/tdtp/libtdtp.so \
    ./pkg/python/libtdtp
```

Артефакты помещаются в `bindings/python/tdtp/`:
- `libtdtp.so` (Linux) / `libtdtp.dll` (Windows) / `libtdtp.dylib` (macOS)
- `libtdtp.h` — заголовочный файл (нужен только при разработке CGo)

### Переменная окружения

Если библиотека лежит в нестандартном месте:
```bash
export TDTP_LIB_PATH=/path/to/libtdtp.so
```

### Порядок поиска библиотеки (`_loader.py`)

1. `TDTP_LIB_PATH` (env)
2. Директория пакета (installed wheel)
3. `bindings/python/tdtp/` (режим разработки)

---

## JSON API (J_*)

Класс `TDTPClientJSON` — stateless, thread-safe.

### Чтение и запись

```python
client = TDTPClientJSON()

# Прочитать файл → dict
data = client.J_read("path/to/file.tdtp.xml")
# Структура:
# {
#   "schema": {"Fields": [{"Name": "ID", "Type": "INTEGER", ...}, ...]},
#   "header": {"type": "reference", "table_name": "users",
#               "message_id": "uuid", "timestamp": "2024-01-15T12:00:00Z"},
#   "data": [["1", "Alice", "alice@ex.com"], ["2", "Bob", "bob@ex.com"], ...]
# }

# Записать dict → файл
client.J_write(data, "output.tdtp.xml")
```

### Фильтрация (TDTQL)

```python
# Простая фильтрация
result = client.J_filter(data, "Age > 18")

# Составное условие
result = client.J_filter(
    data,
    "Balance BETWEEN 1000 AND 5000 AND City IN ('Moscow', 'Omsk')"
)

# NULL-проверки
result = client.J_filter(data, "Email IS NOT NULL")

# Сортировка и лимит
result = client.J_filter(data, "Balance > 0", limit=50)
```

**Операторы TDTQL:** `=`, `!=`, `>`, `>=`, `<`, `<=`,
`IN (...)`, `NOT IN (...)`, `BETWEEN v1 AND v2`,
`LIKE 'pattern%'`, `NOT LIKE ...`, `IS NULL`, `IS NOT NULL`

### Пагинация

```python
page_size = 100
offset = 0

while True:
    page = client.J_filter(data, "Balance > 0", limit=page_size, offset=offset)
    process(page["data"])

    qc = page["query_context"]
    # qc = {
    #   "total_records": 1000,
    #   "matched_records": 750,
    #   "returned_records": 100,
    #   "more_available": True,
    #   "next_offset": 100,
    #   "limit": 100,
    #   "offset": 0,
    # }

    if not qc["more_available"]:
        break
    offset = qc["next_offset"]
```

### Процессоры (требует `build-lib-full`)

```python
# Маскирование полей
masked = client.J_apply_processor(
    data,
    "field_masker",
    fields={"Email": "stars", "Phone": "partial"}
)

# Нормализация
normalized = client.J_apply_processor(
    data,
    "field_normalizer",
    fields={"Email": "lowercase", "Phone": "phone"}
)

# Сжатие
compressed = client.J_apply_processor(data, "compress", level=5)
# compressed["compression"] == "zstd"

# Распаковка
decompressed = client.J_apply_processor(compressed, "decompress")

# Цепочка процессоров
result = client.J_apply_chain(data, [
    {"type": "field_masker", "params": {"fields": {"Email": "stars"}}},
    {"type": "compress",     "params": {"level": 3}},
])
```

### Экспорт с партиционированием

```python
# Автоматически разбивает на части по размеру
result = client.J_export_all(
    data,
    base_path="exports/Users",   # → Users_part_1_of_3.tdtp.xml
    compress=True,
    level=3,
    checksum=True,
)
# result = {"files": ["Users_part_1_of_3.tdtp.xml", ...], "total_parts": 3}
```

### Diff

```python
diff = client.J_diff(old_data, new_data)
# diff = {
#   "added":    [...],   # строки, есть в new, нет в old
#   "removed":  [...],   # строки, есть в old, нет в new
#   "modified": [...],   # строки, изменились
#   "stats":    {"added": 5, "removed": 2, "modified": 3}
# }
```

---

## Direct API (D_*)

Класс `TDTPClientDirect` — максимальная производительность,
явное управление памятью.

### PacketHandle и управление памятью

Каждая D_* операция выделяет память через `C.malloc()`.
**Вы обязаны** освободить её вызовом `free()` или через context manager.

```python
from tdtp import TDTPClientDirect

client = TDTPClientDirect()

# ❌ Утечка памяти
pkt = client.D_read("file.tdtp")
rows = pkt.get_rows()
# pkt никогда не освобождён!

# ✅ Явное освобождение
pkt = client.D_read("file.tdtp")
try:
    rows = pkt.get_rows()
finally:
    pkt.free()

# ✅ Context manager (рекомендуется)
with client.D_read_ctx("file.tdtp") as pkt:
    rows = pkt.get_rows()
# pkt.free() вызывается автоматически
```

### Пример — цепочка операций

```python
with client.D_read_ctx("users.tdtp.xml") as src:
    schema = src.get_schema()   # [{"name": "ID", "type": "INTEGER", ...}, ...]
    print(f"Строк: {len(src.get_rows())}")

    with client.D_filter(
        src,
        [{"field": "Balance", "op": "gt", "value": "1000"}]
    ) as filtered:

        with client.D_apply_mask(
            filtered,
            fields=["Email"],
            mask_char="*",
            visible_chars=0
        ) as masked:
            client.D_write(masked, "result.tdtp.xml")
```

### Операторы фильтра (D_filter)

```python
filters = [
    {"field": "Balance", "op": "gt",      "value": "1000"},
    {"field": "City",    "op": "eq",      "value": "Moscow"},
    {"field": "Age",     "op": "between", "value": "18", "value2": "65"},
    {"field": "Email",   "op": "like",    "value": "%@gmail.com"},
    {"field": "Phone",   "op": "is_null"},
]
```

**Все операторы:** `eq`, `ne`, `gt`, `gte`, `lt`, `lte`,
`in`, `not_in`, `between`, `like`, `not_like`, `is_null`, `is_not_null`

### Сжатие (требует `build-lib-full`)

```python
with client.D_read_ctx("data.tdtp.xml") as pkt:
    with client.D_compress(pkt, level=3) as compressed:
        assert compressed.pkt.compression == b"zstd"
        client.D_write(compressed, "data.tdtp.zstd")

        with client.D_decompress(compressed) as restored:
            assert restored.get_rows() == pkt.get_rows()
```

---

## Pandas-интеграция

```python
import pandas as pd
from tdtp import TDTPClientJSON

client = TDTPClientJSON()
data = client.J_read("users.tdtp.xml")

# TDTP → DataFrame
df = client.J_to_pandas(data)
print(df.dtypes)
# ID           Int64
# Name         object
# Balance      float64
# IsActive     boolean
# CreatedAt    object

# Обработка в pandas
high_value = df[df["Balance"] > 2000].copy()
high_value["Name"] = high_value["Name"].str.upper()

# DataFrame → TDTP
result = client.J_from_pandas(high_value, table_name="high_value_users")
client.J_write(result, "high_value.tdtp.xml")
```

### Маппинг типов

| TDTP Type | pandas dtype |
|-----------|-------------|
| `INTEGER` | `Int64` (nullable) |
| `REAL` | `float64` |
| `BOOLEAN` | `boolean` (nullable) |
| `TEXT` | `object` |
| `DATETIME` / `TIMESTAMP` | `object` (строка ISO-8601) |
| `BLOB` | `object` (hex-строка) |

Пустая строка `""` в TDTP → `pd.NA` (для nullable типов) или `None` (object).

### Сериализация значений (`pandas_ext.py`)

Сериализация делегируется в Go (`J_SerializeValue`) — единый источник правды:

| Тип Python | Результат |
|-----------|-----------|
| `None` / `NaN` / `pd.NA` / `pd.NaT` | `""` (NULL в TDTP) |
| `bool` / `numpy.bool_` | `"true"` / `"false"` |
| `float` без дроби (71160.0) | `"71160"` |
| `bytes` / `bytearray` | Base64 (через Go BLOB serializer) |
| `datetime` / `pd.Timestamp` | UTC RFC3339 (через Go TIMESTAMP serializer) |
| `dict` / `list` | Compact JSON (через Go JSON serializer) |
| Всё остальное | `str(v)` |

---

## Обработка ошибок

```python
from tdtp.exceptions import (
    TDTPError,          # базовый класс
    TDTPParseError,     # ошибка разбора файла
    TDTPFilterError,    # невалидный WHERE или ошибка фильтрации
    TDTPProcessorError, # ошибка процессора (mask/compress/...)
    TDTPWriteError,     # ошибка записи файла
    TDTPLibraryError,   # не удалось загрузить libtdtp.so
)

try:
    data = client.J_read("missing.tdtp.xml")
except TDTPParseError as e:
    print(f"Ошибка разбора: {e}")
except TDTPError as e:
    print(f"Общая ошибка TDTP: {e}")
```

**Важно:** при загрузке модуля (`import tdtp`) сразу проверяется наличие
`libtdtp.so`. Если библиотека не найдена — бросается `TDTPLibraryError`.

---

## Тестирование

```bash
cd bindings/python

# Собрать библиотеку (нужно сделать один раз)
make build-lib-full

# Запустить все тесты
make test

# Только JSON API
make test-j          # pytest tests/test_api_j.py -v

# Только Direct API
make test-d          # pytest tests/test_api_d.py -v

# Бенчмарки (J_* vs D_*)
make bench
```

### Тестовые данные

`tests/testdata/`:
- `users.tdtp.xml` — 8 строк, 7 полей (ID, Name, Email, City, Balance, IsActive, CreatedAt)
- `users_nullable.tdtp.xml` — содержит NULL-значения
- `users_compressed.tdtp.xml` — сжатый файл (zstd)

Константы в `tests/conftest.py`:
```python
SAMPLE_TOTAL_ROWS = 8
SAMPLE_BALANCE_GT_1000_COUNT = 5
SAMPLE_MOSCOW_COUNT = 2
```

### Написание нового теста

```python
# tests/test_api_j.py (добавить в нужный класс или создать новый)

class TestJMyFeature:
    def test_my_case(self, j_client, sample_data_j):
        result = j_client.J_filter(sample_data_j, "ID > 3")
        assert len(result["data"]) == 5
        # убедиться, что query_context не ломает structure
        assert "schema" in result
        assert "header" in result

    def test_error_handling(self, j_client, tmp_tdtp):
        with pytest.raises(TDTPFilterError):
            j_client.J_filter({}, "INVALID ??? SYNTAX")
```

---

## Структура кода

```
bindings/python/
├── Makefile                   # Команды сборки и тестирования
├── pyproject.toml             # Метаданные пакета (setuptools/poetry)
├── DEVELOPER_GUIDE.md         # Этот файл
│
├── tdtp/                      # Python-пакет
│   ├── __init__.py            # Публичный API: TDTPClientJSON, TDTPClientDirect, ...
│   ├── _loader.py             # Загрузка libtdtp.so, настройка ctypes-сигнатур
│   ├── _structs_d.py          # ctypes.Structure для D_* API (D_Packet, D_Field, ...)
│   ├── api_j.py               # TDTPClientJSON — высокоуровневый JSON-клиент
│   ├── api_d.py               # TDTPClientDirect, PacketHandle — Direct-клиент
│   ├── exceptions.py          # Иерархия исключений TDTPError
│   ├── pandas_ext.py          # data_to_pandas(), pandas_to_data()
│   ├── libtdtp.so             # (создаётся make build-lib)
│   └── libtdtp.h              # (создаётся make build-lib)
│
└── tests/
    ├── conftest.py            # Фикстуры pytest (клиенты, пути, константы)
    ├── test_api_j.py          # Тесты JSON API
    ├── test_api_d.py          # Тесты Direct API (включая memory safety)
    ├── test_bench.py          # Бенчмарки производительности
    ├── test_pandas.py         # Тесты pandas-интеграции
    └── testdata/              # Тестовые .tdtp файлы
```

Go-исходники CGo-слоя:
```
pkg/python/libtdtp/
├── main.go                    # package main, import "C", build instructions
├── tdtp_structs.h             # C-определения структур (D_Packet, D_Field, ...)
├── exports_d.go               # D_ReadFile, D_WriteFile, D_FilterRows, D_ApplyMask, D_FreePacket
├── exports_d_compress.go      # D_ApplyCompress, D_ApplyDecompress (build tag: compress)
├── exports_d_compress_stub.go # Stub без сжатия
├── exports_j.go               # J_ReadFile, J_WriteFile, J_FilterRows[Page], J_Diff, J_ExportAll
├── exports_j_compress.go      # J_ApplyProcessor, J_ApplyChain (build tag: compress)
└── exports_j_serialize.go     # J_SerializeValue — канонический сериализатор типов
```

---

## Разработка и расширение

### Добавить новую J_* функцию

1. **Go (exports_j.go):**

```go
//export J_MyNewFunction
func J_MyNewFunction(dataJSON *C.char, param *C.char) *C.char {
    pkt, err := unmarshalJPacket(C.GoString(dataJSON))
    if err != nil {
        return jErr(err.Error())
    }

    // ... логика ...

    result := jPacket{/* ... */}
    return jOK(result)
}
```

2. **Загрузчик (_loader.py):**

```python
lib.J_MyNewFunction.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
lib.J_MyNewFunction.restype  = ctypes.c_void_p
```

3. **Клиент (api_j.py):**

```python
def J_my_new_function(self, data: dict, param: str) -> dict:
    return self._call(
        lib.J_MyNewFunction,
        json.dumps(data).encode(),
        param.encode(),
    )
```

4. **Тесты (tests/test_api_j.py):**

```python
class TestJMyNewFunction:
    def test_basic(self, j_client, sample_data_j):
        result = j_client.J_my_new_function(sample_data_j, "param_value")
        assert "data" in result
```

5. **Пересобрать:**
```bash
make build-lib
make test
```

### Добавить новый D_* функцию

Аналогично, но использовать C-структуры:

```go
// exports_d.go
//export D_MyTransform
func D_MyTransform(src *C.D_Packet, out *C.D_Packet) C.int {
    rows, schema := dGetRows(src), dGetSchema(src)
    // ... трансформация ...
    dFillRows(out, transformedRows)
    dFillSchema(out, schema)
    return 0
}
```

```python
# _loader.py
lib.D_MyTransform.argtypes = [ctypes.POINTER(D_Packet), ctypes.POINTER(D_Packet)]
lib.D_MyTransform.restype  = ctypes.c_int

# api_d.py
def D_my_transform(self, handle: PacketHandle) -> PacketHandle:
    out = D_Packet()
    rc = lib.D_MyTransform(ctypes.byref(handle.pkt), ctypes.byref(out))
    if rc != 0:
        raise TDTPProcessorError(out.get_error())
    return PacketHandle(out)
```

### Добавить поддержку нового типа данных в pandas

`pandas_ext.py` — таблицы `_TDTP_TO_PANDAS` и `_PANDAS_TO_TDTP`.
Для нетривиальной сериализации (bytes, datetime, JSON) — делегировать в `J_SerializeValue`.

```python
# Пример: новый тип MONEY → Decimal
_TDTP_TO_PANDAS["MONEY"] = "object"  # хранить как строку
_PANDAS_TO_TDTP["decimal"] = "MONEY"
```

### Соглашения

- **J_* функции** не владеют передаваемыми `*C.char` — Go сам освобождает
  через `J_FreeString()`. Python не вызывает `free()` на аргументы, только
  на возвращаемые значения через `free_string()`.
- **D_* функции** выделяют память для `out`-параметров.
  Вызывающий код обязан вызвать `D_FreePacket(out)`.
- `D_FreePacket` — идемпотентен (вызов дважды безопасен).
- **Ошибки** возвращаются через `pkt.error[1024]` для D_* и
  через `{"error": "..."}` для J_*.

---

## Типичные проблемы

### `TDTPLibraryError: Cannot find libtdtp.so`

```bash
cd bindings/python
make build-lib      # или make build-lib-full
pip install -e .
```

### `TDTPProcessorError: unknown processor`

Библиотека собрана без `-tags compress`. Нужна полная сборка:
```bash
make build-lib-full
```

### `RuntimeError: D_Packet already freed`

Попытка использовать `PacketHandle` после `.free()`.
Проверьте порядок освобождения. Используйте context manager.

### Утечка памяти в D_*

Если пропущен вызов `free()`:
```python
# Всегда используйте try/finally или context manager
with client.D_read_ctx("file.tdtp") as pkt:
    ...  # free() гарантирован
```
