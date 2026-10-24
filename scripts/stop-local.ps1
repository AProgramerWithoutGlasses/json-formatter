$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$local = Join-Path $root '.local'
$pidPath = Join-Path $local 'server.pid'
if (Test-Path $pidPath) {
    $serverProcess = Get-Process -Id (Get-Content $pidPath) -ErrorAction SilentlyContinue
    if ($serverProcess -and $serverProcess.Path -eq (Join-Path $local 'json-studio.exe')) { Stop-Process -Id $serverProcess.Id }
}
Write-Host 'Local server stopped. Browser history is unchanged.'
