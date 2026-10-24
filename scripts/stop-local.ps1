$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$local = Join-Path $root '.local'
$pidPath = Join-Path $local 'server.pid'
if (Test-Path $pidPath) {
    $serverProcess = Get-Process -Id (Get-Content $pidPath) -ErrorAction SilentlyContinue
    if ($serverProcess -and $serverProcess.Path -eq (Join-Path $local 'json-studio.exe')) { Stop-Process -Id $serverProcess.Id }
}
$clientConfig = Join-Path $local 'mysql-client.ini'
if (Test-Path $clientConfig) {
    $admin = Join-Path (Split-Path (Get-Command mysqld.exe).Source) 'mysqladmin.exe'
    & $admin "--defaults-extra-file=$clientConfig" shutdown
    if ($LASTEXITCODE -ne 0) { throw 'MySQL shutdown failed; it may already be stopped.' }
}
Write-Host 'Local services stopped. Data is retained in .local/.'
