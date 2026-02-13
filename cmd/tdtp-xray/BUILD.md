# TDTP X-Ray - Build Instructions

## Prerequisites

- Go 1.21+
- Wails CLI v2.11+

### Install Wails

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

## Building

### Development Mode

```bash
cd cmd/tdtp-xray
wails dev
```

This will:
- Compile Go backend
- Open application window
- Enable hot reload (changes in `frontend/src/` trigger rebuild)

### Production Build

```bash
cd cmd/tdtp-xray
wails build
```

Output: `build/bin/tdtp-xray.exe` (Windows)

## Project Structure

```
cmd/tdtp-xray/
├── main.go              # Wails entry point
├── app.go               # Go API (backend)
├── wails.json           # Wails configuration
├── go.mod               # Go dependencies
├── frontend/
│   ├── src/             # Source files (edit these)
│   │   ├── index.html
│   │   ├── styles/
│   │   │   ├── main.css
│   │   │   └── wizard.css
│   │   └── scripts/
│   │       └── wizard.js
│   └── dist/            # Built files (auto-generated)
│       └── (mirrors src/)
└── build/               # Wails build output (gitignored)
    └── bin/
        └── tdtp-xray.exe
```

## Updating Frontend

After editing files in `frontend/src/`:

```bash
# Copy to dist/
cp -r frontend/src/* frontend/dist/

# Rebuild
wails build
```

## Troubleshooting

### "wails: command not found"

```bash
# Check GOPATH/bin is in PATH
echo $PATH | grep "go/bin"

# Add to PATH (Windows PowerShell)
$env:Path += ";$(go env GOPATH)\bin"
```

### "Cannot find wails.json"

Make sure you're in `cmd/tdtp-xray/` directory:
```bash
cd cmd/tdtp-xray
pwd  # Should end with: .../tdtp-framework/cmd/tdtp-xray
```

### "Failed to find Vite server URL"

This is expected - we use Vanilla JS (no Vite). Ignore this error or use:
```bash
wails build  # Instead of wails dev
```

### JavaScript not working

Open DevTools in the app (F12) and check Console for errors. Common issues:
- Wails runtime not loaded → check `wails/runtime/runtime.js`
- Path issues → verify `frontend/dist/` structure matches `frontend/src/`

## Windows-Specific

### WebView2 Required

Download from: https://developer.microsoft.com/microsoft-edge/webview2/

### MinGW (Optional)

For faster builds, install MinGW-w64. Or use:
```powershell
wails build -tags nopie
```

## Development Workflow

1. Edit files in `frontend/src/`
2. Copy to `dist/`: `cp -r frontend/src/* frontend/dist/`
3. Rebuild: `wails build`
4. Test: `.\build\bin\tdtp-xray.exe`

Or use `wails dev` for auto-reload (though it may show Vite errors - ignore them).
