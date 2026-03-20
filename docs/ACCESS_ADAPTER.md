# MS Access Adapter

Адаптер для экспорта данных из баз Microsoft Access (.mdb, .accdb) через 32-bit Jet 4.0 ODBC.

## Ограничения

- **Только Windows** (`//go:build windows`)
- **Только 32-bit** — Jet 4.0 ODBC доступен только в 32-bit (`GOARCH=386`)
- Только экспорт; инкрементальный экспорт не поддерживается
- `--list` не работает (нет прав на `MSysObjects`); указывай таблицу явно

## Сборка (PowerShell)

```powershell
$env:GOPROXY="https://goproxy.io"
$env:GONOSUMDB="*"
$env:GOARCH="386"
go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/
$env:GOARCH=""   # сбросить после сборки
```

Быстро в одну строку:
```powershell
$env:GOPROXY="https://goproxy.io"; $env:GONOSUMDB="*"; $env:GOARCH="386"; go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/; $env:GOARCH=""
```

## Конфиг

```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;UID=Admin;PWD=;"

export:
  compress: true
  compress_level: 3
```

С паролем базы и системным файлом (user-level security):
```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;SystemDB=C:\\path\\to\\SYSTEM.MDW;UID=sklad;PWD=secret;"
```

С кодировкой Windows-1251 (если данные не в UTF-8):
```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;UID=Admin;PWD=;"
  charset: windows-1251
```

## Команды

```powershell
# Экспорт таблицы в TDTP XML (multi-part)
.\tdtpcli_x86.exe --config access.yaml --export Товары --output товары.tdtp.xml

# Экспорт в XLSX (все строки, один файл)
.\tdtpcli_x86.exe --config access.yaml --export-xlsx Товары --output товары.xlsx

# Список таблиц (не работает без прав на MSysObjects — укажи таблицу явно)
.\tdtpcli_x86.exe --config access.yaml --list
```

## Конвертация старых форматов

Jet 4.0 не открывает базы Access 2.0 / 95 / 97. Конвертация через DAO (32-bit cscript):

**convert_mdb.vbs** — конвертировать формат:
```vbscript
Dim dao, src, dst
src = "C:\path\DELO19.MDB"
dst = "C:\path\DELO19_2003.MDB"
Set dao = CreateObject("DAO.DBEngine.120")
dao.CompactDatabase src, dst, ";LANGID=0x0419;CP=1251;COUNTRY=0", 64, ";PWD=yourpassword"
WScript.Echo "Done: " & dst
```

Запуск (обязательно 32-bit cscript):
```powershell
C:\Windows\SysWOW64\cscript.exe //nologo convert_mdb.vbs
```

**Убрать пароль базы** (компактировать с пустым паролем назначения):
```vbscript
dao.CompactDatabase src, dst, ";LANGID=0x0419;CP=1251;COUNTRY=0;PWD=", 64, ";PWD=yourpassword"
```

**User-level security** (workgroup .mda/.mdw):
После компактирования security сохраняется. Чтобы убрать — открыть базу от имени admin-пользователя и выдать права через DAO или ADOX перед компактированием без SystemDB.

## Как работает определение типов

Jet ODBC не возвращает типы колонок через стандартный `DatabaseTypeName()` (всегда пусто).
Адаптер читает схему через **ADOX** — встроенный COM-провайдер Windows:

1. Генерируется временный VBScript (`%TEMP%\tdtp-adox-*.vbs`)
2. Запускается через `C:\Windows\SysWOW64\cscript.exe` (32-bit)
3. Скрипт коннектится через `Microsoft.Jet.OLEDB.4.0` и читает `ADOX.Catalog`
4. Возвращает типы колонок в JSON → Go парсит и строит схему

Если `cscript.exe` недоступен — деградация до sample-row inference с предупреждением в stderr.

### Маппинг типов ADOX → TDTP

| ADOX тип | Число | TDTP |
|----------|-------|------|
| adInteger, adBigInt | 3, 20 | INTEGER |
| adSmallInt, adTinyInt, adUnsignedSmallInt | 2, 16, 18 | INTEGER |
| adDouble, adSingle, adNumeric, adDecimal, adVarNumeric | 5, 4, 131, 14, 139 | REAL |
| adCurrency | 6 | REAL |
| adBoolean | 11 | BOOLEAN |
| adDate, adDBDate, adDBTime, adDBTimeStamp | 7, 133, 134, 135 | DATETIME |
| adLongVarBinary, adBinary, adVarBinary | 205, 128, 204 | BLOB |
| adGUID | 72 | TEXT |
| всё остальное (adVarWChar 202, adLongVarWChar 203 и др.) | — | TEXT |

## Зависимости (только Windows, встроено)

| Компонент | Путь | Назначение |
|-----------|------|------------|
| `cscript.exe` | `C:\Windows\SysWOW64\cscript.exe` | Хост VBScript (32-bit) |
| `ADOX.Catalog` | COM (MDAC) | Чтение схемы БД |
| `Microsoft.Jet.OLEDB.4.0` | COM (MDAC 32-bit) | Подключение к .mdb |
| `Microsoft Access Driver (*.mdb)` | ODBC 32-bit | Чтение данных |

Всё входит в состав Windows XP+ и не требует дополнительной установки.
