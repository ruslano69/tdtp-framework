# pkg/python/libtdtp — документация разработчика

Разделяемая библиотека (CGo `c-shared`), экспортирующая TDTP-ядро для использования
из Python (ctypes) и любого другого языка с FFI-поддержкой C ABI.

---

## Содержание

1. [Структура пакета](#структура-пакета)
2. [Сборка](#сборка)
3. [Два семейства API](#два-семейства-api)
4. [Управление памятью](#управление-памятью)
5. [Формат ответов J_*](#формат-ответов-j_)
6. [Справочник функций J_*](#справочник-функций-j_)
7. [Справочник функций D_*](#справочник-функций-d_)
8. [C-структуры (D_* API)](#c-структуры-d_-api)
9. [Типы данных TDTP](#типы-данных-tdtp)
10. [Написание нового адаптера](#написание-нового-адаптера)

---

## Структура пакета

```
pkg/python/libtdtp/
├── main.go                      # пустой main() — обязателен для c-shared
├── exports_j.go                 # J_* функции: I/O, фильтрация, Diff
├── exports_j_serialize.go       # J_SerializeValue — сериализация типов
├── exports_j_compress.go        # J_* compress/decompress  (build tag: compress)
├── exports_j_compress_stub.go   # заглушки для сборки без тега compress
├── exports_d.go                 # D_* функции: прямой доступ без JSON
├── exports_d_compress.go        # D_* compress/decompress  (build tag: compress)
├── exports_d_compress_stub.go   # заглушки для сборки без тега compress
├── tdtp_structs.h               # C-определения структур D_Packet, D_Field, …
├── go.mod / go.sum              # модуль; replace → ../../../  (корень репо)
└── libtdtp                      # скомпилированный .so (в .gitignore)
```

---

## Сборка

```bash
# Базовая сборка (без zstd-процессоров)
cd bindings/python
make build-lib

# Полная сборка (zstd + J_ApplyProcessor + J_ApplyChain)
make build-lib-full

# Эквивалентные команды вручную
cd pkg/python/libtdtp
go build -buildmode=c-shared -o /path/to/libtdtp.so .
go build -tags compress -buildmode=c-shared -o /path/to/libtdtp.so .
```

`make build-lib` копирует `.so` и `.h` в `bindings/python/tdtp/` — стандартное
место для Python-биндинга.

---

## Два семейства API

| | **J_\*** (JSON boundary) | **D_\*** (Direct boundary) |
|---|---|---|
| Граница данных | JSON-строки (`*C.char`) | C-структуры (`D_Packet*`) |
| Простота использования | высокая | требует управления памятью |
| Производительность | небольшой overhead на marshal/unmarshal | максимальная |
| Освобождение памяти | `J_FreeString(ptr)` | `D_FreePacket(&pkt)` |
| Обнаружение ошибок | поле `"error"` в JSON | возврат `1`, поле `pkt.error` |
| Рекомендуется для | Python/ctypes адаптеры | hot-path, C/C++ |

---

## Управление памятью

### J_* — каждая функция возвращает `*C.char`

```
┌──────────────────────────────────────────────────────────────────┐
│  ПРАВИЛО: каждый ненулевой указатель, возвращённый J_*,          │
│  необходимо освободить вызовом J_FreeString(ptr).                │
│  Не делайте этого — утечёт память Go heap.                       │
└──────────────────────────────────────────────────────────────────┘
```

Пример Python (ctypes):
```python
ptr = lib.J_ReadFile(b"/data/users.tdtp.xml")
try:
    raw = ctypes.string_at(ptr)          # копируем в Python bytes
    result = json.loads(raw)
finally:
    lib.J_FreeString(ptr)                # обязательно
```

`J_FreeString(NULL)` безопасен (no-op).

### D_* — Go выделяет память через `C.malloc`

```
┌──────────────────────────────────────────────────────────────────┐
│  ПРАВИЛО: для каждого D_Packet, заполненного Go (D_ReadFile,     │
│  D_FilterRows, D_ApplyMask), вызовите D_FreePacket(&pkt).        │
│  D_Packet, переданный в Go (входной), НЕ освобождается Go —      │
│  Python владеет им.                                              │
└──────────────────────────────────────────────────────────────────┘
```

`D_FreeMaskConfig` существует для симметрии API, но является no-op: `D_MaskConfig`
полностью владеет Python-ctypes.

---

## Формат ответов J_*

### Успех

Большинство функций возвращает `jPacket`-структуру:

```json
{
  "schema": {
    "fields": [
      {"name": "ID",    "type": "INTEGER", "is_key": true},
      {"name": "Name",  "type": "TEXT"},
      {"name": "Score", "type": "DECIMAL", "precision": 18, "scale": 2}
    ]
  },
  "header": {
    "type":        "DATA",
    "table_name":  "Users",
    "message_id":  "abc-123",
    "timestamp":   "2025-11-10T15:30:00Z"
  },
  "data": [
    ["1", "Alice", "9.50"],
    ["2", "Bob",   "7.20"]
  ]
}
```

### Ошибка

```json
{"error": "parse error: unexpected token at line 42"}
```

Проверка в Python:
```python
result = json.loads(ctypes.string_at(ptr))
if "error" in result:
    raise TDTPError(result["error"])
```

### J_SerializeValue — отдельный конверт

```json
{"value": "AP/erv/6=="}   // успех
{"error": "BLOB: invalid hex input: ..."}   // ошибка
```

### J_ExportAll

```json
{"files": ["/tmp/Users_part_1_of_2.tdtp.xml", "/tmp/Users_part_2_of_2.tdtp.xml"],
 "total_parts": 2}
```

---

## Справочник функций J_*

### Управление памятью

#### `J_FreeString(ptr *C.char)`

Освобождает строку, возвращённую любой J_* функцией. Обязателен для каждого
ненулевого указателя. Принимает NULL — no-op.

---

#### `J_GetVersion() *C.char`

Возвращает версию библиотеки как простую C-строку (не JSON).

```python
ptr = lib.J_GetVersion()
version = ctypes.string_at(ptr).decode()   # "1.6.0"
lib.J_FreeString(ptr)
```

---

### I/O

#### `J_ReadFile(path *C.char) *C.char`

Читает `.tdtp.xml` файл. Автоматически разархивирует zstd-сжатые данные
(требует build tag `compress`).

```python
ptr = lib.J_ReadFile(b"/data/users.tdtp.xml")
pkt = json.loads(ctypes.string_at(ptr))
lib.J_FreeString(ptr)
# pkt["schema"], pkt["header"], pkt["data"]
```

**Ошибки:** файл не найден, ошибка парсинга, повреждённый чексум.

---

#### `J_WriteFile(dataJSON *C.char, path *C.char) *C.char`

Записывает `jPacket`-JSON в файл. Возвращает `{"ok": true}`.

```python
payload = json.dumps({"schema": ..., "header": ..., "data": ...}).encode()
ptr = lib.J_WriteFile(payload, b"/out/result.tdtp.xml")
result = json.loads(ctypes.string_at(ptr))
lib.J_FreeString(ptr)
```

---

#### `J_ExportAll(dataJSON, basePath, optionsJSON *C.char) *C.char`

Разбивает датасет на части по байтовому лимиту (идентично `tdtpcli`),
опционально сжимает, записывает все части.

```python
options = json.dumps({"compress": True, "level": 3, "checksum": True}).encode()
ptr = lib.J_ExportAll(payload, b"/out/Users.tdtp.xml", options)
result = json.loads(ctypes.string_at(ptr))
lib.J_FreeString(ptr)
# result["files"]       → список созданных файлов
# result["total_parts"] → количество частей
```

| Поле optionsJSON | Тип | По умолчанию | Описание |
|---|---|---|---|
| `compress` | bool | `false` | Сжать zstd |
| `level` | int | `3` | Уровень сжатия (1–22) |
| `checksum` | bool | `true` | XXH3 чексум |

Имена частей: `Users_part_1_of_3.tdtp.xml`, `Users_part_2_of_3.tdtp.xml`, …
Если часть одна — имя файла не изменяется.

---

### Фильтрация (TDTQL)

#### `J_FilterRows(dataJSON, whereClause *C.char, limit C.int) *C.char`

Применяет TDTQL WHERE-выражение. Возвращает `jPacket` с отфильтрованными
строками.

```python
where = b"Balance > 1000 AND City = 'Omsk'"
ptr = lib.J_FilterRows(payload, where, ctypes.c_int(0))  # 0 = без лимита
result = json.loads(ctypes.string_at(ptr))
lib.J_FreeString(ptr)
```

Операторы: `=`, `!=`, `<`, `<=`, `>`, `>=`, `LIKE`, `IN`, `BETWEEN`, `IS NULL`,
`IS NOT NULL`. Логика: `AND`, `OR`, `NOT`, скобки.

---

#### `J_FilterRowsPage(dataJSON, whereClause *C.char, limit, offset C.int) *C.char`

Фильтрация с пагинацией. Возвращает `jPacket` + поле `query_context`:

```json
{
  "schema": {...}, "header": {...}, "data": [...],
  "query_context": {
    "total_records":    1000,
    "matched_records":  47,
    "returned_records": 10,
    "more_available":   true,
    "next_offset":      10,
    "limit":            10,
    "offset":           0
  }
}
```

Пагинация без состояния: каждый вызов независим. Следующая страница:
```python
offset += result["query_context"]["returned_records"]
# повторить J_FilterRowsPage с новым offset
```

---

### Процессоры (только с тегом `compress`)

#### `J_ApplyProcessor(dataJSON, procType, configJSON *C.char) *C.char`

| `procType` | Описание | Основные параметры |
|---|---|---|
| `"field_masker"` | Маскировка полей | `fields`, `mask_char`, `visible_chars` |
| `"field_normalizer"` | Нормализация значений | `field`, `rules` |
| `"field_validator"` | Валидация | `field`, `rules` |
| `"compress"` | zstd-сжатие | `level` (1–22) |
| `"decompress"` | Распаковка zstd | — |

```python
config = json.dumps({"fields": ["Phone", "Email"], "visible_chars": 4}).encode()
ptr = lib.J_ApplyProcessor(payload, b"field_masker", config)
```

---

#### `J_ApplyChain(dataJSON, chainJSON *C.char) *C.char`

Цепочка процессоров — выполняются последовательно:

```python
chain = json.dumps([
    {"type": "field_normalizer", "params": {"field": "Name", "rules": ["trim", "upper"]}},
    {"type": "field_masker",     "params": {"fields": ["Phone"], "visible_chars": 4}},
]).encode()
ptr = lib.J_ApplyChain(payload, chain)
```

---

### Diff

#### `J_Diff(oldJSON, newJSON *C.char) *C.char`

Сравнивает два датасета по ключевым полям схемы.

```json
{
  "added":    [["5", "Eve",   "8.00"]],
  "removed":  [["3", "Carol", "6.50"]],
  "modified": [
    {
      "key": "2",
      "old_row": ["2", "Bob", "7.20"],
      "new_row": ["2", "Bob", "7.50"],
      "changes": {
        "2": {"field_name": "Score", "old_value": "7.20", "new_value": "7.50"}
      }
    }
  ],
  "stats": {
    "total_in_a": 4, "total_in_b": 4,
    "added": 1, "removed": 1, "modified": 1, "unchanged": 2
  }
}
```

---

### Сериализация типов

#### `J_SerializeValue(tdtpType, value *C.char) *C.char`

**Единственный источник правды** для преобразования Python-значений в TDTP
wire-строки. Все адаптеры (pandas, xlsx, arrow, …) обязаны делегировать сюда
вместо самостоятельной реализации конвертации.

| `tdtpType` | Ожидаемый `value` | Возвращает |
|---|---|---|
| `"BLOB"` | Hex-строка байт, напр. `"deadbeef"` | Base64 (`StdEncoding`) |
| `"TIMESTAMP"` / `"DATETIME"` | ISO-8601 строка (любой формат) | UTC RFC3339, напр. `"2025-11-12T06:15:00Z"` |
| `"JSON"` / `"JSONB"` | Валидный JSON-строка | Compact JSON (без пробелов) |
| Любой другой | Любая строка | Возвращает `value` без изменений |

**Парсинг дат** — поддерживаемые форматы (в порядке приоритета):
```
2006-01-02T15:04:05Z07:00  ← RFC3339 (tz-aware)
2006-01-02T15:04:05         ← ISO-8601 naive (трактуется как UTC)
2006-01-02 15:04:05         ← SQLite / MSSQL без T
2006-01-02                  ← только дата
```

**Нормализация временных зон:** все результаты нормализуются в UTC.
`2025-11-12T09:15:00+03:00` → `"2025-11-12T06:15:00Z"`.

Пример из `pandas_ext.py`:
```python
# bytes → hex → Go → Base64
_go_serialize("BLOB", bytes_value.hex())

# datetime → isoformat → Go → UTC RFC3339
_go_serialize("TIMESTAMP", dt.replace(microsecond=0).isoformat())

# dict → json.dumps → Go → compact JSON
_go_serialize("JSON", json.dumps(obj, ensure_ascii=False))
```

Ответ: `{"value": "..."}` / `{"error": "..."}`.

---

## Справочник функций D_*

#### `D_ReadFile(path *C.char, out *D_Packet) C.int`

Читает файл, заполняет `out`. Возврат: `0` = успех, `1` = ошибка.
При ошибке `out.error` содержит сообщение.
**Вызывающий должен освободить `out` через `D_FreePacket`.**

```python
out = D_Packet()
rc  = lib.D_ReadFile(b"/data/users.tdtp.xml", ctypes.byref(out))
if rc != 0:
    raise TDTPError(out.error.decode())
try:
    # работаем с out.rows, out.schema, out.msg_type, …
finally:
    lib.D_FreePacket(ctypes.byref(out))
```

---

#### `D_WriteFile(pkt *D_Packet, path *C.char) C.int`

Записывает `pkt` в файл. Возврат: `0` = успех, `1` = ошибка.
Входной `pkt` принадлежит вызывающему; Go его не освобождает.

---

#### `D_FilterRows(pkt, filters *D_FilterSpec, count, limit C.int, out *D_Packet) C.int`

Фильтрует строки `pkt` по массиву `D_FilterSpec` (логика AND).
`limit=0` — без ограничений.
**`out` нужно освободить через `D_FreePacket`.**

```python
f = D_FilterSpec()
f.field[:5] = b"Score"
f.op[:1]    = b">"
f.value[:3] = b"5.0"

out = D_Packet()
rc  = lib.D_FilterRows(
    ctypes.byref(src), ctypes.byref(f),
    ctypes.c_int(1), ctypes.c_int(0),
    ctypes.byref(out),
)
```

---

#### `D_ApplyMask(pkt, cfg *D_MaskConfig, out *D_Packet) C.int`

Маскирует поля из `cfg.fields`. **`out` нужно освободить через `D_FreePacket`.**
`cfg` остаётся во владении Python до возврата функции.

---

#### `D_ApplyCompress(pkt *D_Packet, level C.int, out *D_Packet) C.int`

*(только с тегом `compress`)* Сжимает данные zstd.

---

#### `D_ApplyDecompress(pkt, out *D_Packet) C.int`

*(только с тегом `compress`)* Распаковывает zstd-данные.

---

#### `D_FreePacket(pkt *D_Packet)`

Освобождает все `C.malloc`-буферы внутри `pkt` (строки значений, массив полей
схемы). Безопасен для пустого/нулевого пакета.

---

#### `D_FreeMaskConfig(cfg *D_MaskConfig)`

No-op. Существует для симметрии API.

---

## C-структуры (D_* API)

```c
// Одно поле схемы
typedef struct {
    char name[256];      // имя поля
    char type_name[64];  // "INTEGER", "TEXT", "BLOB", …
    int  length;
    int  precision;
    int  scale;
    int  is_key;         // 1 = ключевое поле
    int  is_readonly;    // 1 = только чтение
} D_Field;

// Схема пакета
typedef struct {
    D_Field* fields;
    int      field_count;
} D_Schema;

// Одна строка данных
typedef struct {
    char** values;
    int    value_count;
} D_Row;

// Полный пакет данных
typedef struct {
    D_Row*    rows;
    int       row_count;
    D_Schema  schema;
    char      msg_type[32];      // "DATA", "ACK", …
    char      table_name[256];
    char      message_id[64];
    long long timestamp_unix;    // Unix-секунды
    char      compression[16];   // "" или "zstd"
    char      error[1024];       // ошибка при rc==1
} D_Packet;

// Условие фильтра
typedef struct {
    char field[256];
    char op[32];         // "=", ">", "LIKE", "BETWEEN", …
    char value[1024];
    char value2[1024];   // для BETWEEN: верхняя граница
} D_FilterSpec;

// Конфигурация маскировки
typedef struct {
    char** fields;       // Python владеет массивом
    int    field_count;
    char   mask_char[4]; // символ замены, по умолч. "*"
    int    visible_chars;// кол-во незаменяемых символов справа
} D_MaskConfig;
```

---

## Типы данных TDTP

| Тип | Псевдонимы | Wire-формат |
|---|---|---|
| `INTEGER` | `INT` | строка числа, напр. `"42"` |
| `REAL` | `FLOAT`, `DOUBLE` | строка числа с точкой, напр. `"3.14"` |
| `DECIMAL` | — | строка с фиксированной точностью, напр. `"9.50"` |
| `TEXT` | `VARCHAR`, `CHAR`, `STRING` | UTF-8 строка |
| `BOOLEAN` | `BOOL` | `"true"` / `"false"` (нижний регистр) |
| `DATE` | — | `"2025-11-10"` |
| `DATETIME` | — | UTC RFC3339: `"2025-11-10T15:30:00Z"` |
| `TIMESTAMP` | — | UTC RFC3339: `"2025-11-10T15:30:00Z"` |
| `BLOB` | — | Base64 StdEncoding: `"AP/erv/6=="` |

Псевдонимы нормализуются функцией `schema.NormalizeType`:
`INT→INTEGER`, `FLOAT/DOUBLE→REAL`, `VARCHAR/CHAR/STRING→TEXT`, `BOOL→BOOLEAN`.

---

## Написание нового адаптера

Новый адаптер (xlsx, arrow, parquet, …) должен:

### 1. Сконвертировать данные во входной `jPacket`-JSON

```python
payload = json.dumps({
    "schema": {
        "fields": [
            {"name": "ID",   "type": "INTEGER", "is_key": True},
            {"name": "Name", "type": "TEXT"},
            {"name": "Data", "type": "BLOB"},
        ]
    },
    "header": {
        "type":       "DATA",
        "table_name": "MyTable",
        "message_id": str(uuid.uuid4()),
        "timestamp":  datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"),
    },
    "data": serialized_rows,  # [["1", "Alice", "AP/erv/6=="], ...]
}).encode()
```

### 2. Делегировать сериализацию сложных типов в `_go_serialize`

```python
from tdtp._loader import lib, free_string
import ctypes, json

def _go_serialize(tdtp_type: str, value: str) -> str:
    ptr = lib.J_SerializeValue(tdtp_type.encode(), value.encode())
    raw = ctypes.string_at(ptr)
    free_string(ptr)
    result = json.loads(raw)
    if "error" in result:
        raise ValueError(result["error"])
    return result["value"]

# bytes → BLOB
_go_serialize("BLOB", my_bytes.hex())

# datetime → TIMESTAMP
_go_serialize("TIMESTAMP", my_datetime.isoformat())

# dict/list → JSON
_go_serialize("JSON", json.dumps(my_dict))
```

**Не реализовывать** Base64-кодирование, форматирование RFC3339 и нормализацию
JSON самостоятельно — это обязанность Go.

### 3. Настроить ctypes-сигнатуры в `_loader.py`

Все J_* функции используют единый паттерн:
```python
lib.J_SomeFn.argtypes = [ctypes.c_char_p, ...]
lib.J_SomeFn.restype  = ctypes.c_void_p   # НЕ c_char_p — теряется адрес
```

`restype = c_void_p` сохраняет адрес для последующего `J_FreeString`.
`c_char_p` автоматически конвертируется в Python bytes — адрес теряется.

### 4. Освобождать все указатели

```python
ptr = lib.J_SomeFn(...)
try:
    data = json.loads(ctypes.string_at(ptr))
finally:
    free_string(ptr)   # из tdtp._loader
```

---

## Журнал изменений API

| Версия | Изменение |
|---|---|
| 1.6.0 | `J_SerializeValue` — Go как единый источник правды для сериализации |
| 1.5.x | `J_FilterRowsPage` — пагинация с `query_context` |
| 1.4.x | `J_ExportAll` — партиционирование + опциональный zstd |
| 1.3.x | `J_ApplyChain` — цепочки процессоров |
| 1.2.x | `D_*` семейство — прямой доступ без JSON |
