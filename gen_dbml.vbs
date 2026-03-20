' gen_dbml.vbs — DBML schema generator for MS Access (.mdb)
' Output can be pasted into https://dbdiagram.io
' Requires: 32-bit cscript.exe (C:\Windows\SysWOW64\cscript.exe) + Jet 4.0
' Usage: cscript //nologo gen_dbml.vbs <path_to.mdb> [output.dbml]

Option Explicit

Dim cat, args, mdbPath, outFile, connStr
Set args = WScript.Arguments
If args.Count < 1 Then
    WScript.StdErr.WriteLine "Usage: gen_dbml.vbs <mdb_path> [output.dbml]"
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
lines = "// Generated from: " & mdbPath & vbLf & vbLf

' Tables
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
        lines = lines & "Table " & entityName & " [note: """ & EscQ(t.Name) & """] {" & vbLf

        For Each col In t.Columns
            Dim dbmlType, colName, attrs
            dbmlType = AdoxToDBML(col.Type)
            colName = Translit(col.Name)
            attrs = ""
            Dim notes
            notes = EscQ(col.Name)
            If InStr(pkCols, "|" & col.Name & "|") > 0 Then
                attrs = " [pk, note: """ & notes & """]"
            Else
                attrs = " [note: """ & notes & """]"
            End If
            lines = lines & "    " & colName & " " & dbmlType & attrs & vbLf
        Next
        lines = lines & "}" & vbLf & vbLf
    End If
Next

' References (foreign keys)
For Each t In cat.Tables
    If Left(t.Name,4) <> "MSys" And Left(t.Name,1) <> "~" Then
        For Each key In t.Keys
            If key.Type = 2 And key.RelatedTable <> "" Then
                Dim srcTable, dstTable
                srcTable = Translit(t.Name)
                dstTable = Translit(key.RelatedTable)
                ' Get first FK column
                Dim fkCol, refCol
                fkCol = ""
                refCol = ""
                Dim kc
                For Each kc In key.Columns
                    If fkCol = "" Then
                        fkCol = Translit(kc.Name)
                        refCol = Translit(kc.RelatedColumn)
                    End If
                Next
                If fkCol <> "" And refCol <> "" Then
                    lines = lines & "Ref: " & srcTable & "." & fkCol & " > " & dstTable & "." & refCol & vbLf
                End If
            End If
        Next
    End If
Next

' Write output
Dim stream
Set stream = CreateObject("ADODB.Stream")
stream.Open
stream.Type = 2
stream.Charset = "UTF-8"
stream.WriteText lines
If outFile <> "" Then
    stream.SaveToFile outFile, 2
    stream.Close
    WScript.StdOut.WriteLine "Written to: " & outFile
Else
    WScript.StdOut.Write stream.ReadText
    stream.Close
End If

' ---- helpers ----

Function AdoxToDBML(t)
    Select Case t
        Case 2,3,16,17,18,19,20,21 : AdoxToDBML = "int"
        Case 4,5,14,131            : AdoxToDBML = "float"
        Case 6                     : AdoxToDBML = "decimal"
        Case 11                    : AdoxToDBML = "boolean"
        Case 7,64,133,134,135      : AdoxToDBML = "datetime"
        Case 128,204,205           : AdoxToDBML = "blob"
        Case Else                  : AdoxToDBML = "varchar"
    End Select
End Function

Function EscQ(s)
    EscQ = Replace(s, """", "'")
End Function

Function Translit(s)
    Dim i, c, r, code
    r = ""
    For i = 1 To Len(s)
        c = Mid(s, i, 1)
        code = AscW(c)
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
