param([int]$Port = 8080)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$local = Join-Path $root '.local'
New-Item -ItemType Directory -Force $local | Out-Null
$server = Join-Path $local 'json-studio.exe'
$pidPath = Join-Path $local 'server.pid'
if (Test-Path $pidPath) {
    $previous = Get-Process -Id (Get-Content $pidPath) -ErrorAction SilentlyContinue
    if ($previous -and $previous.Path -eq $server) { Stop-Process -Id $previous.Id; $previous.WaitForExit(5000) | Out-Null }
}
$env:GOCACHE = Join-Path $local 'go-cache'
if (Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue) { throw "Port $Port is already in use. Choose another -Port." }
Push-Location $root
try {
    & go build -buildvcs=false -o $server ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
} finally { Pop-Location }
$env:ADDR = "127.0.0.1:$Port"
$process = Start-Process -FilePath $server -WorkingDirectory $root -RedirectStandardOutput (Join-Path $local 'server-out.log') -RedirectStandardError (Join-Path $local 'server-error.log') -WindowStyle Hidden -PassThru
$process.Id | Set-Content $pidPath
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    if ($process.HasExited) { throw "Server stopped. Check .local/server-error.log." }
    try { $health = Invoke-RestMethod "http://127.0.0.1:$Port/api/health"; if ($health.status -eq 'ok') { break } } catch { Start-Sleep -Milliseconds 400 }
}
if ($attempt -eq 30) { throw 'Server health check failed.' }
Write-Host "JSON Studio is running at http://127.0.0.1:$Port"

Write-Host "History is stored only in each browser. No database service is required."
