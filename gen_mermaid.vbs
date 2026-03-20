Option Explicit

Dim dbPath, cat, tbl, col, key
Dim output
Dim pkCols

dbPath = "H:\Ruslan\Code\Go\TDTP\tdtp-framework\DELO19_nopass.MDB"

Set cat = CreateObject("ADOX.Catalog")
cat.ActiveConnection = "Provider=Microsoft.Jet.OLEDB.4.0;Data Source=" & dbPath

output = "erDiagram" & vbCrLf

' --- Tables ---
For Each tbl In cat.Tables
    Dim tname
    tname = tbl.Name

    ' Skip system tables
    If Left(tname, 4) = "MSys" Or Left(tname, 1) = "~" Then
        ' skip
    ElseIf tbl.Columns.Count = 0 Then
        ' skip empty
    Else
        ' Collect PK columns
        Set pkCols = CreateObject("Scripting.Dictionary")
        Dim idx
        For Each idx In tbl.Indexes
            If idx.PrimaryKey Then
                Dim idxCol
                For Each idxCol In idx.Columns
                    pkCols(idxCol.Name) = True
                Next
            End If
        Next

        ' Mermaid entity name: replace spaces with underscores
        Dim ename
        ename = Replace(tname, " ", "_")

        output = output & "    " & ename & " {" & vbCrLf

        For Each col In tbl.Columns
            Dim ctype, cname, suffix
            ctype = AdoxTypeToStr(col.Type)
            cname = Replace(col.Name, " ", "_")
            suffix = ""
            If pkCols.Exists(col.Name) Then
                suffix = " PK"
            End If
            output = output & "        " & ctype & " " & cname & suffix & vbCrLf
        Next

        output = output & "    }" & vbCrLf
    End If
Next

' --- Relationships (Foreign Keys) ---
For Each tbl In cat.Tables
    Dim t2name
    t2name = tbl.Name
    If Left(t2name, 4) <> "MSys" And Left(t2name, 1) <> "~" Then
        For Each key In tbl.Keys
            ' key.Type = 2 means Foreign Key (adKeyForeign)
            If key.Type = 2 Then
                Dim fromEnt, toEnt, relName
                fromEnt = Replace(t2name, " ", "_")
                toEnt = Replace(key.RelatedTable, " ", "_")
                relName = key.Name
                output = output & "    " & fromEnt & " ||--o{ " & toEnt & " : """ & relName & """" & vbCrLf
            End If
        Next
    End If
Next

' Write output to file using ADODB.Stream for UTF-8
Dim stream
Set stream = CreateObject("ADODB.Stream")
stream.Type = 2 ' adTypeText
stream.Charset = "utf-8"
stream.Open
stream.WriteText output
stream.SaveToFile "H:\Ruslan\Code\Go\TDTP\tdtp-framework\DELO19_schema_raw.txt", 2
stream.Close

WScript.Echo "Done. Lines written: " & UBound(Split(output, vbCrLf))

' --- Helper function ---
Function AdoxTypeToStr(adoxType)
    Select Case adoxType
        Case 2, 3, 16, 17, 18, 19, 20, 21
            AdoxTypeToStr = "int"
        Case 4, 5, 6, 14, 131
            AdoxTypeToStr = "float"
        Case 11
            AdoxTypeToStr = "boolean"
        Case 7, 64, 133, 134, 135
            AdoxTypeToStr = "datetime"
        Case 128, 204, 205
            AdoxTypeToStr = "bytes"
        Case Else
            AdoxTypeToStr = "string"
    End Select
End Function
