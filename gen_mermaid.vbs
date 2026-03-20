' gen_mermaid.vbs — Mermaid ER diagram generator for MS Access (.mdb)
' Requires: 32-bit cscript.exe (C:\Windows\SysWOW64\cscript.exe) + Jet 4.0
' Usage: cscript //nologo gen_mermaid.vbs <path_to.mdb> [output.md]
'
' Entity names are transliterated to Latin (Mermaid requirement).
' Original Russian name is preserved as %% comment above each entity block.

Option Explicit

Dim cat, args, mdbPath, outFile, connStr
Set args = WScript.Arguments
If args.Count < 1 Then
    WScript.StdErr.WriteLine "Usage: gen_mermaid.vbs <mdb_path> [output.md]"
    WScript.Quit 1
End If
mdbPath = args(0)
outFile = ""
If args.Count >= 2 Then outFile = args(1)

Set cat = CreateObject("ADOX.Catalog")
connStr = "Provider=Microsoft.Jet.OLEDB.4.0;Data Source=" & mdbPath
On Error Resume Next
cat.ActiveConnection = connStr
If Err.Number <> 0 Then
    WScript.StdErr.WriteLine "ADOX error: " & Err.Description
    WScript.Quit 2
End If
On Error GoTo 0

Dim lines, t, col, idx, idxCol, key
lines = "erDiagram" & vbLf

For Each t In cat.Tables
    If Left(t.Name,4) <> "MSys" And Left(t.Name,1) <> "~" Then
        Dim pkCols
        pkCols = ""
        For Each idx In t.Indexes
            If idx.PrimaryKey Then
                For Each idxCol In idx.Columns
                    pkCols = pkCols & "|" & idxCol.Name & "|"
                Next
                Exit For
            End If
        Next

        Dim entityName
        entityName = Translit(t.Name)
        lines = lines & "    %% " & t.Name & vbLf
        lines = lines & "    " & entityName & " {" & vbLf

        For Each col In t.Columns
            Dim tdtpType, colName, pkMark
            tdtpType = AdoxToType(col.Type)
            colName = SafeName(col.Name)
            pkMark = ""
            If InStr(pkCols, "|" & col.Name & "|") > 0 Then pkMark = " PK"
            lines = lines & "        " & tdtpType & " " & colName & pkMark & vbLf
        Next
        lines = lines & "    }" & vbLf
    End If
Next

For Each t In cat.Tables
    If Left(t.Name,4) <> "MSys" And Left(t.Name,1) <> "~" Then
        For Each key In t.Keys
            If key.Type = 2 And key.RelatedTable <> "" Then
                Dim src, dst
                src = Translit(t.Name)
                dst = Translit(key.RelatedTable)
                lines = lines & "    " & src & " }o--|| " & dst & " : """ & SafeName(key.Name) & """" & vbLf
            End If
        Next
    End If
Next

Dim result
result = "```mermaid" & vbLf & lines & "```" & vbLf

If outFile <> "" Then
    Dim stream
    Set stream = CreateObject("ADODB.Stream")
    stream.Open
    stream.Type = 2
    stream.Charset = "UTF-8"
    stream.WriteText result
    stream.SaveToFile outFile, 2
    stream.Close
    WScript.StdOut.WriteLine "Written to: " & outFile
Else
    WScript.StdOut.Write result
End If

Function AdoxToType(t)
    Select Case t
        Case 2,3,16,17,18,19,20,21 : AdoxToType = "int"
        Case 4,5,6,14,131          : AdoxToType = "float"
        Case 11                    : AdoxToType = "boolean"
        Case 7,64,133,134,135      : AdoxToType = "datetime"
        Case 128,204,205           : AdoxToType = "bytes"
        Case Else                  : AdoxToType = "string"
    End Select
End Function

Function SafeName(s)
    Dim r
    r = Replace(s, " ", "_")
    r = Replace(r, "/", "_")
    r = Replace(r, "%", "pct")
    r = Replace(r, "?", "")
    SafeName = r
End Function

Function Translit(s)
    ' Uses ChrW() for Cyrillic to avoid ANSI/UTF-8 file encoding issues.
    ' ChrW() takes Unicode code points directly - safe in any file encoding.
    Dim i, c, r, code
    r = ""
    For i = 1 To Len(s)
        c = Mid(s, i, 1)
        code = AscW(c)
        ' Uppercase Cyrillic: U+0410(A) .. U+042F(Ya) = code 1040..1071
        ' Lowercase Cyrillic: U+0430(a) .. U+044F(ya) = code 1072..1103
        ' Yo uppercase U+0401=1025, yo lowercase U+0451=1105
        If code = 1025 Or code = 1105 Then
            r = r & "Yo"
        ElseIf code >= 1040 And code <= 1071 Then
            r = r & CyrUpper(code - 1040)
        ElseIf code >= 1072 And code <= 1103 Then
            r = r & LCase(CyrUpper(code - 1072))
        ElseIf c = " " Or c = "_" Then
            r = r & "_"
        ElseIf (code >= 48 And code <= 57) Or _
               (code >= 65 And code <= 90) Or _
               (code >= 97 And code <= 122) Then
            r = r & c
        End If
    Next
    If r = "" Then r = "T_" & Len(s)
    Translit = r
End Function

Function CyrUpper(n)
    ' Maps Cyrillic uppercase index (0=A..31=Ya) to Latin transliteration.
    ' Follows standard Russian GOST transliteration.
    Dim tbl(31)
    tbl(0)="A"  : tbl(1)="B"  : tbl(2)="V"  : tbl(3)="G"
    tbl(4)="D"  : tbl(5)="E"  : tbl(6)="Zh" : tbl(7)="Z"
    tbl(8)="I"  : tbl(9)="Y"  : tbl(10)="K" : tbl(11)="L"
    tbl(12)="M" : tbl(13)="N" : tbl(14)="O" : tbl(15)="P"
    tbl(16)="R" : tbl(17)="S" : tbl(18)="T" : tbl(19)="U"
    tbl(20)="F" : tbl(21)="Kh": tbl(22)="Ts": tbl(23)="Ch"
    tbl(24)="Sh": tbl(25)="Shch": tbl(26)="": tbl(27)="Y"
    tbl(28)=""  : tbl(29)="E" : tbl(30)="Yu": tbl(31)="Ya"
    If n >= 0 And n <= 31 Then
        CyrUpper = tbl(n)
    Else
        CyrUpper = ""
    End If
End Function
