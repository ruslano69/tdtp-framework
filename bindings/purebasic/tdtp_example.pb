;
; tdtp_example.pb -- Reading a .tdtp.xml file from PureBasic via libtdtp.dll
;
; libtdtp.dll is the JSON-boundary C ABI of the TDTP framework (pkg/python/libtdtp,
; -buildmode=c-shared). It's the same DLL the Python bindings load via ctypes --
; here it's driven directly from PureBasic's own native C interface.
;
; Two non-obvious things this example gets right (verified empirically against a
; real build, not assumed from docs):
;
;   1. ParseJSON(#JSON, Input$) -- when called with #PB_Any as the first argument,
;      PureBasic returns the *real* allocated handle as the function's return
;      value; it does NOT write it back into the variable you passed in. Reusing
;      that variable as if it now held a valid handle passes an invalid ID
;      (still #PB_Any) into every JSON call that follows, and crashes inside
;      JSONValue() with an access violation.
;
;   2. Never call CloseLibrary() on libtdtp.dll. Go's c-shared runtime keeps
;      background goroutines running (GC, sysmon) that are not designed to
;      survive FreeLibrary -- calling it crashes during DLL_PROCESS_DETACH.
;      Just leave the library loaded; Windows reclaims it when the process exits,
;      which is safe.
;

;- === C ABI prototypes ===

Prototype.i P_J_ReadFile(path.p-utf8)  ; char* J_ReadFile(char* path) -> owned by Go, must free
Prototype   P_J_FreeString(ptr.i)      ; void J_FreeString(char* ptr) -- takes the raw pointer, not a PB string

Global J_ReadFile.P_J_ReadFile
Global J_FreeString.P_J_FreeString

;- === Load the library and resolve the exports we use ===

Procedure.i LoadTDTP(dllPath.s)
  If OpenLibrary(0, dllPath)
    J_ReadFile   = GetFunction(0, "J_ReadFile")
    J_FreeString = GetFunction(0, "J_FreeString")
    ProcedureReturn Bool(J_ReadFile And J_FreeString)
  EndIf
  ProcedureReturn #False
EndProcedure

;- === Read a .tdtp.xml file, return a parsed #JSON handle (0 on failure) ===
;
; Caller owns the returned handle and must FreeJSON() it when done.
; Canonical shape (see bindings/python/DEVELOPER_GUIDE.md):
;   {"schema": {"fields": [{"name":..., "type":..., "key":...}, ...]},
;    "header": {"table_name":..., "message_id":..., "timestamp":...},
;    "data":   [["v1","v2",...], ...]}

Procedure.i ReadTDTPFile(path.s)
  Protected ptr.i, json$, jsonId.i, root.i

  ptr = J_ReadFile(path)
  If ptr = 0
    Debug "J_ReadFile: NULL pointer (library-level failure)"
    ProcedureReturn 0
  EndIf

  json$ = PeekS(ptr, -1, #PB_UTF8)  ; copy the content BEFORE freeing it
  J_FreeString(ptr)                 ; mandatory -- otherwise it leaks the Go heap

  jsonId = ParseJSON(#PB_Any, json$)  ; the real handle comes back as the return value
  If Not jsonId
    Debug "JSON parse failed: " + JSONErrorMessage()
    ProcedureReturn 0
  EndIf

  root = JSONValue(jsonId)

  ; TDTP-level error (file not found, checksum mismatch, etc.) comes back as
  ; {"error": "..."} rather than a library-level NULL pointer.
  If GetJSONMember(root, "error")
    Debug "TDTP error: " + GetJSONString(GetJSONMember(root, "error"))
    FreeJSON(jsonId)
    ProcedureReturn 0
  EndIf

  ProcedureReturn jsonId
EndProcedure

;- === Example usage ===

If LoadTDTP("libtdtp.dll")

  doc = ReadTDTPFile("users.tdtp.xml")
  If doc
    root = JSONValue(doc)

    ; --- Header ---
    header = GetJSONMember(root, "header")
    Debug "Table: " + GetJSONString(GetJSONMember(header, "table_name"))

    ; --- Schema.fields: iterate an array of objects ---
    fields = GetJSONMember(GetJSONMember(root, "schema"), "fields")
    For i = 0 To JSONArraySize(fields) - 1
      f = GetJSONElement(fields, i)
      Debug GetJSONString(GetJSONMember(f, "name")) + " : " + GetJSONString(GetJSONMember(f, "type"))
    Next i

    ; --- Data: dump straight into a 2D array (ExtractJSONArray handles nested arrays) ---
    Dim rows.s(0, 0)
    ExtractJSONArray(GetJSONMember(root, "data"), rows())
    ; rows(row, col) now holds every cell as a string

    FreeJSON(doc)
  Else
    Debug "Failed to read/parse the TDTP file"
  EndIf

  ; Deliberately no CloseLibrary(0) -- see the note above.

Else
  Debug "Failed to load libtdtp.dll or resolve its exports"
EndIf
