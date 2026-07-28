#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Starts the orchestrator that drives the export side of this example.

.DESCRIPTION
    Replaces coordinator.py.

    That script kept a cursor per (routing key, table) in Redis, subscribed to
    a RabbitMQ topic exchange, debounced, and shelled out to
    `tdtpcli --export-broker` with a hand-built --where clause. Every one of
    those responsibilities now belongs to something that already had it:

      cursor        -> state/*.json, written by --sync-incremental from the
                       exported data rather than from datetime.now()
      fan-out       -> workflows/sync_out.yaml, run by tdtpcli --steps
      trigger       -> a schedule in this orchestrator
      audit         -> the orchestrator's job log, one record per run

    What the orchestrator adds over a bare timer is the reason to use it:
    scenarios must be checksum-approved before they will run, every run is a
    job with its output retained, and the HTTP API can run, list, and cancel
    them. Edit a workflow file and its approval stops matching -- the schedule
    then skips the run instead of executing changed content unnoticed.

.PARAMETER Addr
    Listen address for the HTTP API. Default :8099.

.PARAMETER Approve
    Approve both scenarios after startup. Required on first run, and again
    after editing a workflow file. Without it the schedules fire but every run
    is refused, which is the intended behaviour, not a fault.

.EXAMPLE
    ./orchestrator.ps1 -Approve      # first run
    ./orchestrator.ps1               # afterwards

.NOTES
    Must run from this directory: the workflow steps use paths relative to it
    (configs/, state/), and tdtpcli inherits the orchestrator's working
    directory.

    --no-auth makes every request admin. That is a local-development setting;
    a real deployment issues tokens through /tokens and gives the central and
    branch operators separate ones.
#>

[CmdletBinding()]
param(
    [string]$Addr = ':8099',
    [switch]$Approve
)

$ErrorActionPreference = 'Stop'

$TravelDir = $PSScriptRoot
# Split-Path twice rather than Join-Path with three arguments: the
# multi-segment form of Join-Path is PowerShell 6+, and this repository is run
# under Windows PowerShell 5.1.
$RepoRoot = Split-Path (Split-Path $TravelDir -Parent) -Parent

$Orchestrator = Join-Path $RepoRoot 'orchestrator.exe'
if (-not (Test-Path $Orchestrator)) { $Orchestrator = Join-Path $RepoRoot 'orchestrator' }
if (-not (Test-Path $Orchestrator)) {
    throw "orchestrator not found at $Orchestrator. Build it: go build -o orchestrator.exe ./cmd/orchestrator/"
}

$TdtpCli = Join-Path $RepoRoot 'tdtpcli.exe'
if (-not (Test-Path $TdtpCli)) { $TdtpCli = Join-Path $RepoRoot 'tdtpcli' }
if (-not (Test-Path $TdtpCli)) {
    throw "tdtpcli not found at $TdtpCli. Build it: go build -o tdtpcli.exe ./cmd/tdtpcli/"
}

$StateDir = Join-Path $TravelDir 'state'
if (-not (Test-Path $StateDir)) { New-Item -ItemType Directory -Path $StateDir | Out-Null }

Set-Location $TravelDir

$args = @(
    '--scenarios',      './workflows',
    '--schedules-seed', './schedules',
    '--runners',        './orchestrator/runners.yaml',
    '--db',             './state/orchestrator.db',
    '--tmp',            './state',
    '--license',        './tdtp.lic',
    '--addr',           $Addr,
    '--no-auth'
)

Write-Host 'Travel Agency orchestrator' -ForegroundColor Cyan
Write-Host "  binary : $Orchestrator"
Write-Host "  api    : http://localhost$Addr"
Write-Host "  jobs   : http://localhost$Addr/jobs"
Write-Host ''

$proc = Start-Process -FilePath $Orchestrator -PassThru -NoNewWindow -ArgumentList $args
Write-Host "  started pid=$($proc.Id)" -ForegroundColor Green

if ($Approve) {
    # The API is not up the instant Start-Process returns. Poll rather than
    # sleep a guessed interval.
    $deadline = (Get-Date).AddSeconds(15)
    $ready = $false
    while ((Get-Date) -lt $deadline) {
        try {
            Invoke-RestMethod -Uri "http://localhost$Addr/healthz" -TimeoutSec 2 | Out-Null
            $ready = $true
            break
        } catch {
            Start-Sleep -Milliseconds 300
        }
    }
    if (-not $ready) { throw "orchestrator did not answer /healthz within 15s" }

    foreach ($name in @('travel-sync-out', 'travel-sync-reference')) {
        $a = Invoke-RestMethod -Method Post -Uri "http://localhost$Addr/scenarios/$name/approve"
        Write-Host ("  approved {0,-24} sha256={1}" -f $name, $a.sha256.Substring(0, 16)) -ForegroundColor Green
    }
}

Write-Host ''
Write-Host 'Schedules:' -ForegroundColor Cyan
Write-Host '  travel-sync-out        @every 15s   incremental, all six changing tables'
Write-Host '  travel-sync-reference  @every 5m    full reload, countries + tours'
Write-Host ''
Write-Host 'Stop with shutdown.ps1, or: Stop-Process -Id ' -NoNewline
Write-Host $proc.Id
