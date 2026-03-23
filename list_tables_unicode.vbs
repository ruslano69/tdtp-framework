Dim cat, t, mdbPath, name, i, c, code, out
mdbPath = WScript.Arguments(0)
Set cat = CreateObject("ADOX.Catalog")
On Error Resume Next
cat.ActiveConnection = "Provider=Microsoft.Jet.OLEDB.4.0;Data Source=" & mdbPath
If Err.Number <> 0 Then
    WScript.StdErr.WriteLine "Error: " & Err.Description
    WScript.Quit 1
End If
On Error GoTo 0
For Each t In cat.Tables
    If Left(t.Name, 4) <> "MSys" Then
        name = t.Name
        out = ""
        For i = 1 To Len(name)
            c = Mid(name, i, 1)
            code = AscW(c)
            If code > 127 Then
                out = out & "\u" & Right("0000" & Hex(code), 4)
            Else
                out = out & c
            End If
        Next
        WScript.StdOut.WriteLine out
    End If
Next
