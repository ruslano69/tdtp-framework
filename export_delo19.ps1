$ErrorActionPreference = "Continue"

$mdbPath  = "$PSScriptRoot\DELO19_nopass.MDB"
$outDir   = "$PSScriptRoot\DELO19"
$config   = "$PSScriptRoot\access_delo19.yaml"
$cli      = "$PSScriptRoot\tdtpcli_x86.exe"
$vbs      = "$PSScriptRoot\list_tables_unicode.vbs"

New-Item -ItemType Directory -Force -Path $outDir | Out-Null

# Get table list (unicode-escaped ASCII from VBScript)
$rawLines = & 'C:\Windows\SysWOW64\cscript.exe' //nologo $vbs $mdbPath 2>$null

# Decode \uXXXX escapes to real Unicode strings
function Decode-Unicode($s) {
    [regex]::Replace($s, '\\u([0-9A-Fa-f]{4})', {
        param($m) [char][convert]::ToInt32($m.Groups[1].Value, 16)
    })
}

$tables = $rawLines | Where-Object { $_ -ne "" } | ForEach-Object { Decode-Unicode $_ }
Write-Host "Found $($tables.Count) tables"

$ok   = 0
$fail = 0
$failList = @()

foreach ($table in $tables) {
    $safe   = $table -replace '[\\/:*?"<>|]', '_'
    $output = "$outDir\$safe.tdtp.xml"

    Write-Host -NoNewline "  Exporting '$table' ... "
    $result = & $cli --config $config --export $table --output $output 2>&1
    $exitCode = $LASTEXITCODE
    if ($exitCode -eq 0) {
        Write-Host "OK"
        $ok++
    } else {
        Write-Host "FAIL"
        $failList += $table
        $fail++
    }
}

Write-Host ""
Write-Host "Done: $ok OK, $fail failed"
if ($failList.Count -gt 0) {
    Write-Host "Failed tables:"
    $failList | ForEach-Object { Write-Host "  - $_" }
}
