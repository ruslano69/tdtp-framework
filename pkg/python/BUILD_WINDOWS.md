# Building Python Module on Windows

This guide explains how to build the TDTP Python module on Windows.

## Prerequisites

1. **Go 1.19+** installed and in PATH
2. **Python 3.7+** installed
3. **GCC for Windows** (MinGW-w64) - required for CGO
4. **Git** for cloning the repository

## Installing MinGW-w64

CGO requires GCC on Windows. Install MinGW-w64:

### Option 1: Via Chocolatey (Recommended)
```powershell
choco install mingw
```

### Option 2: Direct Download
1. Download from: https://github.com/niXman/mingw-builds-binaries/releases
2. Extract to `C:\mingw64`
3. Add `C:\mingw64\bin` to PATH

### Option 3: MSYS2
```powershell
# Install MSYS2 from https://www.msys2.org/
# Then in MSYS2 shell:
pacman -S mingw-w64-x86_64-gcc
```

## Build Steps

### 1. Clone and Update Repository

```powershell
cd G:\DEV\Go\TDTP\tdtp-framework-main
git pull origin claude/fix-etl-case-mismatch-6T6Pm
```

### 2. Navigate to Python Module

```powershell
cd pkg\python\libtdtp
```

### 3. Update Dependencies

```powershell
go mod tidy
```

If you get network errors, try:
```powershell
$env:GOPROXY="https://goproxy.io,direct"
go mod tidy
```

Or use direct mode:
```powershell
$env:GOPROXY="direct"
go mod tidy
```

### 4. Build Shared Library

```powershell
go build -buildmode=c-shared -o ..\tdtp\libtdtp.dll main.go
```

**Note:** On Windows, the output is `.dll` (not `.so` like on Linux)

### 5. Verify Build

Check that the DLL was created:
```powershell
dir ..\tdtp\libtdtp.dll
```

You should see:
- `libtdtp.dll` - the shared library
- `libtdtp.h` - the C header (auto-generated)

### 6. Install Python Package

```powershell
cd ..
pip install -e .
```

### 7. Test

```powershell
python -c "import tdtp; print(tdtp.__version__)"
```

## Troubleshooting

### Error: "gcc: command not found"

**Solution:** GCC not in PATH. Install MinGW-w64 and add to PATH:

```powershell
$env:Path += ";C:\mingw64\bin"
# Or permanently:
[Environment]::SetEnvironmentVariable("Path", $env:Path + ";C:\mingw64\bin", "User")
```

### Error: "cannot find module providing package"

**Solution:** Run `go mod tidy` to download dependencies:

```powershell
cd pkg\python\libtdtp
$env:GOPROXY="https://goproxy.io,direct"
go mod tidy
```

### Error: "dial tcp: lookup storage.googleapis.com"

**Solution:** Network issue with Go proxy. Use alternative:

```powershell
# Use Chinese proxy (faster for some regions)
$env:GOPROXY="https://goproxy.cn,direct"
go mod tidy

# Or use direct GitHub
$env:GOPROXY="direct"
go mod tidy
```

### Error: Building on Windows Subsystem for Linux (WSL)

If building in WSL, you need to build for Linux:

```bash
cd pkg/python/libtdtp
go build -buildmode=c-shared -o ../tdtp/libtdtp.so main.go
```

## Complete Build Script

Save as `build.ps1`:

```powershell
# Build TDTP Python Module
$ErrorActionPreference = "Stop"

Write-Host "Building TDTP Python Module for Windows..." -ForegroundColor Green

# Check prerequisites
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Error: Go not found. Install from https://go.dev/dl/" -ForegroundColor Red
    exit 1
}

if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
    Write-Host "Error: GCC not found. Install MinGW-w64" -ForegroundColor Red
    exit 1
}

# Navigate to libtdtp
Set-Location pkg\python\libtdtp

# Update dependencies
Write-Host "Updating dependencies..." -ForegroundColor Yellow
$env:GOPROXY="https://goproxy.io,direct"
go mod tidy

if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: go mod tidy failed" -ForegroundColor Red
    exit 1
}

# Build shared library
Write-Host "Building shared library..." -ForegroundColor Yellow
go build -buildmode=c-shared -o ..\tdtp\libtdtp.dll main.go

if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Build failed" -ForegroundColor Red
    exit 1
}

Write-Host "✓ Build successful!" -ForegroundColor Green
Write-Host "Output: pkg\python\tdtp\libtdtp.dll" -ForegroundColor Green

# Install Python package
Set-Location ..
Write-Host "Installing Python package..." -ForegroundColor Yellow
pip install -e .

if ($LASTEXITCODE -eq 0) {
    Write-Host "✓ Installation complete!" -ForegroundColor Green

    # Test
    Write-Host "Testing import..." -ForegroundColor Yellow
    python -c "import tdtp; print('TDTP version:', tdtp.__version__)"

    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ All done! TDTP Python module is ready." -ForegroundColor Green
    }
}
```

Run:
```powershell
.\build.ps1
```

## Next Steps

After successful build:

1. **Test with a TDTP file:**
```python
import tdtp
df = tdtp.read_tdtp('path/to/file.tdtp.xml')
print(df.shape)
```

2. **Check examples:**
```powershell
cd pkg\python
python example.py path\to\file.tdtp.xml
```

3. **Use with pandas:**
```python
import tdtp
df = tdtp.read_tdtp('file.tdtp.xml')
pdf = df.to_pandas()
pdf.to_excel('output.xlsx')
```

## Support

If you encounter issues:
- Check that Go, GCC, and Python are in PATH
- Try alternative GOPROXY settings
- Ensure you pulled latest changes with import fixes
- Open issue at: https://github.com/ruslano69/tdtp-framework/issues
