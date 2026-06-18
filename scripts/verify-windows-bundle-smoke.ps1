# Smoke-test the Windows private desktop entrypoint without npm/pnpm/corepack install steps.
$ErrorActionPreference = "Stop"

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$TempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("acbh-bundle-smoke-" + [Guid]::NewGuid().ToString("N"))
$BundleRoot = Join-Path $TempRoot "bundle"
$AppData = Join-Path $TempRoot "appdata"

function Get-FreeTcpPort {
    $Listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Parse("127.0.0.1"), 0)
    $Listener.Start()
    try {
        return $Listener.LocalEndpoint.Port
    } finally {
        $Listener.Stop()
    }
}

function Invoke-Pnpm {
    param([Parameter(Mandatory = $true)][string[]]$PnpmArgs)

    if (Get-Command pnpm -ErrorAction SilentlyContinue) {
        & pnpm @PnpmArgs
    } elseif (Get-Command corepack -ErrorAction SilentlyContinue) {
        & corepack pnpm @PnpmArgs
    } else {
        throw "pnpm not found; install pnpm or use a full release bundle"
    }
    if ($LASTEXITCODE -ne 0) {
        throw "pnpm command failed: $($PnpmArgs -join ' ')"
    }
}

try {
    New-Item -ItemType Directory -Force -Path (Join-Path $BundleRoot "coordinator\dist") | Out-Null

    Push-Location (Join-Path $RepoRoot "apps\coordinator")
    try {
        Invoke-Pnpm -PnpmArgs @("build")
    } finally {
        Pop-Location
    }

    Push-Location (Join-Path $RepoRoot "agent")
    try {
        go build -o (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") .
        go build -o (Join-Path $BundleRoot "acbh-desktop-windows-amd64.exe") .\cmd\acbh-desktop
    } finally {
        Pop-Location
    }

    Copy-Item -Recurse -Force (Join-Path $RepoRoot "apps\coordinator\dist\*") (Join-Path $BundleRoot "coordinator\dist")
    Copy-Item -Force (Join-Path $RepoRoot "apps\coordinator\package.json") (Join-Path $BundleRoot "coordinator\package.json")
    New-Item -ItemType Directory -Force -Path (Join-Path $BundleRoot "scripts") | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $BundleRoot "docs") | Out-Null
    Copy-Item -Force (Join-Path $RepoRoot "scripts\acbh-desktop-gui.ps1") (Join-Path $BundleRoot "scripts\acbh-desktop-gui.ps1")
    Copy-Item -Recurse -Force (Join-Path $RepoRoot "docs\zh-CN") (Join-Path $BundleRoot "docs\zh-CN")
    $GuiText = Get-Content -Raw -Encoding UTF8 -Path (Join-Path $BundleRoot "scripts\acbh-desktop-gui.ps1")
    $ParseErrors = $null
    $null = [System.Management.Automation.PSParser]::Tokenize(
        $GuiText,
        [ref]$ParseErrors
    )
    if ($ParseErrors -and $ParseErrors.Count -gt 0) {
        throw "GUI script parse failed: $($ParseErrors[0].Message)"
    }
    foreach ($ForbiddenPattern in @(
        '\.DoWork\s*(\+)?\s*=',
        '\.RunWorkerCompleted\s*(\+)?\s*=',
        '\.ProgressChanged\s*(\+)?\s*=',
        'Task\.Run',
        'ThreadPool',
        'New-Object\s+System\.ComponentModel\.BackgroundWorker',
        '\[System\.Threading\.Thread\]',
        '\.BeginInvoke\('
    )) {
        if ($GuiText -match $ForbiddenPattern) {
            throw "GUI event binding smoke failed; forbidden pattern: $ForbiddenPattern"
        }
    }
    foreach ($RequiredText in @("Invoke-AgentCommandSafe", "Redact-Secrets", "try", "catch", "[System.Diagnostics.ProcessStartInfo]::new()", ".ArgumentList.Add(", "ConvertFrom-JsonSafe", "StartsWith", "desktop status --json")) {
        if (-not $GuiText.Contains($RequiredText)) {
            throw "GUI safe command smoke failed; missing $RequiredText"
        }
    }
    foreach ($Word in @("发送心跳", "后台服务", "扫描服务端包", "安全同步世界快照", "上传同步制品", "拉取同步制品", "接管演练", "控制端", "本地主机代理", "术语说明", "启动 MC 服务端", "desktop start-server")) {
        if (-not $GuiText.Contains($Word)) {
            throw "GUI wording smoke failed; missing $Word"
        }
    }
    $Redacted = $GuiText
    if (-not $GuiText.Contains("Redact-Secrets") -or -not $GuiText.Contains("ak_[已隐藏]")) {
        throw "GUI log redaction helper is missing"
    }
    Push-Location (Join-Path $BundleRoot "coordinator")
    try {
        Invoke-Pnpm -PnpmArgs @("install", "--prod", "--offline")
    } finally {
        Pop-Location
    }

    $PackageJson = Get-Content -Raw -Path (Join-Path $BundleRoot "coordinator\package.json") | ConvertFrom-Json
    if (-not $PackageJson.dependencies.ws) {
        throw "ws is not present in coordinator production dependencies"
    }

    $Port = Get-FreeTcpPort
    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop start `
        --app-data-dir $AppData `
        --coordinator (Join-Path $BundleRoot "coordinator\dist\index.js") `
        --port "$Port"
    if ($LASTEXITCODE -ne 0) {
        throw "desktop start failed"
    }

    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop status --app-data-dir $AppData --port "$Port" --json | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "desktop status failed"
    }

    # hard smoke for hotfix3: status --json must be pure json, no ACBH prefix, stdout only
    $statusStdout = Join-Path $BundleRoot "status.stdout.txt"
    $statusStderr = Join-Path $BundleRoot "status.stderr.txt"
    # force utf8 for redirect so unicode json (chinese in publicEntryMessage) not mangled by console cp
    cmd /c "chcp 65001 >nul && `"$(Join-Path $BundleRoot 'acbh-agent-windows-amd64.exe')`" desktop status --app-data-dir `"$AppData`" --port `"$Port`" --json 1>`"$statusStdout`" 2>`"$statusStderr`""
    $soBytes = [System.IO.File]::ReadAllBytes($statusStdout)
    $so = [System.Text.Encoding]::UTF8.GetString($soBytes)
    $seBytes = [System.IO.File]::ReadAllBytes($statusStderr)
    $se = [System.Text.Encoding]::UTF8.GetString($seBytes)
    $soTrim = $so.Trim()
    if (-not $soTrim.StartsWith("{")) {
        throw "status --json stdout must start with { , got: $($soTrim.Substring(0,[Math]::Min(20,$soTrim.Length)))"
    }
    try { $null = $soTrim | ConvertFrom-Json } catch { throw "status --json stdout not valid JSON: $($_.Exception.Message)  raw starts: $($soTrim.Substring(0,100))" }
    if ($soTrim -like "ACBH*") { throw "status --json stdout must not start with ACBH" }
    if ($so -match "ACBH 私人桌面模式状态") { throw "status --json must not contain chinese plain status text" }
    if ($se -and $se.Trim() -ne "") {
        # stderr may have warnings but for clean status expect empty or logged separate; allow but not pollute json
    }
    $statusJson = $soTrim | ConvertFrom-Json
    if (-not $statusJson.publicEntryStatus) { throw "status json must contain publicEntryStatus" }
    if ($statusJson.publicEntryStatus -ne "not_configured") { throw "in private mode publicEntryStatus must be not_configured" }
    # no secrets
    $statusRaw = $soTrim
    if ($statusRaw -match "(?i)accessKey|hostToken|rcon\.password") { throw "status json leaked secret (accessKey/hostToken/rcon password)" }

    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop daemon start --app-data-dir $AppData --json | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "desktop daemon start failed"
    }
    $dstatusStd = Join-Path $BundleRoot "dstatus.stdout.txt"
    cmd /c "chcp 65001 >nul && `"$(Join-Path $BundleRoot 'acbh-agent-windows-amd64.exe')`" desktop daemon status --app-data-dir `"$AppData`" --json 1>`"$dstatusStd`" "
    $dBytes = [System.IO.File]::ReadAllBytes($dstatusStd)
    $dText = [System.Text.Encoding]::UTF8.GetString($dBytes).Trim()
    $DaemonStatus = $dText | ConvertFrom-Json
    if (-not $DaemonStatus.running) {
        throw "desktop daemon status did not report running"
    }
    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop daemon stop --app-data-dir $AppData --json | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "desktop daemon stop failed"
    }

    $ServerDir = Join-Path $TempRoot "fixture-server"
    New-Item -ItemType Directory -Force -Path (Join-Path $ServerDir "world") | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $ServerDir "mods") | Out-Null
    Set-Content -Encoding UTF8 -Path (Join-Path $ServerDir "server.jar") -Value "fake jar"
    Set-Content -Encoding UTF8 -Path (Join-Path $ServerDir "mods\demo.jar") -Value "fake mod"
    Set-Content -Encoding UTF8 -Path (Join-Path $ServerDir "world\level.dat") -Value "fake world"
    Set-Content -Encoding UTF8 -Path (Join-Path $ServerDir "eula.txt") -Value "eula=true"
    Set-Content -Encoding UTF8 -Path (Join-Path $ServerDir "server.properties") -Value "enable-rcon=false`nserver-port=25565"
    if (-not (Test-Path (Join-Path $ServerDir "server.jar"))) { throw "fixture server.jar not created" }

    $inspRaw = & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop inspect-server --server-dir $ServerDir --json 2>&1
    $inspExit = $LASTEXITCODE
    $inspJson = $inspRaw | Out-String
    if ($inspExit -ne 0) {
        throw "desktop inspect-server failed"
    }
    $impRaw = & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop import-server --app-data-dir $AppData --server-dir $ServerDir 2>&1
    $impExit = $LASTEXITCODE
    $impOut = $impRaw | Out-String
    if ($impExit -ne 0) {
        throw "desktop import-server failed"
    }

    # hotfix3 MC start preflight smoke: no-jar (copy good then delete the jar file, point config to copy)
    $BadNoJar = Join-Path $TempRoot "bad-server-no-jar"
    if (Test-Path $BadNoJar) { Remove-Item -Recurse -Force $BadNoJar }
    Copy-Item -Recurse -Force $ServerDir $BadNoJar
    Remove-Item (Join-Path $BadNoJar "server.jar") -Force -ErrorAction SilentlyContinue
    $cfgPath = Join-Path $AppData "config.yaml"
    $cfg = Get-Content -Raw -Encoding UTF8 -Path $cfgPath | ConvertFrom-Json
    if (-not $cfg.server) { $cfg | Add-Member -NotePropertyName server -NotePropertyValue (@{}) -Force }
    $cfg.server.dir = $BadNoJar
    $cfg.server.command = "java -jar no.jar nogui"
    $json = $cfg | ConvertTo-Json -Depth 5
    $utf8NoBom = New-Object System.Text.UTF8Encoding $false
    [IO.File]::WriteAllText($cfgPath, $json, $utf8NoBom)
    $OldErr = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $noJarOut = & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop start-server --app-data-dir $AppData --json 2>&1 | Out-String
    $ErrorActionPreference = $OldErr
    if (-not ($noJarOut -match "missing_jar|fabric-server-launch.jar|找不到服务端 jar")) {
        throw "start-server for no-jar fixture must return chinese '找不到服务端 jar' or missing_jar; got: $noJarOut"
    }

    # hotfix3 MC start preflight smoke: eula=false
    $BadEula = Join-Path $TempRoot "bad-server-eula-false"
    New-Item -ItemType Directory -Force -Path $BadEula | Out-Null
    Set-Content -Encoding UTF8 -Path (Join-Path $BadEula "server.jar") -Value "dummy"
    Set-Content -Encoding UTF8 -Path (Join-Path $BadEula "eula.txt") -Value "eula=false"
    $OldErr = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop import-server --app-data-dir $AppData --server-dir $BadEula 2>$null | Out-Null
    $ErrorActionPreference = $OldErr
    $OldErr = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $eulaOut = & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop start-server --app-data-dir $AppData --json 2>&1 | Out-String
    $ErrorActionPreference = $OldErr
    if (-not ($eulaOut -match "eula_false|eula.txt 设置|Minecraft EULA 未确认")) {
        throw "start-server for eula-false must return chinese 'Minecraft EULA 未确认' or eula_false; got: $eulaOut"
    }

    # restore good server dir for subsequent tests (scan etc)
    $restoreRaw = & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop import-server --app-data-dir $AppData --server-dir $ServerDir 2>&1
    $restoreExit = $LASTEXITCODE
    $restoreOut = $restoreRaw | Out-String
    if ($restoreExit -ne 0) {
        throw "restore import failed after bad eula"
    }

    $rconStd = Join-Path $BundleRoot "rcon.stdout.txt"
    cmd /c "chcp 65001 >nul && `"$(Join-Path $BundleRoot 'acbh-agent-windows-amd64.exe')`" desktop rcon-status --app-data-dir `"$AppData`" --json 1>`"$rconStd`" "
    $rBytes = [System.IO.File]::ReadAllBytes($rconStd)
    $rText = [System.Text.Encoding]::UTF8.GetString($rBytes).Trim()
    $RconStatus = $rText | ConvertFrom-Json
    if ($RconStatus.enabled -or -not $RconStatus.message.Contains("RCON 未开启")) {
        throw "desktop rcon-status did not explain disabled RCON in Chinese"
    }

    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop scan-pack --app-data-dir $AppData --json | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "desktop scan-pack failed"
    }
    $LatestManifest = & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop latest-manifest --app-data-dir $AppData --json | ConvertFrom-Json
    if (-not (Test-Path $LatestManifest.path) -or $LatestManifest.artifactKind -ne "server-pack") {
        throw "desktop scan-pack did not create latest server-pack manifest"
    }

    $OldErrorActionPreference = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $SafeSyncOutput = & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop safe-sync-world --app-data-dir $AppData 2>&1 | Out-String
    $SafeSyncExit = $LASTEXITCODE
    $ErrorActionPreference = $OldErrorActionPreference
    if ($SafeSyncExit -eq 0 -or -not ($SafeSyncOutput -match "RCON|未开启|password|safe-sync")) {
        throw "desktop safe-sync-world should block before password input when RCON is disabled"
    }

    $ErrorActionPreference = "Continue"
    $PushOutput = & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop push-latest --app-data-dir $AppData 2>&1 | Out-String
    $PushExit = $LASTEXITCODE
    $ErrorActionPreference = $OldErrorActionPreference
    if ($PushExit -eq 0 -or -not ($PushOutput -match "current host|not current")) {
        throw "desktop push-latest should block before upload when host is not current host"
    }

    foreach ($RequiredPath in @(
        "scripts\acbh-desktop-gui.ps1",
        "docs\zh-CN\windows-private-desktop-quickstart.md",
        "coordinator\package.json",
        "coordinator\node_modules\ws\package.json",
        "acbh-desktop-windows-amd64.exe",
        "acbh-agent-windows-amd64.exe"
    )) {
        if (-not (Test-Path (Join-Path $BundleRoot $RequiredPath))) {
            throw "bundle smoke missing $RequiredPath"
        }
    }

    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop stop --app-data-dir $AppData --port "$Port"
    if ($LASTEXITCODE -ne 0) {
        throw "desktop stop failed"
    }

    Write-Host "Windows private desktop bundle smoke passed." -ForegroundColor Green
} finally {
    try {
        $AgentExe = Join-Path $BundleRoot "acbh-agent-windows-amd64.exe"
        if (Test-Path $AgentExe) {
            & $AgentExe desktop daemon stop --app-data-dir $AppData 2>$null | Out-Null
            & $AgentExe desktop stop --app-data-dir $AppData --port "$Port" 2>$null | Out-Null
        }
    } catch {
    }
    Start-Sleep -Milliseconds 500
    if (Test-Path $TempRoot) {
        Remove-Item -Recurse -Force $TempRoot -ErrorAction SilentlyContinue
    }
}
