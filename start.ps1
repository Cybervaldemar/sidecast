$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
$lanplayOSArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
$lanplayBinaryName = if ($lanplayOSArch -eq 'Arm64') { 'lanplay-windows-arm64.exe' } else { 'lanplay-windows-amd64.exe' }
$lanplayBinary = Join-Path (Join-Path $PSScriptRoot 'build') $lanplayBinaryName
if (-not (Test-Path -LiteralPath $lanplayBinary)) {
    Write-Host 'Build LANPlay first: npm ci; npm run build:app' -ForegroundColor Yellow
    exit 1
}
Write-Host 'Open http://localhost:8080 in Chrome or Edge. Press Ctrl+C to stop.'
& $lanplayBinary --data (Join-Path $PSScriptRoot 'data')
exit $LASTEXITCODE
