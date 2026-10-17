$ErrorActionPreference = 'Stop'
$lanplayRecord = Join-Path $PSScriptRoot '.runtime\v2-server.json'
if (-not (Test-Path -LiteralPath $lanplayRecord)) {
    Write-Host 'No background LANPlay v2 process is recorded. Use Ctrl+C in its terminal.'
    exit 0
}
$lanplaySaved = Get-Content -LiteralPath $lanplayRecord -Raw | ConvertFrom-Json
$lanplayProcess = Get-Process -Id $lanplaySaved.processId -ErrorAction SilentlyContinue
if (-not $lanplayProcess) { Write-Host 'LANPlay v2 is already stopped.'; exit 0 }
$lanplayExpectedBinaries = @((Join-Path $PSScriptRoot 'build\lanplay-windows-amd64.exe'), (Join-Path $PSScriptRoot 'build\lanplay-windows-arm64.exe'))
if ($lanplayProcess.Path -notin $lanplayExpectedBinaries -or $lanplayProcess.StartTime.ToUniversalTime().ToString('o') -ne $lanplaySaved.startedUtc) {
    throw 'Saved process does not match this LANPlay v2 binary and start time. Refusing to stop it.'
}
Stop-Process -Id $lanplayProcess.Id
Write-Host 'LANPlay v2 stopped.'
