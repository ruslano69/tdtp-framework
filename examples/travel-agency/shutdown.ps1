#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Graceful shutdown of travel-agency infrastructure.
    Order: activity nodes → coordinator queue drain → stop coordinator
           → data queue drain → stop consumers → purge leftovers.

.PARAMETER DrainTimeoutSec
    Seconds to wait for each drain stage before force-purging (default 60).

.PARAMETER Force
    Skip drain, kill everything and purge all queues immediately.

.EXAMPLE
    .\shutdown.ps1
    .\shutdown.ps1 -Force
    .\shutdown.ps1 -DrainTimeoutSec 30
#>
param(
    [int]$DrainTimeoutSec = 60,
    [switch]$Force
)

$RABBIT_CONTAINER = "tdtp-rabbitmq"

$DATA_QUEUES = @(
    "tdtp.sync.flights",
    "tdtp.sync.reservations",
    "tdtp.sync.countries",
    "tdtp.sync.guides",
    "tdtp.sync.tours",
    "tdtp.sync.schedule",
    "tdtp.sync.branch.customers",
    "tdtp.sync.branch.sales"
)
$COORD_QUEUE = "tdtp.coordinator"

function Get-QueueDepth($queue) {
    $out = docker exec $RABBIT_CONTAINER rabbitmqctl list_queues name messages 2>$null |
           Where-Object { $_ -match "^$([regex]::Escape($queue))\s" }
    if ($out -match "\s(\d+)$") { return [int]$Matches[1] }
    return 0
}

function Get-AllDepths($queues) {
    $raw = docker exec $RABBIT_CONTAINER rabbitmqctl list_queues name messages 2>$null
    $map = @{}
    foreach ($q in $queues) {
        $line = $raw | Where-Object { $_ -match "^$([regex]::Escape($q))\s" }
        if ($line -match "\s(\d+)$") { $map[$q] = [int]$Matches[1] } else { $map[$q] = 0 }
    }
    return $map
}

function Stop-ByScript($pattern, $label) {
    $killed = 0
    $procs = Get-WmiObject Win32_Process -Filter "Name='python.exe'" -ErrorAction SilentlyContinue
    foreach ($p in $procs) {
        if ($p.CommandLine -match $pattern) {
            Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
            $killed++
        }
    }
    if ($killed -gt 0) {
        Write-Host "         Stopped $killed $label process(es)" -ForegroundColor Green
    } else {
        Write-Host "         No $label processes found" -ForegroundColor DarkGray
    }
}

function Drain-Queue($queue, $label) {
    $elapsed = 0
    while ($elapsed -lt $DrainTimeoutSec) {
        $d = Get-QueueDepth $queue
        if ($d -eq 0) { Write-Host "         $label empty" -ForegroundColor Green; return }
        Write-Host "         $label : $d msg(s)..." -ForegroundColor DarkCyan
        Start-Sleep -Seconds 3
        $elapsed += 3
    }
    $d = Get-QueueDepth $queue
    if ($d -gt 0) {
        Write-Host "         Timeout -- purging $queue ($d messages)" -ForegroundColor Red
        docker exec $RABBIT_CONTAINER rabbitmqctl purge_queue $queue 2>$null | Out-Null
    }
}

# ─────────────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "=== Travel Agency Graceful Shutdown ===" -ForegroundColor Cyan
Write-Host ""

# ── Force mode ────────────────────────────────────────────────────────────────
if ($Force) {
    Write-Host "[ FORCE ] Killing all processes and purging queues..." -ForegroundColor Red
    Stop-ByScript "activity\.py"   "activity"
    Stop-ByScript "coordinator\.py" "coordinator"
    Stop-ByScript "consumer\.py"   "consumer"
    foreach ($q in $DATA_QUEUES + $COORD_QUEUE) {
        docker exec $RABBIT_CONTAINER rabbitmqctl purge_queue $q 2>$null | Out-Null
        Write-Host "         Purged: $q" -ForegroundColor DarkYellow
    }
    Write-Host "`nDone (forced)." -ForegroundColor Green
    exit 0
}

# ── Step 1: Stop activity nodes ───────────────────────────────────────────────
Write-Host "[ 1/4 ] Stopping activity nodes..." -ForegroundColor Yellow
Stop-ByScript "activity\.py" "activity"
Start-Sleep -Seconds 1

# ── Step 2: Drain coordinator queue ───────────────────────────────────────────
Write-Host "[ 2/4 ] Draining coordinator queue..." -ForegroundColor Yellow
Drain-Queue $COORD_QUEUE "tdtp.coordinator"

# ── Step 3: Stop coordinator, drain data queues ───────────────────────────────
Write-Host "[ 3/4 ] Stopping coordinator, draining data queues..." -ForegroundColor Yellow
Stop-ByScript "coordinator\.py" "coordinator"

$elapsed = 0
while ($elapsed -lt $DrainTimeoutSec) {
    $depths = Get-AllDepths $DATA_QUEUES
    $total  = ($depths.Values | Measure-Object -Sum).Sum
    if ($total -eq 0) { Write-Host "         All data queues empty" -ForegroundColor Green; break }
    $pending = ($depths.GetEnumerator() | Where-Object { $_.Value -gt 0 } |
                ForEach-Object { "$($_.Key)=$($_.Value)" }) -join ", "
    Write-Host "         Pending: $pending" -ForegroundColor DarkCyan
    Start-Sleep -Seconds 3
    $elapsed += 3
}
# Purge anything left
$depths = Get-AllDepths $DATA_QUEUES
foreach ($q in $DATA_QUEUES) {
    if ($depths[$q] -gt 0) {
        docker exec $RABBIT_CONTAINER rabbitmqctl purge_queue $q 2>$null | Out-Null
        Write-Host "         Purged $q ($($depths[$q]) msg(s))" -ForegroundColor Red
    }
}

# ── Step 4: Stop consumers ────────────────────────────────────────────────────
Write-Host "[ 4/4 ] Stopping consumers..." -ForegroundColor Yellow
Stop-ByScript "consumer\.py" "consumer"

# ── Summary ───────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "Final queue state:" -ForegroundColor Cyan
$all = Get-AllDepths ($DATA_QUEUES + $COORD_QUEUE)
foreach ($q in $DATA_QUEUES + $COORD_QUEUE) {
    $d = $all[$q]
    $color = if ($d -eq 0) { "Green" } else { "Red" }
    Write-Host ("  {0,-42} {1} msg(s)" -f $q, $d) -ForegroundColor $color
}
Write-Host ""
Write-Host "Shutdown complete." -ForegroundColor Green
