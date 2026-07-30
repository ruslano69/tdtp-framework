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

## 🔨 Сборка для 32-битного ODBC

### Почему обязателен GOARCH=386

Microsoft Jet 4.0 ODBC — **32-битный** In-Process COM-сервер (`msjet40.dll`).
Windows не позволяет 64-битному процессу загрузить 32-битную DLL в своё адресное пространство.

Последствия попытки запустить x64-бинарник:

```
sql: unknown driver "odbc" — драйвер не зарегистрирован
```
или
```
Architecture mismatch: cannot load 32-bit DLL into 64-bit process
```

Единственное решение — собрать Go-бинарник как **32-битный** (`GOARCH=386`), чтобы он сам был
32-битным процессом и мог загружать Jet ODBC DLL напрямую.

> **Примечание.** Microsoft Access Database Engine 2016 Redistributable существует в x64-варианте
> и поддерживает `.accdb` и современные `.mdb` (Jet 4.0). Но для старых баз (Jet 2.x / 3.x)
> он не всегда работает. Jet 4.0 32-bit — универсальное решение для любого формата `.mdb`.

---

### 32-битный ODBC vs 64-битный ODBC на Windows

На Windows существуют **два независимых** диспетчера ODBC:

| | 64-bit ODBC | 32-bit ODBC |
|---|---|---|
| Утилита настройки | `C:\Windows\System32\odbcad32.exe` | `C:\Windows\SysWOW64\odbcad32.exe` |
| Реестр | `HKLM\SOFTWARE\ODBC` | `HKLM\SOFTWARE\WOW6432Node\ODBC` |
| Драйверы Access | ❌ нет (Jet только 32-bit) | ✅ есть |
| Используется | 64-bit процессами | 32-bit процессами |

Стандартный `odbcad32.exe` из `System32` — **64-битный**. Он не покажет драйверы Access.
Чтобы убедиться что 32-bit драйвер установлен, нужно запустить именно `SysWOW64\odbcad32.exe`.

Проверка из PowerShell:
```powershell
# 32-битные драйверы Access
Get-ItemProperty "HKLM:\SOFTWARE\WOW6432Node\ODBC\ODBCINST.INI\ODBC Drivers" |
    Select-Object -Property * | Where-Object { $_ -match "Access" }
```

Или из Python:
```python
import winreg
key = winreg.OpenKey(winreg.HKEY_LOCAL_MACHINE,
    r"SOFTWARE\WOW6432Node\ODBC\ODBCINST.INI\ODBC Drivers")
i = 0
while True:
    try:
        name, _, _ = winreg.EnumValue(key, i); print(name); i += 1
    except OSError:
        break
```

---

### Сборка (PowerShell — одна строка)

```powershell
$env:GOPROXY="https://goproxy.io"; $env:GONOSUMDB="*"; $env:GOARCH="386"; go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/; $env:GOARCH=""
```

По шагам:
```powershell
$env:GOPROXY   = "https://goproxy.io"   # прямой прокси, без googleapis redirect
$env:GONOSUMDB = "*"                    # отключить sum-проверку для старых псевдоверсий
$env:GOARCH    = "386"                  # цель: 32-bit x86
go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/
$env:GOARCH    = ""                     # сбросить, иначе все следующие сборки будут x86
```

> **`-tags nokafka`** — kafka-go тянет CGo-зависимости, несовместимые с `GOARCH=386`.
> Access-адаптеру Kafka не нужна, тег безопасно исключает её из сборки.

---

### Почему GOPROXY=goproxy.io, а не proxy.golang.org

`proxy.golang.org` перенаправляет скачивание модулей на `storage.googleapis.com`.
Если в окружении прописан `no_proxy=*.googleapis.com`, скачивание падает с 403/timeout.

`goproxy.io` отдаёт модули напрямую без редиректов — работает даже в закрытых сетях.

Альтернативная цепочка (если goproxy.io не доступен):
```powershell
$env:GOPROXY = "https://goproxy.cn,https://goproxy.io,direct"
```

---

### Регистрация адаптера в бинарнике

Access-адаптер регистрируется через `init()` по blank-импорту. Файл использует build tag
`//go:build windows`, поэтому на Linux/macOS он автоматически исключается из компиляции:

```go
// cmd/tdtpcli/drivers_access.go
//go:build windows

package main

import _ "github.com/ruslano69/tdtp-framework/pkg/adapters/access"
```

Без этого файла в бинарнике `access` не появится в списке адаптеров — `--list` вернёт
`unknown database type: access`.

---

### Схема работы 32-битного стека

```
tdtpcli_x86.exe (32-bit Go процесс)
       │
       │  database/sql  →  odbc driver (alexbrainman/odbc)
       │                       │
       │                       │  ODBC API (Unicode: SQLConnectW, SQLExecDirectW)
       │                       ▼
       │              msjet40.dll  (Jet 4.0, 32-bit COM, In-Process)
       │                       │
       │                       ▼
       │              DELO26.MDB  (Jet 2.x/3.x/4.x формат)
       │
       │  Schema introspection (ADOX)
       │       │
       │       │  os/exec  →  C:\Windows\SysWOW64\cscript.exe (32-bit)
       │       │                       │
       │       │               VBScript  →  ADOX.Catalog  →  Jet OLE DB 4.0
       │       │                       │
       │       └──────── JSON схема ◄──┘
       │
       ▼
  TDTP XML (UTF-8, XML-escaped, windows-1251 → UTF-8 если charset задан)
```

`alexbrainman/odbc` использует Unicode ODBC API (`SQL_C_WCHAR`) — имена колонок всегда
приходят как UTF-16 и конвертируются в UTF-8 автоматически. Данные из Jet 2.x могут
приходить как ANSI-байты (Windows-1251) — для них нужен параметр `charset: windows-1251`
в конфиге, который активирует побайтовую конвертацию через `charmap.Windows1251`.

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
