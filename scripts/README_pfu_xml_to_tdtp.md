# PFU XML → TDTP converter

`pfu_xml_to_tdtp.go` converts the **illustrated** `<REESTR_LN><RECORD>` XML
shape to a TDTP v1.4 reference packet. This XML shape has not been verified
against a real PFU `export.xml`; the documented `WIC_*` export is not accepted
until a real, anonymized XML sample establishes its record nesting and values.

From the repository root in PowerShell:

```powershell
go run ./scripts/pfu_xml_to_tdtp.go `
  --input .\export.xml `
  --output .\leaves.tdtp.xml `
  --max-days 30

go run ./cmd/tdtpcli_v2 test .\leaves.tdtp.xml
```

`--max-days` is a required **business rule**, not a universal PFU limit. The
reference date is fixed once at startup in `Europe/Kyiv`. By default,
`DATE_START` must be within ±1 calendar month of it. Options:

- `--today YYYY-MM-DD`: fixed reference date for repeatable runs.
- `--window-months N`: number of calendar months on each side (default 1).
- `--window-field start|end|both`: which date must be in the window (default `start`).
- `--errors path`: path for the validation report (default `errors.xml` beside the TDTP output).

The converter rejects missing required fields, duplicate field names,
non-positive `VERSION`, impossible or non-ISO dates, an end before the start,
and durations over `--max-days`. Duration includes both endpoint dates. Every
`<RECORD>` is checked independently. Valid records are written to TDTP; invalid
ones are skipped and listed in `errors.xml` with their source record number,
`NUM_LN` when available, and the reason. The command prints a warning with
the skipped count. If every record is invalid, it writes only `errors.xml` and
exits with an error. A malformed XML document or unsupported envelope also
fails without a TDTP file. A clean run does not create `errors.xml`. Existing
output and report files are never intentionally replaced.

`NUM_LN`, `NUM_CASE`, and `RN_OKP` stay `TEXT`, preserving leading zeroes.
`DATE_START`/`DATE_END` become `DATE`; `VERSION` and the computed
`DURATION_DAYS` become `INTEGER`. Additional flat `RECORD` tags are retained as
`TEXT`. The output carries TDTP xxh3 integrity hashes and can be used as a
`type: tdtp` source in an ordinary ETL pipeline.
