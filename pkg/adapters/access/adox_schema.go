//go:build windows

package access

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// adoxField is the JSON structure output by the VBScript helpers.
// The inspect VBScript populates all fields; the export VBScript omits
// NativeType/Identity/Nullable — they default to zero values, which is fine.
type adoxField struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	AdoxType   int    `json:"adoxtype"`
	Length     int    `json:"length"`
	Key        bool   `json:"key"`
	NativeType string `json:"native_type"` // human-readable Access type (inspect only)
	Identity   bool   `json:"identity"`    // AutoNumber column (inspect only)
	Nullable   bool   `json:"nullable"`    // column allows NULLs (inspect only)
}

// adoxFK is one FK column reference output by the inspect VBScript.
type adoxFK struct {
	Column    string `json:"column"`
	RefTable  string `json:"ref_table"`
	RefColumn string `json:"ref_column"`
	OnDelete  string `json:"on_delete"`
}

// adoxInspectResult is the top-level JSON returned by adoxInspectVBScript.
type adoxInspectResult struct {
	Columns     []adoxField `json:"columns"`
	ForeignKeys []adoxFK    `json:"foreign_keys"`
}

// adoxVBScript is the embedded VBScript that reads schema via ADOX.Catalog.
// Requires 32-bit cscript.exe (SysWOW64) because Jet 4.0 is 32-bit only.
// Usage: cscript //nologo <script.vbs> <mdb_path> <table_name> [uid] [pwd]
const adoxVBScript = `
Dim cat, tbl, col, args, mdbPath, tableName, uid, pwd, connStr
Set args = WScript.Arguments
If args.Count < 2 Then
    WScript.StdErr.WriteLine "Usage: script.vbs <mdb_path> <table_name> [uid] [pwd]"
    WScript.Quit 1
End If
mdbPath   = args(0)
tableName = args(1)
uid = ""
pwd = ""
If args.Count >= 3 Then uid = args(2)
If args.Count >= 4 Then pwd = args(3)

Set cat = CreateObject("ADOX.Catalog")
connStr = "Provider=Microsoft.Jet.OLEDB.4.0;Data Source=" & mdbPath
If uid <> "" Then connStr = connStr & ";User ID=" & uid
If pwd <> "" Then connStr = connStr & ";Password=" & pwd
On Error Resume Next
cat.ActiveConnection = connStr
If Err.Number <> 0 Then
    WScript.StdErr.WriteLine "ADOX connect error: " & Err.Description
    WScript.Quit 2
End If
On Error GoTo 0

Dim found, t
found = False
For Each t In cat.Tables
    If t.Name = tableName Then
        found = True
        Set tbl = t
        Exit For
    End If
Next
If Not found Then
    WScript.StdErr.WriteLine "Table not found: " & tableName
    WScript.Quit 3
End If


' Collect primary key column names
Dim pkCols, idx, idxCol
pkCols = ""
For Each idx In tbl.Indexes
    If idx.PrimaryKey Then
        For Each idxCol In idx.Columns
            pkCols = pkCols & "|" & idxCol.Name & "|"
        Next
        Exit For
    End If
Next

WScript.StdOut.Write "["
Dim first
first = True
For Each col In tbl.Columns
    If Not first Then WScript.StdOut.Write ","
    first = False
    Dim tdtpType, length, isKey
    tdtpType = AdoxTypeToTDTP(col.Type)
    length = 0
    If tdtpType = "TEXT" Then length = 1000
    isKey = "false"
    If InStr(pkCols, "|" & col.Name & "|") > 0 Then isKey = "true"
    WScript.StdOut.Write "{""name"":" & JsonStr(col.Name) & ",""type"":""" & tdtpType & """,""adoxtype"":" & col.Type & ",""length"":" & length & ",""key"":" & isKey & "}"
Next
WScript.StdOut.WriteLine "]"
Set cat = Nothing

Function AdoxTypeToTDTP(t)
    ' ADOX data type constants:
    ' adSmallInt=2, adInteger=3, adSingle=4, adDouble=5, adCurrency=6
    ' adDate=7, adBoolean=11, adDecimal=14, adTinyInt=16
    ' adUnsignedTinyInt=17, adUnsignedSmallInt=18, adUnsignedInt=19
    ' adBigInt=20, adUnsignedBigInt=21, adBinary=128, adVarBinary=204
    ' adLongVarBinary=205, adGUID=72, adNumeric=131
    ' adChar=129, adWChar=130, adVarChar=200, adVarWChar=202
    ' adLongVarChar=201, adLongVarWChar=203
    ' adDBDate=133, adDBTime=134, adDBTimeStamp=135, adFileTime=64
    Select Case t
        Case 2, 3, 16, 17, 18, 19, 20, 21
            AdoxTypeToTDTP = "INTEGER"
        Case 4, 5, 6, 14, 131
            AdoxTypeToTDTP = "REAL"
        Case 11
            AdoxTypeToTDTP = "BOOLEAN"
        Case 7, 64, 133, 134, 135
            AdoxTypeToTDTP = "DATETIME"
        Case 128, 204, 205
            AdoxTypeToTDTP = "BLOB"
        Case Else
            AdoxTypeToTDTP = "TEXT"
    End Select
End Function

Function JsonStr(s)
    Dim i, c, code, result
    result = """"
    For i = 1 To Len(s)
        c = Mid(s, i, 1)
        code = AscW(c)
        If code > 127 Or code < 32 Then
            result = result & "\u" & Right("000" & Hex(code), 4)
        ElseIf c = "\" Then
            result = result & "\\"
        ElseIf c = """" Then
            result = result & "\"""
        Else
            result = result & c
        End If
    Next
    JsonStr = result & """"
End Function
`

// getSchemaViaADOX tries to read column types from Access via ADOX (VBScript + cscript.exe).
// Returns nil slice + error if unavailable — caller falls back to sample-row inference.
func getSchemaViaADOX(dsn, tableName string) ([]adoxField, error) {
	cscript := `C:\Windows\SysWOW64\cscript.exe`
	if _, err := os.Stat(cscript); err != nil {
		return nil, fmt.Errorf("cscript.exe not found: %w", err)
	}

	mdbPath, uid, pwd := parseDSNForADOX(dsn)
	if mdbPath == "" {
		return nil, fmt.Errorf("cannot extract DBQ from DSN")
	}

	// Write VBScript to temp file
	tmp, err := os.CreateTemp("", "tdtp-adox-*.vbs")
	if err != nil {
		return nil, fmt.Errorf("cannot create temp vbs: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(adoxVBScript); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	_ = tmp.Close()

	args := []string{"//nologo", tmp.Name(), mdbPath, tableName}
	if uid != "" {
		args = append(args, uid)
	}
	if pwd != "" {
		args = append(args, pwd)
	}

	cmd := exec.Command(cscript, args...)
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		return nil, fmt.Errorf("cscript error: %w (stderr: %s)", err, strings.TrimSpace(string(stderr)))
	}

	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		// Re-run capturing combined output for diagnostics
		diagOut, _ := exec.Command(cscript, args...).CombinedOutput()
		return nil, fmt.Errorf("ADOX script produced no output (combined: %s)", strings.TrimSpace(string(diagOut)))
	}

	var fields []adoxField
	if err := json.Unmarshal([]byte(outStr), &fields); err != nil {
		return nil, fmt.Errorf("cannot parse ADOX output: %w (output: %s)", err, outStr)
	}
	return fields, nil
}

var reDSNKey = regexp.MustCompile(`(?i)(\w+)=([^;]*)`)

// parseDSNForADOX extracts DBQ, UID, PWD from an ODBC connection string.
func parseDSNForADOX(dsn string) (mdbPath, uid, pwd string) {
	for _, m := range reDSNKey.FindAllStringSubmatch(dsn, -1) {
		switch strings.ToUpper(m[1]) {
		case "DBQ":
			mdbPath = m[2]
		case "UID":
			uid = m[2]
		case "PWD":
			pwd = m[2]
		}
	}
	return
}

// adoxFieldsToSchemaOrdered reorders ADOX fields to match the ODBC column order.
// ADOX returns columns alphabetically; ODBC returns them in table definition order.
func adoxFieldsToSchemaOrdered(fields []adoxField, colOrder []string) packet.Schema {
	// Build lookup map: name → adoxField (case-insensitive)
	byName := make(map[string]adoxField, len(fields))
	for _, f := range fields {
		byName[strings.ToLower(f.Name)] = f
	}

	schema := packet.Schema{Fields: make([]packet.Field, len(colOrder))}
	for i, col := range colOrder {
		if f, ok := byName[strings.ToLower(col)]; ok {
			schema.Fields[i] = packet.Field{Name: col, Type: f.Type, Length: f.Length, Key: f.Key}
		} else {
			log.Printf("⚠ access ADOX: column %q not found in ADOX schema — defaulting to TEXT", col)
			schema.Fields[i] = packet.Field{Name: col, Type: "TEXT", Length: 1000}
		}
	}
	return schema
}

var timeType = reflect.TypeOf(time.Time{})

// goValueToTDTPType infers TDTP type from the actual Go value returned by Jet ODBC.
func goValueToTDTPType(v any) string {
	if v == nil {
		return "TEXT"
	}
	switch v.(type) {
	case int64, int32, int16, int8, int, uint64, uint32, uint16, uint8, uint:
		return "INTEGER"
	case float64, float32:
		return "REAL"
	case bool:
		return "BOOLEAN"
	case time.Time:
		return "DATETIME"
	case []byte:
		// Jet ODBC returns []byte for TEXT columns too — default to TEXT, not BLOB.
		// Real BLOB columns are rare and will be handled by ADOX schema path.
		return "TEXT"
	case string:
		return "TEXT"
	default:
		rv := reflect.ValueOf(v)
		if rv.Type().ConvertibleTo(timeType) {
			return "DATETIME"
		}
		return "TEXT"
	}
}

func goValueToTDTPLength(v any) int {
	if _, ok := v.(string); ok {
		return 1000
	}
	return 0
}

// adoxInspectVBScript is a VBScript that reads extended table metadata via ADOX.Catalog.
// Output: JSON {"columns":[...],"foreign_keys":[...]}
// Each column: {name, type(tdtp), native_type, adoxtype, length, key, identity, nullable}
// Each FK:     {column, ref_table, ref_column, on_delete}
const adoxInspectVBScript = `
Dim cat, tbl, col, args, mdbPath, tableName, uid, pwd, connStr
Set args = WScript.Arguments
If args.Count < 2 Then
    WScript.StdErr.WriteLine "Usage: script.vbs <mdb_path> <table_name> [uid] [pwd]"
    WScript.Quit 1
End If
mdbPath   = args(0)
tableName = args(1)
uid = ""
pwd = ""
If args.Count >= 3 Then uid = args(2)
If args.Count >= 4 Then pwd = args(3)

Set cat = CreateObject("ADOX.Catalog")
connStr = "Provider=Microsoft.Jet.OLEDB.4.0;Data Source=" & mdbPath
If uid <> "" Then connStr = connStr & ";User ID=" & uid
If pwd <> "" Then connStr = connStr & ";Password=" & pwd
On Error Resume Next
cat.ActiveConnection = connStr
If Err.Number <> 0 Then
    WScript.StdErr.WriteLine "ADOX connect error: " & Err.Description
    WScript.Quit 2
End If
On Error GoTo 0

Dim found, t
found = False
For Each t In cat.Tables
    If t.Name = tableName Then
        found = True
        Set tbl = t
        Exit For
    End If
Next
If Not found Then
    WScript.StdErr.WriteLine "Table not found: " & tableName
    WScript.Quit 3
End If

' Collect primary key column names
Dim pkCols, idx, idxCol
pkCols = ""
For Each idx In tbl.Indexes
    If idx.PrimaryKey Then
        For Each idxCol In idx.Columns
            pkCols = pkCols & "|" & idxCol.Name & "|"
        Next
        Exit For
    End If
Next

WScript.StdOut.Write "{""columns"":["
Dim first
first = True
For Each col In tbl.Columns
    If Not first Then WScript.StdOut.Write ","
    first = False

    Dim tdtpType, nativeType, colLen, isKey, isAuto, isNullable
    tdtpType   = AdoxTypeToTDTP(col.Type)
    nativeType = AdoxTypeToNative(col.Type)
    colLen     = col.DefinedSize
    isKey      = "false"
    If InStr(pkCols, "|" & col.Name & "|") > 0 Then isKey = "true"

    ' AutoIncrement (AutoNumber in Access)
    isAuto = "false"
    On Error Resume Next
    Dim autoVal
    autoVal = col.Properties("AutoIncrement").Value
    If Err.Number = 0 And autoVal = True Then isAuto = "true"
    Err.Clear
    On Error GoTo 0

    ' Nullable
    isNullable = "true"
    On Error Resume Next
    Dim nullVal
    nullVal = col.Properties("Nullable").Value
    If Err.Number = 0 Then
        If nullVal = False Then isNullable = "false"
    End If
    Err.Clear
    On Error GoTo 0

    WScript.StdOut.Write "{" & _
        """name"":" & JsonStr(col.Name) & _
        ",""type"":""" & tdtpType & """" & _
        ",""native_type"":""" & nativeType & """" & _
        ",""adoxtype"":" & col.Type & _
        ",""length"":" & colLen & _
        ",""key"":" & isKey & _
        ",""identity"":" & isAuto & _
        ",""nullable"":" & isNullable & _
        "}"
Next

WScript.StdOut.Write "],""foreign_keys"":["

' adKeyForeign = 2
first = True
Dim k, kc
For Each k In tbl.Keys
    If k.Type = 2 Then
        For Each kc In k.Columns
            If Not first Then WScript.StdOut.Write ","
            first = False
            WScript.StdOut.Write "{" & _
                """column"":" & JsonStr(kc.Name) & _
                ",""ref_table"":" & JsonStr(k.RelatedTable) & _
                ",""ref_column"":" & JsonStr(kc.RelatedColumn) & _
                ",""on_delete"":""" & DeleteRuleToStr(k.DeleteRule) & """" & _
                "}"
        Next
    End If
Next

WScript.StdOut.WriteLine "]}"
Set cat = Nothing

Function AdoxTypeToTDTP(t)
    Select Case t
        Case 2, 3, 16, 17, 18, 19, 20, 21 : AdoxTypeToTDTP = "INTEGER"
        Case 4, 5, 6, 14, 131             : AdoxTypeToTDTP = "REAL"
        Case 11                            : AdoxTypeToTDTP = "BOOLEAN"
        Case 7, 64, 133, 134, 135          : AdoxTypeToTDTP = "DATETIME"
        Case 128, 204, 205                 : AdoxTypeToTDTP = "BLOB"
        Case Else                          : AdoxTypeToTDTP = "TEXT"
    End Select
End Function

Function AdoxTypeToNative(t)
    Select Case t
        Case 2          : AdoxTypeToNative = "Number (Integer)"
        Case 3          : AdoxTypeToNative = "Number (Long Integer)"
        Case 4          : AdoxTypeToNative = "Number (Single)"
        Case 5          : AdoxTypeToNative = "Number (Double)"
        Case 6          : AdoxTypeToNative = "Currency"
        Case 7, 64, 133, 134, 135 : AdoxTypeToNative = "Date/Time"
        Case 11         : AdoxTypeToNative = "Yes/No"
        Case 14, 131    : AdoxTypeToNative = "Number (Decimal)"
        Case 16         : AdoxTypeToNative = "Number (Byte)"
        Case 17, 18, 19 : AdoxTypeToNative = "Number (Integer)"
        Case 20, 21     : AdoxTypeToNative = "Number (Long Integer)"
        Case 72         : AdoxTypeToNative = "Text (GUID)"
        Case 128, 204, 205      : AdoxTypeToNative = "OLE Object"
        Case 129, 130, 200, 202 : AdoxTypeToNative = "Text"
        Case 201, 203           : AdoxTypeToNative = "Memo"
        Case Else : AdoxTypeToNative = "Unknown (" & t & ")"
    End Select
End Function

Function DeleteRuleToStr(r)
    Select Case r
        Case 1  : DeleteRuleToStr = "CASCADE"
        Case 2  : DeleteRuleToStr = "SET NULL"
        Case 3  : DeleteRuleToStr = "SET DEFAULT"
        Case Else : DeleteRuleToStr = "NO ACTION"
    End Select
End Function

Function JsonStr(s)
    Dim i, c, code, result
    result = """"
    For i = 1 To Len(s)
        c = Mid(s, i, 1)
        code = AscW(c)
        If code > 127 Or code < 32 Then
            result = result & "\u" & Right("000" & Hex(code), 4)
        ElseIf c = "\" Then
            result = result & "\\"
        ElseIf c = """" Then
            result = result & "\"""
        Else
            result = result & c
        End If
    Next
    JsonStr = result & """"
End Function
`

// getInspectViaADOX runs adoxInspectVBScript via cscript.exe and returns extended
// column + FK metadata. Returns error if cscript/ADOX is unavailable.
func getInspectViaADOX(dsn, tableName string) (*adoxInspectResult, error) {
	cscript := `C:\Windows\SysWOW64\cscript.exe`
	if _, err := os.Stat(cscript); err != nil {
		return nil, fmt.Errorf("cscript.exe not found: %w", err)
	}

	mdbPath, uid, pwd := parseDSNForADOX(dsn)
	if mdbPath == "" {
		return nil, fmt.Errorf("cannot extract DBQ from DSN")
	}

	tmp, err := os.CreateTemp("", "tdtp-adox-inspect-*.vbs")
	if err != nil {
		return nil, fmt.Errorf("cannot create temp vbs: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(adoxInspectVBScript); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	_ = tmp.Close()

	args := []string{"//nologo", tmp.Name(), mdbPath, tableName}
	if uid != "" {
		args = append(args, uid)
	}
	if pwd != "" {
		args = append(args, pwd)
	}

	cmd := exec.Command(cscript, args...)
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		return nil, fmt.Errorf("cscript error: %w (stderr: %s)", err, strings.TrimSpace(string(stderr)))
	}

	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		diagOut, _ := exec.Command(cscript, args...).CombinedOutput()
		return nil, fmt.Errorf("ADOX inspect script produced no output (combined: %s)", strings.TrimSpace(string(diagOut)))
	}

	var result adoxInspectResult
	if err := json.Unmarshal([]byte(outStr), &result); err != nil {
		return nil, fmt.Errorf("cannot parse ADOX inspect output: %w (output: %s)", err, outStr)
	}
	return &result, nil
}

// schemaFromSampleRow infers types by scanning one real row (fallback).
// Columns with NULL in the sample row default to TEXT with a warning.
func schemaFromSampleRow(columns []string, vals []any) packet.Schema {
	fields := make([]packet.Field, len(columns))
	for i, col := range columns {
		t := goValueToTDTPType(vals[i])
		l := goValueToTDTPLength(vals[i])
		if vals[i] == nil {
			log.Printf("⚠ access schema: column %q is NULL in sample row — defaulting to TEXT", col)
		}
		fields[i] = packet.Field{Name: col, Type: t, Length: l}
	}
	return packet.Schema{Fields: fields}
}
