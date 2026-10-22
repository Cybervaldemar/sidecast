$ErrorActionPreference = 'Stop'
$sidecastExpectedBinaries = @(
    (Join-Path $PSScriptRoot 'build\windows-amd64\sidecast-windows-amd64.exe')
)
$sidecastProcesses = @(Get-Process -Name 'sidecast-windows-*' -ErrorAction SilentlyContinue | Where-Object { $_.Path -in $sidecastExpectedBinaries })
if (-not $sidecastProcesses.Count) {
    Write-Host 'Sidecast is already stopped.'
    exit 0
}
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class SidecastTray {
    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    public static extern IntPtr FindWindowEx(IntPtr parent, IntPtr after, string className, string title);
    [DllImport("user32.dll")]
    public static extern uint GetWindowThreadProcessId(IntPtr window, out uint processId);
    [DllImport("user32.dll")]
    public static extern bool PostMessage(IntPtr window, uint message, IntPtr wParam, IntPtr lParam);
}
'@
$sidecastWindow = [IntPtr]::Zero
$sidecastClosed = @()
while ($true) {
    $sidecastWindow = [SidecastTray]::FindWindowEx([IntPtr]::Zero, $sidecastWindow, 'Sidecast.Tray', 'Sidecast')
    if ($sidecastWindow -eq [IntPtr]::Zero) { break }
    [uint32]$sidecastWindowProcessId = 0
    [void][SidecastTray]::GetWindowThreadProcessId($sidecastWindow, [ref]$sidecastWindowProcessId)
    if ($sidecastWindowProcessId -in $sidecastProcesses.Id) {
        if (-not [SidecastTray]::PostMessage($sidecastWindow, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero)) {
            throw 'Cannot send Exit to the Sidecast tray window.'
        }
        $sidecastClosed += $sidecastWindowProcessId
    }
}
foreach ($sidecastProcess in $sidecastProcesses) {
    if ($sidecastProcess.Id -notin $sidecastClosed) {
        Write-Warning 'Sidecast has no tray window. Stop its service or console process directly.'
    } elseif (-not $sidecastProcess.WaitForExit(10000)) {
        throw 'Sidecast did not finish shutting down within 10 seconds.'
    }
}
if ($sidecastClosed.Count) { Write-Host 'Sidecast stopped.' }
