# MS Access adapter

Exports data from Microsoft Access databases (`.mdb`, `.accdb`) through the
32-bit Jet 4.0 ODBC driver.

## Limitations

- **Windows only** (`//go:build windows`)
- **32-bit only** — Jet 4.0 ODBC exists only as 32-bit, so `GOARCH=386`
- Export only; there is no incremental export
- `--list` does not work: it needs rights on `MSysObjects` that the driver
  usually does not have. Name the table explicitly.

## Building (PowerShell)

```powershell
$env:GOPROXY="https://goproxy.io"
$env:GONOSUMDB="*"
$env:GOARCH="386"
go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/
$env:GOARCH=""   # reset it afterwards
```

On one line:
```powershell
$env:GOPROXY="https://goproxy.io"; $env:GONOSUMDB="*"; $env:GOARCH="386"; go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/; $env:GOARCH=""
```

## Configuration

Access is configured through `dsn` and nothing else — the individual `host`,
`port` and `database` fields do not apply to it.

```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;UID=Admin;PWD=;"

export:
  compress: true
  compress_level: 3
```

With a database password and a workgroup file (user-level security):
```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;SystemDB=C:\\path\\to\\SYSTEM.MDW;UID=sklad;PWD=secret;"
```

With Windows-1251 text, where the data is not UTF-8:
```yaml
database:
  type: access
  dsn: "Driver={Microsoft Access Driver (*.mdb)};DBQ=C:\\path\\to\\db.mdb;UID=Admin;PWD=;"
  charset: windows-1251
```

## Commands

```powershell
# Export a table to TDTP XML (multi-part)
.\tdtpcli_x86.exe --config access.yaml --export Товары --output tovary.tdtp.xml

# Export to XLSX (every row, one file)
.\tdtpcli_x86.exe --config access.yaml --export-xlsx Товары --output tovary.xlsx

# Listing tables — usually fails without rights on MSysObjects; name the table instead
.\tdtpcli_x86.exe --config access.yaml --list
```

## Converting older formats

Jet 4.0 will not open an Access 2.0, 95 or 97 database. Convert through DAO,
using the 32-bit `cscript`.

**convert_mdb.vbs** — convert the format:
```vbscript
Dim dao, src, dst
src = "C:\path\DELO19.MDB"
dst = "C:\path\DELO19_2003.MDB"
Set dao = CreateObject("DAO.DBEngine.120")
dao.CompactDatabase src, dst, ";LANGID=0x0419;CP=1251;COUNTRY=0", 64, ";PWD=yourpassword"
WScript.Echo "Done: " & dst
```

Run it with the 32-bit host — this matters:
```powershell
C:\Windows\SysWOW64\cscript.exe //nologo convert_mdb.vbs
```

**Removing the database password** — compact with an empty destination password:
```vbscript
dao.CompactDatabase src, dst, ";LANGID=0x0419;CP=1251;COUNTRY=0;PWD=", 64, ";PWD=yourpassword"
```

**User-level security** (a `.mda`/`.mdw` workgroup file): compacting preserves
it. To remove it, open the database as an admin user, grant rights through DAO
or ADOX, and compact again without `SystemDB`.

## How column types are determined

Jet's ODBC driver does not report column types through the standard
`DatabaseTypeName()` — it always returns empty. The adapter reads the schema
through **ADOX**, the COM provider built into Windows:

1. a temporary VBScript is generated at `%TEMP%\tdtp-adox-*.vbs`
2. it runs under `C:\Windows\SysWOW64\cscript.exe` (32-bit)
3. the script connects through `Microsoft.Jet.OLEDB.4.0` and reads `ADOX.Catalog`
4. it returns the column types as JSON, which Go parses into a schema

If `cscript.exe` is unavailable the adapter degrades to inferring types from a
sample row, with a warning on stderr.

### ADOX to TDTP type mapping

| ADOX type | Number | TDTP |
|-----------|--------|------|
| adSmallInt, adInteger, adTinyInt, adUnsignedTinyInt, adUnsignedSmallInt, adUnsignedInt, adBigInt, adUnsignedBigInt | 2, 3, 16, 17, 18, 19, 20, 21 | INTEGER |
| adSingle, adDouble, adCurrency, adDecimal, adNumeric | 4, 5, 6, 14, 131 | REAL |
| adBoolean | 11 | BOOLEAN |
| adDate, adDBFileTime, adDBDate, adDBTime, adDBTimeStamp | 7, 64, 133, 134, 135 | DATETIME |
| adBinary, adVarBinary, adLongVarBinary | 128, 204, 205 | BLOB |
| everything else — adGUID 72, adVarWChar 202, adLongVarWChar 203 and the rest | — | TEXT |

> Note that `adVarNumeric` (139) is **not** mapped to REAL. It falls through to
> TEXT along with everything else the table does not name. An earlier version of
> this document listed it as REAL, which the code has never done.

## Dependencies — all present on Windows

| Component | Location | Purpose |
|-----------|----------|---------|
| `cscript.exe` | `C:\Windows\SysWOW64\cscript.exe` | 32-bit VBScript host |
| `ADOX.Catalog` | COM (MDAC) | Reading the database schema |
| `Microsoft.Jet.OLEDB.4.0` | COM (MDAC, 32-bit) | Connecting to `.mdb` |
| `Microsoft Access Driver (*.mdb)` | ODBC, 32-bit | Reading the data |

All of it ships with Windows XP and later; nothing needs installing.
