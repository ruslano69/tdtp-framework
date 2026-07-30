# MS Access adapter

Exports data from Microsoft Access databases (`.mdb`, `.accdb`) through the 32-bit Jet 4.0 ODBC driver and ADOX.

> **Hard limits — read these first:**
>
> | Limit | Why |
> |-------------|---------|
> | **Windows only** | `//go:build windows` — it uses Win32 COM (ADOX), ODBC MDAC and `SysWOW64\cscript.exe` |
> | **x86 (32-bit) only** | Microsoft Jet 4.0 ODBC is a 32-bit component, so the build needs `GOARCH=386` |
>
> `tdtpcli_x86.exe` will not run on Linux or macOS, nor as a 64-bit process.

---

## Building for the 32-bit ODBC driver

### Why GOARCH=386 is not optional

Microsoft Jet 4.0 ODBC is a **32-bit** in-process COM server (`msjet40.dll`).
Windows will not let a 64-bit process load a 32-bit DLL into its address space.

What happens if you try it with an x64 binary:

```
sql: unknown driver "odbc" — the driver is not registered
```
or
```
Architecture mismatch: cannot load 32-bit DLL into 64-bit process
```

The only fix is to build the Go binary as **32-bit** (`GOARCH=386`), so that it is itself a
32-bit process and can load the Jet ODBC DLL directly.

> **A note.** Microsoft Access Database Engine 2016 Redistributable does exist as x64
> and handles `.accdb` and modern `.mdb` (Jet 4.0). But for older databases (Jet 2.x, 3.x)
> it does not always work. Jet 4.0 32-bit handles every `.mdb` format there is.

---

### 32-bit versus 64-bit ODBC on Windows

Windows has **two independent** ODBC managers:

| | 64-bit ODBC | 32-bit ODBC |
|---|---|---|
| Configuration tool | `C:\Windows\System32\odbcad32.exe` | `C:\Windows\SysWOW64\odbcad32.exe` |
| Registry | `HKLM\SOFTWARE\ODBC` | `HKLM\SOFTWARE\WOW6432Node\ODBC` |
| Access drivers | absent — Jet is 32-bit only | present |
| Used by | 64-bit processes | 32-bit processes |

The `odbcad32.exe` in `System32` is the **64-bit** one, and it will not show the Access drivers.
To confirm the 32-bit driver is installed, run `SysWOW64\odbcad32.exe` specifically.

From PowerShell:
```powershell
# The 32-bit Access drivers
Get-ItemProperty "HKLM:\SOFTWARE\WOW6432Node\ODBC\ODBCINST.INI\ODBC Drivers" |
    Select-Object -Property * | Where-Object { $_ -match "Access" }
```

Or from Python:
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

### Building it (PowerShell, one line)

```powershell
$env:GOPROXY="https://goproxy.io"; $env:GONOSUMDB="*"; $env:GOARCH="386"; go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/; $env:GOARCH=""
```

Step by step:
```powershell
$env:GOPROXY   = "https://goproxy.io"   # a direct proxy, with no googleapis redirect
$env:GONOSUMDB = "*"                    # skip the sum check for old pseudo-versions
$env:GOARCH    = "386"                  # target: 32-bit x86
go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/
$env:GOARCH    = ""                     # reset it, or every later build is x86 too
```

> **`-tags nokafka`** — kafka-go pulls in cgo dependencies that do not work with `GOARCH=386`.
> The Access adapter has no use for Kafka, and the tag leaves it out safely.

---

### Why GOPROXY=goproxy.io rather than proxy.golang.org

`proxy.golang.org` redirects module downloads to `storage.googleapis.com`.
Where the environment sets `no_proxy=*.googleapis.com`, that download fails with a 403 or a timeout.

`goproxy.io` serves modules directly with no redirect, and works even on a closed network.

An alternative chain, if goproxy.io is unreachable:
```powershell
$env:GOPROXY = "https://goproxy.cn,https://goproxy.io,direct"
```

---

### Registering the adapter in the binary

The Access adapter registers itself in `init()`, via a blank import. The file carries the build tag
`//go:build windows`, so on Linux and macOS it is excluded from the build automatically:

```go
// cmd/tdtpcli/drivers_access.go
//go:build windows

package main

import _ "github.com/ruslano69/tdtp-framework/pkg/adapters/access"
```

Without that file `access` never appears in the adapter list, and `--list` returns
`unknown database type: access`.

---

### How the 32-bit stack fits together

```
tdtpcli_x86.exe (a 32-bit Go process)
       │
       │  database/sql  →  odbc driver (alexbrainman/odbc)
       │                       │
       │                       │  ODBC API (Unicode: SQLConnectW, SQLExecDirectW)
       │                       ▼
       │              msjet40.dll  (Jet 4.0, 32-bit COM, In-Process)
       │                       │
       │                       ▼
       │              DELO26.MDB  (Jet 2.x/3.x/4.x format)
       │
       │  Schema introspection (ADOX)
       │       │
       │       │  os/exec  →  C:\Windows\SysWOW64\cscript.exe (32-bit)
       │       │                       │
       │       │               VBScript  →  ADOX.Catalog  →  Jet OLE DB 4.0
       │       │                       │
       │       └──────── JSON schema ◄──┘
       │
       ▼
  TDTP XML (UTF-8, XML-escaped; windows-1251 → UTF-8 when charset is set)
```

`alexbrainman/odbc` uses the Unicode ODBC API (`SQL_C_WCHAR`), so column names always
arrive as UTF-16 and are converted to UTF-8 for you. Data out of Jet 2.x may
arrive as ANSI bytes (Windows-1251); those need `charset: windows-1251`
in the config, which turns on byte-wise conversion through `charmap.Windows1251`.

---

## Configuration

### The DSN

```
Driver={Microsoft Access Driver (*.mdb, *.accdb)};DBQ=C:\path\to\db.mdb;UID=Admin;PWD=;
```

### A minimal config.yaml

```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;UID=Admin;PWD=;"

export:
  compress: true
  compress_level: 3
```

### With a database password and a workgroup file (.mda/.mdw)

```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;SystemDB=C:\\SYSTEM.MDW;UID=sklad;PWD=secret;"
```

### With Windows-1251 text (older Russian databases)

```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;UID=Admin;PWD=;"
  charset: windows-1251
```

---

## Usage

```powershell
# Export a table to TDTP XML
.\tdtpcli_x86.exe --config access.yaml --export Товары --output tovary.tdtp.xml

# Export to XLSX
.\tdtpcli_x86.exe --config access.yaml --export-xlsx Товары --output tovary.xlsx

# Listing tables needs rights on MSysObjects; without them, name the table explicitly
.\tdtpcli_x86.exe --config access.yaml --list

# Inspect — schema and statistics
.\tdtpcli_x86.exe --config access.yaml --inspect Товары
```

---

## How column types are determined

Jet's ODBC driver does not report column types through `DatabaseTypeName()` — it always returns empty.
The adapter reads the schema through **ADOX**, the 32-bit COM provider built into Windows:

```
Go (x86) → exec SysWOW64\cscript.exe → VBScript → ADOX.Catalog (Jet OLE DB 4.0)
                                                     ↓
                                              JSON [{"name":"..","type":"TEXT",...}]
                                                     ↓
                                              Go parses it → packet.Schema
```

1. a temporary VBScript is generated at `%TEMP%\tdtp-adox-*.vbs`
2. it runs under `C:\Windows\SysWOW64\cscript.exe`, the 32-bit host
3. the script connects through `Microsoft.Jet.OLEDB.4.0` and reads `ADOX.Catalog`
4. it returns the types as JSON, and Go builds a `packet.Schema`

**Degradation:** if `cscript.exe` is unavailable or ADOX does not answer, the adapter falls back to inferring types from a sample row — a warning on stderr, and TEXT for any column that was all NULL.

### ADOX to TDTP type mapping

| Access / ADOX type | Number | TDTP |
|-------------------|-------|------|
| AutoNumber, Long Integer | 3, 20 | INTEGER |
| Integer, Byte, SmallInt | 2, 16, 18, 19, 21 | INTEGER |
| Double, Single, Decimal, Numeric, Currency | 4, 5, 6, 14, 131 | REAL |
| Yes/No | 11 | BOOLEAN |
| Date/Time | 7, 64, 133, 134, 135 | DATETIME |
| OLE Object (BLOB) | 128, 204, 205 | BLOB |
| GUID | 72 | TEXT |
| Text, Memo, and everything else | — | TEXT |

---

## Converting older formats

Jet 4.0 will not open an Access 2.0, 95 or 97 database. Convert through DAO, using the 32-bit `cscript`:

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

## Dependencies — Windows only, and all part of the OS

| Component | Location | Purpose |
|-----------|------|------------|
| `cscript.exe` (32-bit) | `C:\Windows\SysWOW64\cscript.exe` | The VBScript host for ADOX |
| `ADOX.Catalog` | COM / MDAC | Reading the database schema |
| `Microsoft.Jet.OLEDB.4.0` | COM / MDAC, 32-bit | Connecting to the `.mdb` |
| `Microsoft Access Driver (*.mdb)` | ODBC, 32-bit | Reading the rows |

All of it ships with Windows XP and later, and **needs no separate installation**.

---

## What works and what does not

| Operation | Supported |
|----------|---------------|
| Exporting tables | yes |
| TDTQL filtering | yes, pushed into SQL |
| Exporting views (Access Queries) | yes |
| Importing data | no — this is a read-only source |
| Incremental export | no |
| `--list` | only with rights on MSysObjects |
| Compression (zstd, kanzi) | yes |
| Compact format | ✅ |

---

## Compatibility

- ✅ Access 2000 / 2002 / 2003 (.mdb, Jet 4.0)
- Access 2007 and later (`.accdb`, ACE through the Jet ODBC driver)
- Access 97 and older — convert through DAO first
- Linux and macOS — unsupported, because of the Windows COM dependencies

## Links

- [docs/ACCESS_ADAPTER.md](../../../docs/ACCESS_ADAPTER.md)
- [alexbrainman/odbc](https://github.com/alexbrainman/odbc)
- [TDTP Specification](../../../docs/TDTP_SPEC.md)
