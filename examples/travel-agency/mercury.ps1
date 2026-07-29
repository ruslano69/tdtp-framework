#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Starts the xZMercury mock that hands out encryption keys.

.DESCRIPTION
    With encryption on, nothing moves without this: the exporter asks Mercury to
    bind a key to the packet's UUID, and every listener asks it to resolve that
    key back. The key itself never touches a file — it lives in Mercury's memory
    and is issued once per package.

    That is also the shape of the failure to expect. If Mercury is down, the
    export fails at the encrypt step and the checkpoint stays put, so nothing is
    lost; but a packet already sitting in a queue cannot be opened until Mercury
    is back, because the key is not in the packet.

.PARAMETER Addr
    Listen address. Default :3077, matching security.mercury_url in configs/.

.EXAMPLE
    ./mercury.ps1

.NOTES
    This is the mock from cmd/xzmercury-mock, and it is a mock in one way that
    matters: it does not sign its responses, so the client has to be told to
    skip HMAC verification with MERCURY_SERVER_SECRET=dev-mode — which
    listeners.ps1 and orchestrator.ps1 set for you.

    A real deployment runs real xZMercury and a real shared secret, and then
    that variable must NOT be set to dev-mode: the verification it skips is what
    proves the key came from the server you think it did, and a leaked dev
    binding would otherwise be accepted.
#>

[CmdletBinding()]
param([string]$Addr = ':3077')

$ErrorActionPreference = 'Stop'

$TravelDir = $PSScriptRoot
$RepoRoot = Split-Path (Split-Path $TravelDir -Parent) -Parent

$Mock = Join-Path $RepoRoot 'xzmercury-mock.exe'
if (-not (Test-Path $Mock)) { $Mock = Join-Path $RepoRoot 'xzmercury-mock' }
if (-not (Test-Path $Mock)) {
    throw "xzmercury-mock not found at $Mock. Build it: go build -o xzmercury-mock.exe ./cmd/xzmercury-mock/"
}

Write-Host 'xZMercury mock (key binding for encrypted packets)' -ForegroundColor Cyan
Write-Host "  url : http://localhost$($Addr.TrimStart(':') -replace '^', ':')"
Write-Host '  note: does not sign responses — clients run with MERCURY_SERVER_SECRET=dev-mode'
Write-Host ''

$proc = Start-Process -FilePath $Mock -PassThru -NoNewWindow -ArgumentList @('--addr', $Addr)
Write-Host "  started pid=$($proc.Id)" -ForegroundColor Green
Write-Host '  Stop with shutdown.ps1, or: Stop-Process -Id ' -NoNewline
Write-Host $proc.Id
