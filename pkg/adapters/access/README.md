# MS Access Adapter для TDTP Framework

Адаптер для экспорта данных из баз Microsoft Access (.mdb / .accdb) через 32-bit Jet 4.0 ODBC + ADOX.

> ⚠️ **Критические ограничения — прочти перед использованием:**
>
> | Ограничение | Причина |
> |-------------|---------|
> | **Только Windows** | `//go:build windows` — использует Win32 COM (ADOX), ODBC MDAC и `SysWOW64\cscript.exe` |
> | **Только x86 (32-bit)** | Microsoft Jet 4.0 ODBC — 32-bit компонент. Сборка обязательно с `GOARCH=386` |
>
> Бинарник `tdtpcli_x86.exe` нельзя запускать ни на Linux/macOS, ни как 64-bit процесс.

---

## 🔨 Сборка (PowerShell — одна строка)

```powershell
$env:GOPROXY="https://goproxy.io"; $env:GONOSUMDB="*"; $env:GOARCH="386"; go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/; $env:GOARCH=""
```

Или по шагам:
```powershell
$env:GOPROXY   = "https://goproxy.io"
$env:GONOSUMDB = "*"
$env:GOARCH    = "386"
go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/
$env:GOARCH    = ""   # сбросить после сборки!
```

> **Почему `-tags nokafka`?** Kafka-go тянет CGo-зависимости, несовместимые с `GOARCH=386`. Access-адаптер Kafka не нужен.

---

## ⚙️ Конфигурация

### Базовый формат DSN

```
Driver={Microsoft Access Driver (*.mdb, *.accdb)};DBQ=C:\path\to\db.mdb;UID=Admin;PWD=;
```

### Минимальный config.yaml

```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;UID=Admin;PWD=;"

export:
  compress: true
  compress_level: 3
```

### С паролем базы и workgroup (.mda/.mdw)

```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;SystemDB=C:\\SYSTEM.MDW;UID=sklad;PWD=secret;"
```

### С кодировкой Windows-1251 (старые русские базы)

```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;UID=Admin;PWD=;"
  charset: windows-1251
```

---

## 🚀 Использование

```powershell
# Экспорт таблицы в TDTP XML
.\tdtpcli_x86.exe --config access.yaml --export Товары --output товары.tdtp.xml

# Экспорт в XLSX
.\tdtpcli_x86.exe --config access.yaml --export-xlsx Товары --output товары.xlsx

# Список таблиц (требует прав на MSysObjects; если недоступно — указывай таблицу явно)
.\tdtpcli_x86.exe --config access.yaml --list

# Inspect (схема + статистика)
.\tdtpcli_x86.exe --config access.yaml --inspect Товары
```

---

## 🔍 Как работает определение типов

Jet ODBC не возвращает типы колонок через `DatabaseTypeName()` (всегда пусто).
Адаптер читает схему через **ADOX** — 32-bit COM-провайдер Windows:

```
Go (x86) → exec SysWOW64\cscript.exe → VBScript → ADOX.Catalog (Jet OLE DB 4.0)
                                                     ↓
                                              JSON [{"name":"..","type":"TEXT",...}]
                                                     ↓
                                              Go парсит → packet.Schema
```

1. Генерируется временный VBScript (`%TEMP%\tdtp-adox-*.vbs`)
2. Запускается через `C:\Windows\SysWOW64\cscript.exe` (32-bit хост)
3. Скрипт коннектится через `Microsoft.Jet.OLEDB.4.0` и читает `ADOX.Catalog`
4. Возвращает типы в JSON → Go строит `packet.Schema`

**Деградация:** если `cscript.exe` недоступен или ADOX не отвечает — автоматический fallback на sample-row inference (предупреждение в stderr, типы TEXT для NULL-колонок).

### Маппинг типов ADOX → TDTP

| Access / ADOX тип | Число | TDTP |
|-------------------|-------|------|
| AutoNumber, Long Integer | 3, 20 | INTEGER |
| Integer, Byte, SmallInt | 2, 16, 18, 19, 21 | INTEGER |
| Double, Single, Decimal, Numeric, Currency | 4, 5, 6, 14, 131 | REAL |
| Yes/No | 11 | BOOLEAN |
| Date/Time | 7, 64, 133, 134, 135 | DATETIME |
| OLE Object (BLOB) | 128, 204, 205 | BLOB |
| GUID | 72 | TEXT |
| Text, Memo и всё остальное | — | TEXT |

---

## 🔄 Конвертация старых форматов

Jet 4.0 не открывает базы Access 2.0 / 95 / 97. Конвертация через DAO (32-bit cscript):

**convert_mdb.vbs:**
```vbscript
Dim dao
Set dao = CreateObject("DAO.DBEngine.120")
dao.CompactDatabase "C:\path\OLD.MDB", "C:\path\NEW.MDB", _
    ";LANGID=0x0419;CP=1251;COUNTRY=0", 64, ";PWD=yourpassword"
WScript.Echo "Done"
```

```powershell
C:\Windows\SysWOW64\cscript.exe //nologo convert_mdb.vbs
```

---

## 📦 Зависимости (Windows-only, встроено в ОС)

| Компонент | Путь | Назначение |
|-----------|------|------------|
| `cscript.exe` (32-bit) | `C:\Windows\SysWOW64\cscript.exe` | Хост VBScript для ADOX |
| `ADOX.Catalog` | COM / MDAC | Чтение схемы БД |
| `Microsoft.Jet.OLEDB.4.0` | COM / MDAC 32-bit | Подключение к .mdb |
| `Microsoft Access Driver (*.mdb)` | ODBC 32-bit | Чтение строк данных |

Всё входит в состав Windows XP+ и **не требует дополнительной установки**.

---

## ⚡ Особенности и ограничения

| Операция | Поддерживается |
|----------|---------------|
| Экспорт таблиц | ✅ |
| TDTQL фильтрация | ✅ (SQL push-down) |
| Экспорт VIEW (Queries) | ✅ |
| Импорт данных | ❌ (read-only source) |
| Инкрементальный экспорт | ❌ |
| `--list` таблиц | ⚠️ Только если есть права на MSysObjects |
| Компрессия (zstd/kanzi) | ✅ |
| Compact format | ✅ |

---

## 📝 Совместимость

- ✅ Access 2000 / 2002 / 2003 (.mdb, Jet 4.0)
- ✅ Access 2007+ (.accdb, ACE через Jet ODBC драйвер)
- ⚠️ Access 97 и старше — нужна предварительная конвертация через DAO
- ❌ Linux / macOS — не поддерживается (Windows COM зависимости)

## 🔗 Ссылки

- [Документация: docs/ACCESS_ADAPTER.md](../../../docs/ACCESS_ADAPTER.md)
- [alexbrainman/odbc](https://github.com/alexbrainman/odbc)
- [TDTP Specification](../../../docs/TDTP_SPEC.md)
