$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
$sidecastBinary = Join-Path $PSScriptRoot 'build\windows-amd64\sidecast-windows-amd64.exe'
if (-not (Test-Path -LiteralPath $sidecastBinary)) {
    Write-Host 'Build Sidecast first: npm ci; npm run build:app' -ForegroundColor Yellow
    exit 1
}
Start-Process -FilePath $sidecastBinary -ArgumentList @('--data', ('"' + (Join-Path $PSScriptRoot 'data') + '"')) -WorkingDirectory $PSScriptRoot -WindowStyle Hidden
Write-Host 'Sidecast is running in the system tray. The studio opens automatically. Use the studio shutdown button or Exit in the tray to stop.'
