#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Starts the import listeners for one node.

.DESCRIPTION
    Replaces consumer.py. That script subscribed to a Redis channel and, on each
    notification, launched `tdtpcli --map` as a fresh process -- reconnecting to
    RabbitMQ every time, and acknowledging nothing.

    tdtpcli does this itself:

        tdtpcli --map <mapping> --input broker://<queue> --listen

    One long-lived connection per queue, and the message is ACKed only after the
    upsert succeeds. If the process dies mid-write the message returns to the
    queue instead of being lost -- consumer.py had already consumed it by then.

    The Redis notification disappears as a concept: the queue is the signal.
    Publishing "there is something in the queue" only made sense while the
    consumer was not listening to the queue.

    Auditing is no longer the launcher's business either. consumer.py wrote a
    small JSON marker to S3 after each sync; tdtpcli writes a proper audit
    record per message (see the audit: section of the node config), to a
    database kept deliberately separate from the one being written to.

.PARAMETER Node
    Which set of queues to serve: branch or central.

.PARAMETER TdtpCli
    Path to the tdtpcli binary. Defaults to the repository root build.

.PARAMETER Config
    Node config passed to every listener -- supplies broker credentials and the
    audit sink. Defaults to configs/config_<node>.yaml.

.EXAMPLE
    ./listeners.ps1 -Node branch
    ./listeners.ps1 -Node central

.NOTES
    One process per queue, so a stuck entity cannot hold up the rest -- with
    consumer.py a single slow upsert blocked every other queue on that node.
    Stop them with shutdown.ps1, or Ctrl+C in each window: SIGTERM finishes the
    message in flight before exiting.
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('branch', 'central')]
    [string]$Node,

    [string]$TdtpCli,
    [string]$Config
)

$ErrorActionPreference = 'Stop'

$TravelDir = $PSScriptRoot
# Split-Path twice rather than Join-Path with three arguments: the multi-segment
# form of Join-Path is PowerShell 6+, and this repository is run under Windows
# PowerShell 5.1, where it fails with "positional parameter not found".
$RepoRoot = Split-Path (Split-Path $TravelDir -Parent) -Parent

if (-not $TdtpCli) {
    $TdtpCli = Join-Path $RepoRoot 'tdtpcli.exe'
    if (-not (Test-Path $TdtpCli)) { $TdtpCli = Join-Path $RepoRoot 'tdtpcli' }
}
if (-not (Test-Path $TdtpCli)) {
    throw "tdtpcli not found at $TdtpCli. Build it: go build -o tdtpcli.exe ./cmd/tdtpcli/"
}

if (-not $Config) {
    $Config = Join-Path $TravelDir "configs/config_$Node.yaml"
}
if (-not (Test-Path $Config)) {
    throw "node config not found: $Config"
}

# Which entities each node receives. Mirrors NODE_QUEUES from the consumer.py
# this replaces; the queue names themselves live in the mapping files, so they
# are named once, not here as well.
$NodeMappings = @{
    'branch'  = @('sync_countries', 'sync_guides', 'sync_tours', 'sync_schedule')
    'central' = @('sync_flights', 'sync_reservations', 'sync_branch_customers', 'sync_branch_sales')
}

$MappingsDir = Join-Path $RepoRoot 'mappings'
$started = @()

Write-Host "Starting $Node listeners" -ForegroundColor Cyan
Write-Host "  tdtpcli : $TdtpCli"
Write-Host "  config  : $Config"
Write-Host ""

foreach ($name in $NodeMappings[$Node]) {
    $mapping = Join-Path $MappingsDir "$name.yaml"
    if (-not (Test-Path $mapping)) {
        Write-Host "  skip  $name -- mapping not found: $mapping" -ForegroundColor Yellow
        continue
    }

    # The queue is read from the mapping's input_source.broker.queue, so it is
    # defined in exactly one place. --input broker:// with an empty name means
    # "whatever the mapping says".
    $queue = (Select-String -Path $mapping -Pattern '^\s*queue:\s*(\S+)' |
              Select-Object -First 1).Matches.Groups[1].Value

    $proc = Start-Process -FilePath $TdtpCli -PassThru -NoNewWindow -ArgumentList @(
        '--config', $Config,
        '--map',    $mapping,
        '--input',  "broker://$queue",
        '--listen'
    )

    $started += [pscustomobject]@{ Entity = $name; Queue = $queue; Pid = $proc.Id }
    Write-Host ("  started {0,-26} queue={1,-30} pid={2}" -f $name, $queue, $proc.Id) -ForegroundColor Green
}

if (-not $started) {
    throw "no listeners started for node $Node"
}

Write-Host ""
Write-Host "$($started.Count) listener(s) running. Stop with shutdown.ps1 or Ctrl+C." -ForegroundColor Cyan
$started | Format-Table -AutoSize
