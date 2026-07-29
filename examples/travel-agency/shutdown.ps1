#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Graceful shutdown of travel-agency infrastructure.

.DESCRIPTION
    Order matters: stop producing before stopping consuming, so nothing is
    still arriving when the listeners go down.

      1. activity.py     -- stop simulating changes
      2. orchestrator    -- stop triggering exports (no new packets enqueued)
      3. drain           -- let the listeners finish what is already queued
      4. listeners       -- stop tdtpcli --map --listen
      5. purge           -- whatever survived the drain timeout

    Two stages from the earlier version are gone with the scripts they served:
    coordinator.py (replaced by the orchestrator) and its tdtp.coordinator
    queue, and consumer.py (replaced by listeners.ps1).

    An interrupted listener is safe: --listen acknowledges a message only
    after the upsert commits, so anything in flight returns to the queue
    rather than vanishing. A purge here does discard messages -- it runs only
    after the drain timeout, and says how many.

.PARAMETER DrainTimeoutSec
    Seconds to wait for the data queues to empty before force-purging
    (default 60).

.PARAMETER Force
    Skip the drain: kill everything and purge all queues immediately.

.EXAMPLE
    ./shutdown.ps1
    ./shutdown.ps1 -Force
    ./shutdown.ps1 -DrainTimeoutSec 30
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

function Get-AllDepths($queues) {
    $raw = docker exec $RABBIT_CONTAINER rabbitmqctl list_queues name messages 2>$null
    $map = @{}
    foreach ($q in $queues) {
        $line = $raw | Where-Object { $_ -match "^$([regex]::Escape($q))\s" }
        if ($line -match "\s(\d+)$") { $map[$q] = [int]$Matches[1] } else { $map[$q] = 0 }
    }
    return $map
}

function Stop-PythonByScript($pattern, $label) {
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

function Stop-ByImage($name, $label) {
    $procs = Get-Process $name -ErrorAction SilentlyContinue
    if ($procs) {
        $procs | Stop-Process -Force -ErrorAction SilentlyContinue
        Write-Host "         Stopped $($procs.Count) $label process(es)" -ForegroundColor Green
    } else {
        Write-Host "         No $label processes found" -ForegroundColor DarkGray
    }
}

# -----------------------------------------------------------------------------
Write-Host ""
Write-Host "=== Travel Agency Graceful Shutdown ===" -ForegroundColor Cyan
Write-Host ""

if ($Force) {
    Write-Host "[ FORCE ] Killing all processes and purging queues..." -ForegroundColor Red
    Stop-PythonByScript "activity\.py" "activity"
    Stop-ByImage "orchestrator" "orchestrator"
    Stop-ByImage "tdtpcli" "listener"
    Stop-ByImage "xzmercury-mock" "mercury"
    foreach ($q in $DATA_QUEUES) {
        docker exec $RABBIT_CONTAINER rabbitmqctl purge_queue $q 2>$null | Out-Null
        Write-Host "         Purged: $q" -ForegroundColor DarkYellow
    }
    Write-Host "`nDone (forced)." -ForegroundColor Green
    exit 0
}

# -- Step 1: stop the simulation ----------------------------------------------
Write-Host "[ 1/4 ] Stopping activity nodes..." -ForegroundColor Yellow
Stop-PythonByScript "activity\.py" "activity"
Start-Sleep -Seconds 1

# -- Step 2: stop triggering exports ------------------------------------------
# Before the drain, not after: the orchestrator is what puts new packets on the
# queues, so draining while it is still running is a race it wins.
Write-Host "[ 2/4 ] Stopping orchestrator..." -ForegroundColor Yellow
Stop-ByImage "orchestrator" "orchestrator"

# -- Step 3: drain the data queues --------------------------------------------
Write-Host "[ 3/4 ] Draining data queues..." -ForegroundColor Yellow
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

# -- Step 4: stop the listeners, purge the remainder ---------------------------
Write-Host "[ 4/4 ] Stopping listeners..." -ForegroundColor Yellow
Stop-ByImage "tdtpcli" "listener"
# Mercury last: a listener still draining needs it to decrypt what it holds.
Stop-ByImage "xzmercury-mock" "mercury"

$depths = Get-AllDepths $DATA_QUEUES
foreach ($q in $DATA_QUEUES) {
    if ($depths[$q] -gt 0) {
        docker exec $RABBIT_CONTAINER rabbitmqctl purge_queue $q 2>$null | Out-Null
        Write-Host "         Purged $q ($($depths[$q]) msg(s))" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "Final queue state:" -ForegroundColor Cyan
$all = Get-AllDepths $DATA_QUEUES
foreach ($q in $DATA_QUEUES) {
    $d = $all[$q]
    $color = if ($d -eq 0) { "Green" } else { "Red" }
    Write-Host ("  {0,-42} {1} msg(s)" -f $q, $d) -ForegroundColor $color
}
Write-Host ""
Write-Host "Checkpoints in state/ are left alone: they are the resume point," -ForegroundColor DarkGray
Write-Host "not runtime state. Delete them only to force a full re-sync." -ForegroundColor DarkGray
Write-Host ""
Write-Host "Shutdown complete." -ForegroundColor Green
