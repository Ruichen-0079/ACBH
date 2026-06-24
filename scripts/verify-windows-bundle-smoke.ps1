# Smoke-test the Windows desktop bundle without global installs during runtime.
$ErrorActionPreference = "Stop"

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$TempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("acbh-bundle-smoke-" + [Guid]::NewGuid().ToString("N"))
$BundleRoot = Join-Path $TempRoot "bundle"
$AppData = Join-Path $TempRoot "appdata-owner"
$JoinAppData = Join-Path $TempRoot "appdata-joiner"
$Port = $null

function Get-FreeTcpPort {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Parse("127.0.0.1"), 0)
    $listener.Start()
    try {
        return $listener.LocalEndpoint.Port
    } finally {
        $listener.Stop()
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

function Write-Utf8NoBom {
    param([Parameter(Mandatory = $true)][string]$Path, [Parameter(Mandatory = $true)][string]$Text)
    $encoding = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($Path, $Text, $encoding)
}

function Copy-PowerShellUtf8Bom {
    param([Parameter(Mandatory = $true)][string]$Source, [Parameter(Mandatory = $true)][string]$Target)
    $text = [System.IO.File]::ReadAllText($Source, [System.Text.Encoding]::UTF8)
    $encoding = [System.Text.UTF8Encoding]::new($true)
    [System.IO.File]::WriteAllText($Target, $text, $encoding)
}

function Quote-ProcessArgument {
    param([string]$Arg)
    if ($null -eq $Arg) { return '""' }
    $value = [string]$Arg
    if ($value.Length -eq 0) { return '""' }
    if ($value -notmatch '[\s"]') { return $value }
    $builder = New-Object System.Text.StringBuilder
    [void]$builder.Append('"')
    $backslashes = 0
    foreach ($ch in $value.ToCharArray()) {
        if ($ch -eq '\') {
            $backslashes++
            continue
        }
        if ($ch -eq '"') {
            if ($backslashes -gt 0) { [void]$builder.Append(('\' * ($backslashes * 2))) }
            [void]$builder.Append('\"')
            $backslashes = 0
            continue
        }
        if ($backslashes -gt 0) {
            [void]$builder.Append(('\' * $backslashes))
            $backslashes = 0
        }
        [void]$builder.Append($ch)
    }
    if ($backslashes -gt 0) { [void]$builder.Append(('\' * ($backslashes * 2))) }
    [void]$builder.Append('"')
    return $builder.ToString()
}

function Join-ProcessArguments {
    param([string[]]$CommandArgs)
    return (($CommandArgs | ForEach-Object { Quote-ProcessArgument $_ }) -join " ")
}

function Set-ProcessUtf8OutputEncoding {
    param([System.Diagnostics.ProcessStartInfo]$ProcessStartInfo)
    $utf8 = New-Object System.Text.UTF8Encoding $false
    try { $ProcessStartInfo.StandardOutputEncoding = $utf8 } catch { }
    try { $ProcessStartInfo.StandardErrorEncoding = $utf8 } catch { }
}

function Invoke-AgentRaw {
    param([Parameter(Mandatory = $true)][Alias("Args")][string[]]$CommandArgs)

    $agentExe = Join-Path $BundleRoot "acbh-agent-windows-amd64.exe"
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $agentExe
    $psi.Arguments = Join-ProcessArguments $CommandArgs
    $psi.WorkingDirectory = $BundleRoot
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    Set-ProcessUtf8OutputEncoding $psi
    $process = [System.Diagnostics.Process]::Start($psi)
    if ($null -eq $process) {
        throw "failed to start agent process"
    }
    $stdout = $process.StandardOutput.ReadToEnd()
    $stderr = $process.StandardError.ReadToEnd()
    $process.WaitForExit()
    return [pscustomobject]@{
        ExitCode = $process.ExitCode
        Stdout = $(if ($null -eq $stdout) { "" } else { $stdout.Trim() })
        Stderr = $(if ($null -eq $stderr) { "" } else { $stderr.Trim() })
        Command = $CommandArgs -join " "
    }
}

function Invoke-AgentJson {
    param([Parameter(Mandatory = $true)][Alias("Args")][string[]]$CommandArgs)

    $result = Invoke-AgentRaw -CommandArgs $CommandArgs
    if ($result.ExitCode -ne 0) {
        throw "agent command failed ($($result.Command)): $($result.Stderr) $($result.Stdout)"
    }
    if (-not $result.Stdout.StartsWith("{") -and -not $result.Stdout.StartsWith("[")) {
        throw "agent command did not return pure JSON ($($result.Command)): $($result.Stdout)"
    }
    return $result.Stdout | ConvertFrom-Json
}

function New-TestEnvironmentPackage {
    param([Parameter(Mandatory = $true)][string]$Target)

    $pkgRoot = Join-Path $TempRoot "envpkg-root"
    New-Item -ItemType Directory -Force -Path (Join-Path $pkgRoot "base") | Out-Null
    $readme = Join-Path $pkgRoot "base\readme.txt"
    Write-Utf8NoBom -Path $readme -Text "ACBH runtime package smoke"
    $hash = (Get-FileHash -Algorithm SHA256 -Path $readme).Hash.ToLowerInvariant()
    $size = (Get-Item $readme).Length
    $manifest = [ordered]@{
        version = 1
        id = "smoke-runtime-base"
        packageId = "acbh-runtime-base-windows-amd64"
        kind = "runtime-base"
        os = "windows"
        architecture = "amd64"
        signature = "smoke-signature-placeholder"
        files = @(
            [ordered]@{
                path = "base/readme.txt"
                sha256 = $hash
                size = $size
            }
        )
    } | ConvertTo-Json -Depth 6
    Write-Utf8NoBom -Path (Join-Path $pkgRoot "acbh-package.json") -Text $manifest
    Compress-Archive -Path (Join-Path $pkgRoot "*") -DestinationPath $Target -Force
}

try {
    New-Item -ItemType Directory -Force -Path (Join-Path $BundleRoot "coordinator\dist") | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $BundleRoot "scripts") | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $BundleRoot "docs") | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $BundleRoot "runtime\node") | Out-Null

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
    Copy-PowerShellUtf8Bom -Source (Join-Path $RepoRoot "scripts\acbh-desktop-gui.ps1") -Target (Join-Path $BundleRoot "scripts\acbh-desktop-gui.ps1")
    Copy-Item -Recurse -Force (Join-Path $RepoRoot "docs\zh-CN") (Join-Path $BundleRoot "docs\zh-CN")

    $nodeCommand = Get-Command node -ErrorAction SilentlyContinue
    if (-not $nodeCommand) {
        throw "node.exe is required to assemble the smoke bundle runtime"
    }
    Copy-Item -Force $nodeCommand.Source (Join-Path $BundleRoot "runtime\node\node.exe")

    Push-Location (Join-Path $BundleRoot "coordinator")
    try {
        if (Get-Command npm -ErrorAction SilentlyContinue) {
            & npm install --omit=dev --no-audit --no-fund --package-lock=false
            if ($LASTEXITCODE -ne 0) { throw "npm install failed" }
        } else {
            Invoke-Pnpm -PnpmArgs @("install", "--prod", "--offline")
        }
    } finally {
        Pop-Location
    }

    $guiPath = Join-Path $BundleRoot "scripts\acbh-desktop-gui.ps1"
    $guiBytes = [System.IO.File]::ReadAllBytes($guiPath)
    if ($guiBytes.Length -lt 3 -or $guiBytes[0] -ne 0xEF -or $guiBytes[1] -ne 0xBB -or $guiBytes[2] -ne 0xBF) {
        throw "GUI script in bundle must be UTF-8 with BOM for Windows PowerShell 5.1"
    }
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -STA -File $guiPath -SelfTest | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "GUI script failed powershell.exe -File self-test"
    }
    $guiText = Get-Content -Raw -Encoding UTF8 -Path $guiPath
    $parseErrors = $null
    $null = [System.Management.Automation.PSParser]::Tokenize($guiText, [ref]$parseErrors)
    if ($parseErrors -and $parseErrors.Count -gt 0) {
        throw "GUI script parse failed: $($parseErrors[0].Message)"
    }
    foreach ($forbiddenPattern in @(
        '\.DoWork\s*(\+)?\s*=',
        '\.RunWorkerCompleted\s*(\+)?\s*=',
        '\.ProgressChanged\s*(\+)?\s*=',
        'Task\.Run',
        'ThreadPool',
        'New-Object\s+System\.ComponentModel\.BackgroundWorker',
        '\[System\.Threading\.Thread\]',
        '\.BeginInvoke\('
    )) {
        if ($guiText -match $forbiddenPattern) {
            throw "GUI smoke failed; forbidden async/event pattern: $forbiddenPattern"
        }
    }
    foreach ($requiredText in @(
        "Invoke-AgentCommandSafe",
        "Add-SafeClick",
        "Show-GuiError",
        "Redact-Secrets",
        '"desktop", "environment", "check"',
        '"desktop", "setup", "configure-network"',
        '"desktop", "server", "start-auto"',
        '"desktop", "server", "select-launch"',
        "Update-ServerSummary",
        "Show-LaunchEvidence",
        "advancedPanel",
        "Import-OfflinePack",
        '$advancedPanel.Visible = $false',
        'New-Object System.Diagnostics.ProcessStartInfo'
    )) {
        if (-not $guiText.Contains($requiredText)) {
            throw "GUI smoke failed; missing required text: $requiredText"
        }
    }

    $packageJson = Get-Content -Raw -Path (Join-Path $BundleRoot "coordinator\package.json") | ConvertFrom-Json
    if (-not $packageJson.dependencies.ws) {
        throw "ws is not present in coordinator production dependencies"
    }

    $envReport = Invoke-AgentJson -Args @("desktop", "environment", "check", "--app-data-dir", $AppData, "--json")
    if (-not (Test-Path $envReport.environmentReportPath)) {
        throw "desktop environment check did not write environment-report.json"
    }
    $envReportRaw = Invoke-AgentRaw -Args @("desktop", "environment", "check", "--app-data-dir", $AppData, "--json")
    $mojibakeMarkers = @([char]0x951B, [char]0x6D93, [char]0x6748, [char]0x9241, [char]0xFFFD)
    foreach ($marker in $mojibakeMarkers) {
        if ($envReportRaw.Stdout.Contains([string]$marker)) {
            throw "desktop environment check JSON was decoded with mojibake"
        }
    }
    $null = $envReportRaw.Stdout | ConvertFrom-Json

    $packagePath = Join-Path $TempRoot "acbh-runtime-base-windows-amd64.zip"
    New-TestEnvironmentPackage -Target $packagePath
    $verifyPackage = Invoke-AgentJson -Args @("desktop", "environment", "verify-package", "--file", $packagePath, "--json")
    if (-not $verifyPackage.ok) {
        throw "verify-package should pass for generated smoke package"
    }
    $importPackage = Invoke-AgentJson -Args @("desktop", "environment", "import-pack", "--app-data-dir", $AppData, "--file", $packagePath, "--json")
    if (-not $importPackage.ok -or -not (Test-Path (Join-Path $AppData "runtime\base\readme.txt"))) {
        throw "import-pack did not install generated smoke package"
    }

    $Port = Get-FreeTcpPort
    $start = Invoke-AgentRaw -Args @("desktop", "start", "--app-data-dir", $AppData, "--coordinator", (Join-Path $BundleRoot "coordinator\dist\index.js"), "--port", "$Port")
    if ($start.ExitCode -ne 0) {
        throw "desktop start failed: $($start.Stderr) $($start.Stdout)"
    }
    $status = Invoke-AgentJson -Args @("desktop", "status", "--app-data-dir", $AppData, "--port", "$Port", "--json")
    if (-not $status.healthOk) {
        throw "desktop status did not report healthy coordinator"
    }
    if (($status | ConvertTo-Json -Depth 8) -match "(?i)accessKey|hostToken|rcon\.password") {
        throw "desktop status leaked sensitive field names"
    }

    $coordURL = "http://127.0.0.1:$Port"
    $network = Invoke-AgentJson -Args @("desktop", "setup", "configure-network", "--app-data-dir", $AppData, "--host-name", "127.0.0.1", "--coordinator-port", "$Port", "--public-game-port", "25565", "--json")
    if ($network.coordinatorUrl -ne $coordURL -or $network.playerAddress -ne "127.0.0.1:25565") {
        throw "configure-network returned unexpected addresses"
    }

    $group = Invoke-AgentJson -Args @("desktop", "setup", "create-group", "--app-data-dir", $AppData, "--group-name", "Smoke Group", "--display-name", "Owner", "--coordinator-url", $coordURL, "--json")
    if (-not $group.ok -or -not $group.inviteCode) {
        throw "setup create-group did not create an invite code"
    }
    $joined = Invoke-AgentJson -Args @("desktop", "setup", "join-group", "--app-data-dir", $JoinAppData, "--invite-code", $group.inviteCode, "--display-name", "Friend", "--coordinator-url", $coordURL, "--json")
    if (-not $joined.ok -or $joined.groupId -ne $group.groupId) {
        throw "setup join-group failed to use invite code"
    }

    $serverDir = Join-Path $TempRoot "fixture-server"
    New-Item -ItemType Directory -Force -Path (Join-Path $serverDir "world") | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $serverDir "mods") | Out-Null
    Write-Utf8NoBom -Path (Join-Path $serverDir "paper-1.20.1.jar") -Text "fake jar"
    Write-Utf8NoBom -Path (Join-Path $serverDir "mods\demo.jar") -Text "fake mod"
    Write-Utf8NoBom -Path (Join-Path $serverDir "world\level.dat") -Text "fake world"
    Write-Utf8NoBom -Path (Join-Path $serverDir "eula.txt") -Text "eula=true"
    Write-Utf8NoBom -Path (Join-Path $serverDir "server.properties") -Text "enable-rcon=false`nserver-port=25565`nlevel-name=world"

    $inspect = Invoke-AgentJson -Args @("desktop", "setup", "inspect-server", "--app-data-dir", $AppData, "--server-dir", $serverDir, "--json")
    if (-not $inspect.ok -or -not $inspect.inspectionOk -or -not $inspect.launchReady -or $inspect.state -ne "ReadyToStart" -or $inspect.report.serverType -ne "Paper" -or -not $inspect.report.launchEntry) {
        throw "setup inspect-server did not recognize Paper fixture"
    }
    if ($inspect.requiredJavaVersion -ne "17" -or [string]::IsNullOrWhiteSpace($inspect.detectedJavaVersion) -or [string]::IsNullOrWhiteSpace($inspect.detectedJavaPath)) {
        throw "setup inspect-server did not report distinct required/detected Java fields"
    }
    $candidates = Invoke-AgentJson -Args @("desktop", "server", "candidates", "--app-data-dir", $AppData, "--json")
    if ($candidates.jars.Count -lt 1 -or $candidates.recommended.kind -ne "jar") {
        throw "desktop server candidates did not return launch candidates"
    }
    $profile = Invoke-AgentJson -Args @("desktop", "server", "launch-profile", "--app-data-dir", $AppData, "--json")
    if ($profile.kind -ne "jar" -or $profile.serverType -ne "Paper") {
        throw "desktop server launch-profile did not return saved profile"
    }
    $complete = Invoke-AgentJson -Args @("desktop", "setup", "complete", "--app-data-dir", $AppData, "--json")
    if (-not $complete.ok -or $complete.state -ne "Ready") {
        throw "setup complete did not enter Ready state"
    }
    $cfg = Get-Content -Raw -Encoding UTF8 -Path (Join-Path $AppData "config.yaml") | ConvertFrom-Json
    if ($cfg.server.dir -ne $serverDir -or -not $cfg.server.command) {
        throw "setup inspect-server did not persist server config for one-click start"
    }

    $autoStatus = Invoke-AgentJson -Args @("desktop", "server", "status", "--app-data-dir", $AppData, "--json")
    if ($null -eq $autoStatus.ok) {
        throw "desktop server status did not return the simplified status contract"
    }
    $stopAuto = Invoke-AgentJson -Args @("desktop", "server", "stop-auto", "--app-data-dir", $AppData, "--json")
    if (-not $stopAuto.ok) {
        throw "desktop server stop-auto should be idempotent in smoke"
    }

    foreach ($requiredPath in @(
        "scripts\acbh-desktop-gui.ps1",
        "docs\zh-CN\windows-private-desktop-quickstart.md",
        "coordinator\package.json",
        "coordinator\node_modules\ws\package.json",
        "runtime\node\node.exe",
        "acbh-desktop-windows-amd64.exe",
        "acbh-agent-windows-amd64.exe"
    )) {
        if (-not (Test-Path (Join-Path $BundleRoot $requiredPath))) {
            throw "bundle smoke missing $requiredPath"
        }
    }

    Write-Host "Windows desktop simple-flow bundle smoke passed." -ForegroundColor Green
} finally {
    try {
        $agentExe = Join-Path $BundleRoot "acbh-agent-windows-amd64.exe"
        if (Test-Path $agentExe) {
            & $agentExe desktop server stop-auto --app-data-dir $AppData 2>$null | Out-Null
            if ($Port) {
                & $agentExe desktop stop --app-data-dir $AppData --port "$Port" 2>$null | Out-Null
            }
        }
    } catch {
    }
    Start-Sleep -Milliseconds 500
    if (Test-Path $TempRoot) {
        Remove-Item -Recurse -Force $TempRoot -ErrorAction SilentlyContinue
    }
}
