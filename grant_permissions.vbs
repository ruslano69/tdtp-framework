Dim dao, ws, db, doc
Dim mdbPath, sysPath

mdbPath = "H:\Ruslan\Code\Go\TDTP\tdtp-framework\DELO19_nopass.MDB"
sysPath = "H:\Ruslan\Code\Go\TDTP\tdtp-framework\SYSTEM_2003.MDA"

Set dao = CreateObject("DAO.DBEngine.120")
dao.SystemDB = sysPath
Set ws = dao.CreateWorkspace("", "newadmin", "newsipkey", 2)

On Error Resume Next
Set db = ws.OpenDatabase(mdbPath, False, False)
If Err.Number <> 0 Then
    WScript.Echo "FAIL open: " & Err.Description
    WScript.Quit 1
End If
On Error GoTo 0

' dbSecReadDef=4, dbSecReadData=1, dbSecInsertData=32, dbSecReplaceData=64, dbSecDeleteData=128
Dim readPerm
readPerm = 1 Or 4 Or 32 Or 64 Or 128

Dim cnt, skipped
cnt = 0 : skipped = 0

For Each doc In db.Containers("Tables").Documents
    If Left(doc.Name, 4) <> "MSys" Then
        On Error Resume Next
        doc.UserName = "Admin"
        doc.Permissions = readPerm
        If Err.Number <> 0 Then
            skipped = skipped + 1
            Err.Clear
        Else
            cnt = cnt + 1
        End If
        On Error GoTo 0
    End If
Next

db.Close
WScript.Echo "Done: " & cnt & " OK, " & skipped & " skipped"
