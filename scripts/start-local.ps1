param([int]$Port = 8080, [int]$MySQLPort = 13307)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$local = Join-Path $root '.local'
New-Item -ItemType Directory -Force $local | Out-Null
$mysql = (Get-Command mysqld.exe -ErrorAction Stop).Source
$mysqlClient = Join-Path (Split-Path $mysql) 'mysql.exe'
$config = Join-Path $local 'mysql.ini'
$data = Join-Path $local 'mysql-data'
$credentialsPath = Join-Path $local 'credentials.json'
$clientConfig = Join-Path $local 'mysql-client.ini'
function New-LocalPassword { return [Guid]::NewGuid().ToString('N') + [Guid]::NewGuid().ToString('N') }
function Invoke-LocalSQL([string]$sql, [switch]$Bootstrap) {
    if ($Bootstrap) { $sql | & $mysqlClient --no-defaults --protocol=TCP --host=127.0.0.1 "--port=$MySQLPort" --user=root --skip-password }
    else { $sql | & $mysqlClient "--defaults-extra-file=$clientConfig" }
    if ($LASTEXITCODE -ne 0) { throw 'Local MySQL setup failed. See .local/mysql-error.log.' }
}
if (!(Test-Path $credentialsPath)) {
    @{ root = (New-LocalPassword); app = (New-LocalPassword); port = $MySQLPort } | ConvertTo-Json | Set-Content -Encoding utf8 $credentialsPath
}
$credentials = Get-Content $credentialsPath -Raw | ConvertFrom-Json
if ($credentials.port -ne $MySQLPort) { throw 'This local database uses a different port. Reuse its original MySQLPort.' }
@"
[mysqld]
basedir=$( (Split-Path (Split-Path $mysql)).Replace('\','/') )
datadir=$( $data.Replace('\','/') )
port=$MySQLPort
bind-address=127.0.0.1
mysqlx=OFF
default-time-zone=+00:00
character-set-server=utf8mb4
log-error=$( (Join-Path $local 'mysql-error.log').Replace('\','/') )
pid-file=$( (Join-Path $local 'mysql.pid').Replace('\','/') )
"@ | Set-Content -Encoding ascii $config
if (!(Test-Path (Join-Path $data 'mysql'))) {
    Write-Host 'Initializing isolated MySQL data directory...'
    & $mysql "--defaults-file=$config" --initialize-insecure
    if ($LASTEXITCODE -ne 0) { throw 'MySQL initialization failed. See .local/mysql-error.log.' }
}
$listener = Get-NetTCPConnection -State Listen -LocalPort $MySQLPort -ErrorAction SilentlyContinue
if ($listener) {
    $owner = Get-CimInstance Win32_Process -Filter "ProcessId = $($listener[0].OwningProcess)"
    if (!$owner -or !$owner.CommandLine.Contains($config)) { throw "Port $MySQLPort is used by another service; refusing to touch it." }
}
if (!$listener) {
    Start-Process -FilePath $mysql -ArgumentList "--defaults-file=`"$config`"" -WindowStyle Hidden | Out-Null
}
for ($attempt = 0; $attempt -lt 40; $attempt++) {
    $tcp = [Net.Sockets.TcpClient]::new()
    try { $tcp.Connect('127.0.0.1', $MySQLPort); $tcp.Dispose(); break } catch { $tcp.Dispose(); Start-Sleep -Milliseconds 500 }
}
if ($attempt -eq 40) { throw 'MySQL did not become ready.' }
if (!(Test-Path (Join-Path $local 'db-ready'))) {
    $setup = @"
CREATE DATABASE IF NOT EXISTS json_formatter CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
CREATE USER IF NOT EXISTS 'json_formatter'@'127.0.0.1' IDENTIFIED BY '$($credentials.app)';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, INDEX ON json_formatter.* TO 'json_formatter'@'127.0.0.1';
ALTER USER 'root'@'localhost' IDENTIFIED BY '$($credentials.root)';
"@
    Invoke-LocalSQL $setup -Bootstrap
    'ready' | Set-Content (Join-Path $local 'db-ready')
}
@"
[client]
protocol=TCP
host=127.0.0.1
port=$MySQLPort
user=root
password=$($credentials.root)
default-character-set=utf8mb4
"@ | Set-Content -Encoding ascii $clientConfig
Invoke-LocalSQL 'SELECT 1;' | Out-Null
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
$env:MYSQL_DSN = "json_formatter:$($credentials.app)@tcp(127.0.0.1:$MySQLPort)/json_formatter?charset=utf8mb4"
$env:ADDR = "127.0.0.1:$Port"
$process = Start-Process -FilePath $server -WorkingDirectory $root -RedirectStandardOutput (Join-Path $local 'server-out.log') -RedirectStandardError (Join-Path $local 'server-error.log') -WindowStyle Hidden -PassThru
$process.Id | Set-Content $pidPath
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    if ($process.HasExited) { throw "Server stopped. Check .local/server-error.log." }
    try { $health = Invoke-RestMethod "http://127.0.0.1:$Port/api/health"; if ($health.status -eq 'ok') { break } } catch { Start-Sleep -Milliseconds 400 }
}
if ($attempt -eq 30) { throw 'Server health check failed.' }
Write-Host "JSON Studio is running at http://127.0.0.1:$Port"
Write-Host "Isolated MySQL: 127.0.0.1:$MySQLPort. Local data and credentials: .local/ (gitignored)."
