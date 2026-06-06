param(
    [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path,
    [string]$WorkDir = (Join-Path $env:TEMP "acbh-two-host-demo"),
    [int]$Port = 6212
)

$ErrorActionPreference = "Stop"
$coordinatorProcess = $null
$originalAppData = $env:AppData
$originalStorageRoot = $env:ACBH_STORAGE_ROOT
$originalTimeout = $env:ACBH_HOST_HEARTBEAT_TIMEOUT_MS
$originalPort = $env:PORT

function Invoke-JsonPost {
    param([string]$Url, [hashtable]$Body)
    return Invoke-RestMethod -Method Post -Uri $Url -ContentType "application/json" -Body ($Body | ConvertTo-Json -Depth 12 -Compress)
}

function Use-AgentConfig {
    param([string]$Root)
    $env:AppData = $Root
    $env:XDG_CONFIG_HOME = $Root
}

function Read-AgentConfig {
    param([string]$Root)
    return Get-Content -LiteralPath (Join-Path $Root "acbh/config.yaml") -Raw | ConvertFrom-Json
}

try {
    if (Test-Path -LiteralPath $WorkDir) {
        $resolved = [IO.Path]::GetFullPath($WorkDir)
        $tempRoot = [IO.Path]::GetFullPath($env:TEMP)
        if (-not $resolved.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase)) {
            throw "Refusing to remove demo directory outside the temporary directory: $resolved"
        }
        Remove-Item -LiteralPath $WorkDir -Recurse -Force
    }
    New-Item -ItemType Directory -Path $WorkDir | Out-Null

    Push-Location $RepoRoot
    try {
        corepack pnpm build:coordinator
        if ($LASTEXITCODE -ne 0) { throw "Coordinator build failed" }
        $go = Get-Command go -ErrorAction Stop
        & $go.Source -C agent build -o (Join-Path $WorkDir "acbh-agent.exe") .
        if ($LASTEXITCODE -ne 0) { throw "Agent build failed" }
    } finally {
        Pop-Location
    }

    $agent = Join-Path $WorkDir "acbh-agent.exe"
    $hostAConfig = Join-Path $WorkDir "host-a-config"
    $hostBConfig = Join-Path $WorkDir "host-b-config"
    $sourceDir = Join-Path $WorkDir "source-server"
    $restoreDir = Join-Path $WorkDir "host-b-server"
    $manifestPath = Join-Path $WorkDir "snap_000001.manifest.json"
    $fakeServer = Join-Path $RepoRoot "examples/two-host-takeover-demo/fake-server.ps1"
    New-Item -ItemType Directory -Path (Join-Path $sourceDir "world/region") -Force | Out-Null
    New-Item -ItemType Directory -Path $restoreDir -Force | Out-Null
    Set-Content -LiteralPath (Join-Path $sourceDir "world/region/r.0.0.mca") -Value "fake-region-data" -NoNewline

    $env:ACBH_STORAGE_ROOT = Join-Path $WorkDir "coordinator-storage"
    $env:ACBH_HOST_HEARTBEAT_TIMEOUT_MS = "1000"
    $env:PORT = [string]$Port
    $coordinatorStdout = Join-Path $WorkDir "coordinator-stdout.log"
    $coordinatorStderr = Join-Path $WorkDir "coordinator-stderr.log"
    $coordinatorProcess = Start-Process -FilePath "node" `
        -ArgumentList (Join-Path $RepoRoot "apps/coordinator/dist/index.js") `
        -WorkingDirectory $RepoRoot `
        -RedirectStandardOutput $coordinatorStdout `
        -RedirectStandardError $coordinatorStderr `
        -WindowStyle Hidden `
        -PassThru

    $baseUrl = "http://127.0.0.1:$Port"
    $ready = $false
    for ($attempt = 0; $attempt -lt 50; $attempt++) {
        try {
            $health = Invoke-RestMethod -Uri "$baseUrl/health"
            if ($health.ok) {
                $ready = $true
                break
            }
        } catch {
            Start-Sleep -Milliseconds 100
        }
    }
    if (-not $ready) { throw "Coordinator did not become ready" }

    $group = Invoke-JsonPost "$baseUrl/v1/groups" @{
        name = "Two Host Demo"
        ownerName = "Demo Owner"
    }

    Use-AgentConfig $hostAConfig
    & $agent login --coordinator $baseUrl --group-id $group.groupId --access-key $group.accessKey --name HostA --device-name HostA-PC --platform windows
    if ($LASTEXITCODE -ne 0) { throw "Host A login failed" }
    $hostA = Read-AgentConfig $hostAConfig

    Use-AgentConfig $hostBConfig
    & $agent login --coordinator $baseUrl --group-id $group.groupId --access-key $group.accessKey --name HostB --device-name HostB-PC --platform windows
    if ($LASTEXITCODE -ne 0) { throw "Host B login failed" }
    $hostB = Read-AgentConfig $hostBConfig

    Use-AgentConfig $hostAConfig
    & $agent heartbeat --status standby --java-available true
    Use-AgentConfig $hostBConfig
    & $agent heartbeat --status standby --java-available false

    $initialElection = Invoke-JsonPost "$baseUrl/v1/groups/$($group.groupId)/election/run" @{
        groupId = $group.groupId
        hostId = $hostA.hostId
        hostToken = $hostA.hostToken
        reason = "no-current-host"
    }
    if ($initialElection.selectedHostId -ne $hostA.hostId) {
        throw "Initial election did not select Host A"
    }

    Use-AgentConfig $hostAConfig
    & $agent takeover poll
    & $agent takeover accept
    & $agent takeover complete
    & $agent heartbeat --status hosting --java-available true

    & $agent scan `
        --server-dir $sourceDir `
        --artifact-kind world-snapshot `
        --artifact-id snap_000001 `
        --server-pack-version pack_000001 `
        --output $manifestPath
    if ($LASTEXITCODE -ne 0) { throw "Host A scan failed" }
    & $agent push --manifest $manifestPath --server-dir $sourceDir
    if ($LASTEXITCODE -ne 0) { throw "Host A push failed" }

    Start-Sleep -Milliseconds 1200
    Use-AgentConfig $hostBConfig
    & $agent heartbeat --status standby --java-available true --latest-world-snapshot snap_000001
    & $agent election check-timeout
    & $agent takeover poll

    $command = "powershell.exe -NoProfile -ExecutionPolicy Bypass -File `"$fakeServer`""
    & $agent takeover run `
        --server-dir $restoreDir `
        --command $command `
        --log-dir (Join-Path $WorkDir "host-b-logs") `
        --stop-timeout 5s
    if ($LASTEXITCODE -ne 0) { throw "Host B takeover run failed" }

    $state = Invoke-RestMethod -Uri "$baseUrl/v1/groups/$($group.groupId)/state"
    if ($state.currentHostId -ne $hostB.hostId) {
        throw "Coordinator currentHostId is not Host B"
    }
    if ($state.currentHostGeneration -ne 2) {
        throw "Expected currentHostGeneration 2, got $($state.currentHostGeneration)"
    }

    $sourceHash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $sourceDir "world/region/r.0.0.mca")).Hash
    $restoredHash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $restoreDir "world/region/r.0.0.mca")).Hash
    if ($sourceHash -ne $restoredHash) {
        throw "Restored world object SHA256 does not match source"
    }

    $serverStatus = & $agent server status
    if (($serverStatus -join "`n") -notmatch "Server status: running") {
        throw "Fake server is not running after takeover"
    }

    Write-Output ""
    Write-Output "Two-host takeover demo passed."
    Write-Output "Host A: $($hostA.hostId)"
    Write-Output "Host B selected: $($hostB.hostId)"
    Write-Output "Artifact restored SHA256: $restoredHash"
    Write-Output "Coordinator currentHostId: $($state.currentHostId)"
    Write-Output "Coordinator currentHostGeneration: $($state.currentHostGeneration)"
    & $agent server stop
} finally {
    if ($coordinatorProcess -ne $null -and -not $coordinatorProcess.HasExited) {
        Stop-Process -Id $coordinatorProcess.Id -Force
    }
    $env:AppData = $originalAppData
    $env:ACBH_STORAGE_ROOT = $originalStorageRoot
    $env:ACBH_HOST_HEARTBEAT_TIMEOUT_MS = $originalTimeout
    $env:PORT = $originalPort
}
