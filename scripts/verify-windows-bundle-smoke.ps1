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
    foreach ($Word in @("发送心跳", "后台服务", "扫描服务端包", "安全同步世界快照", "上传同步制品", "拉取同步制品", "接管演练", "控制端", "本地主机代理", "术语说明")) {
        if (-not $GuiText.Contains($Word)) {
            throw "GUI wording smoke failed; missing $Word"
        }
    }
    $Redacted = $GuiText
    if (-not $GuiText.Contains("Protect-Text") -or -not $GuiText.Contains("ak_[已隐藏]")) {
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

    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop daemon start --app-data-dir $AppData --json | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "desktop daemon start failed"
    }
    $DaemonStatus = & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop daemon status --app-data-dir $AppData --json | ConvertFrom-Json
    if (-not $DaemonStatus.running) {
        throw "desktop daemon status did not report running"
    }
    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop daemon stop --app-data-dir $AppData --json | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "desktop daemon stop failed"
    }

    $ServerDir = Join-Path $TempRoot "fixture server"
    New-Item -ItemType Directory -Force -Path (Join-Path $ServerDir "world") | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $ServerDir "mods") | Out-Null
    Set-Content -Encoding UTF8 -Path (Join-Path $ServerDir "server.jar") -Value "fake jar"
    Set-Content -Encoding UTF8 -Path (Join-Path $ServerDir "mods\demo.jar") -Value "fake mod"
    Set-Content -Encoding UTF8 -Path (Join-Path $ServerDir "world\level.dat") -Value "fake world"
    Set-Content -Encoding UTF8 -Path (Join-Path $ServerDir "eula.txt") -Value "eula=true"
    Set-Content -Encoding UTF8 -Path (Join-Path $ServerDir "server.properties") -Value "enable-rcon=false`nserver-port=25565"

    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop inspect-server --server-dir $ServerDir --json | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "desktop inspect-server failed"
    }
    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop import-server --app-data-dir $AppData --server-dir $ServerDir | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "desktop import-server failed"
    }

    $RconStatus = & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop rcon-status --app-data-dir $AppData --json | ConvertFrom-Json
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
    if ($SafeSyncExit -eq 0 -or -not $SafeSyncOutput.Contains("RCON 未开启")) {
        throw "desktop safe-sync-world should block before password input when RCON is disabled"
    }

    $ErrorActionPreference = "Continue"
    $PushOutput = & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop push-latest --app-data-dir $AppData 2>&1 | Out-String
    $PushExit = $LASTEXITCODE
    $ErrorActionPreference = $OldErrorActionPreference
    if ($PushExit -eq 0 -or -not $PushOutput.Contains("current host")) {
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
