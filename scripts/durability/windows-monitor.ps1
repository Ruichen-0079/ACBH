[CmdletBinding()]
param(
    [ValidateSet('start', 'run', 'status', 'stop', 'collect')]
    [string]$Mode = 'status',
    [ValidateRange(10, 86400)]
    [int]$IntervalSeconds = 60,
    [string]$OutputDirectory = (Join-Path $env:LOCALAPPDATA 'ACBH\durability'),
    [string]$AgentStatusUri = 'http://127.0.0.1:6132/local/v1/status',
    [string]$PublicHost = '127.0.0.1',
    [ValidateRange(1, 65535)]
    [int]$PublicPort = 25575,
    [ValidateRange(1, 65535)]
    [int]$ProtectedPort = 25565,
    [string]$AgentLogDirectory = (Join-Path $env:APPDATA 'acbh\logs'),
    [ValidateRange(1024, [long]::MaxValue)]
    [long]$MaxBytes = 20MB,
    [ValidateRange(1, 100)]
    [int]$MaxFiles = 5,
    [string]$SummaryPath = ''
)

$ErrorActionPreference = 'Stop'
$scriptPath = $MyInvocation.MyCommand.Path
$pidPath = Join-Path $OutputDirectory 'windows-monitor.pid.json'

function New-OutputDirectory {
    New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
}

function Test-TcpPort([string]$HostName, [int]$Port, [int]$TimeoutMilliseconds = 3000) {
    $client = [Net.Sockets.TcpClient]::new()
    try {
        $result = $client.BeginConnect($HostName, $Port, $null, $null)
        if (-not $result.AsyncWaitHandle.WaitOne($TimeoutMilliseconds)) { return $false }
        $client.EndConnect($result)
        return $true
    } catch {
        return $false
    } finally {
        $client.Dispose()
    }
}

function Get-ListenerPid([int]$Port) {
    $listener = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if ($null -eq $listener) { return 0 }
    return [int]$listener.OwningProcess
}

function Get-DirectoryBytes([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) { return 0L }
    $measure = Get-ChildItem -LiteralPath $Path -File -Recurse -ErrorAction SilentlyContinue |
        Measure-Object -Property Length -Sum
    if ($null -eq $measure.Sum) { return 0L }
    return [long]$measure.Sum
}

function Get-VerifiedMonitorProcess {
    if (-not (Test-Path -LiteralPath $pidPath)) { return $null }
    try {
        $metadata = Get-Content -LiteralPath $pidPath -Raw | ConvertFrom-Json
        $process = Get-CimInstance Win32_Process -Filter "ProcessId=$($metadata.pid)" -ErrorAction Stop
        $expected = [IO.Path]::GetFullPath($scriptPath)
        if (-not $process.CommandLine.Contains($expected) -or -not $process.CommandLine.Contains('-Mode run')) {
            return $null
        }
        $actualStart = ([DateTime]$process.CreationDate).ToUniversalTime()
        $recordedStart = if ($metadata.start_utc -is [DateTime]) {
            ([DateTime]$metadata.start_utc).ToUniversalTime()
        } else {
            [DateTimeOffset]::Parse([string]$metadata.start_utc).UtcDateTime
        }
        if ([Math]::Abs(($actualStart - $recordedStart).TotalSeconds) -gt 2) { return $null }
        return $process
    } catch {
        return $null
    }
}

function Get-CurrentLogFile {
    New-OutputDirectory
    $current = Get-ChildItem -LiteralPath $OutputDirectory -Filter 'windows-monitor-*.jsonl' -File |
        Sort-Object LastWriteTimeUtc -Descending | Select-Object -First 1
    if ($null -eq $current -or $current.Length -ge $MaxBytes) {
        $name = 'windows-monitor-{0}-{1}.jsonl' -f ([DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss')), $PID
        $current = New-Item -ItemType File -Path (Join-Path $OutputDirectory $name) -Force
    }
    $files = @(Get-ChildItem -LiteralPath $OutputDirectory -Filter 'windows-monitor-*.jsonl' -File |
        Sort-Object LastWriteTimeUtc -Descending)
    if ($files.Count -gt $MaxFiles) {
        $files | Select-Object -Skip $MaxFiles | ForEach-Object {
            Remove-Item -LiteralPath $_.FullName -Force
        }
    }
    return $current.FullName
}

function Get-Sample {
    $now = [DateTime]::UtcNow
    $agentState = 'UNREACHABLE'
    $minecraftState = 'UNKNOWN'
    $relayState = 'UNKNOWN'
    $coordinatorState = 'UNKNOWN'
    $relayReason = ''
    $localPort = 0
    try {
        $status = Invoke-RestMethod -Uri $AgentStatusUri -TimeoutSec 5
        $agentState = [string]$status.overall
        $minecraftState = [string]$status.minecraft.state
        $relayState = [string]$status.relay.state
        $coordinatorState = [string]$status.coordinator.state
        $relayReason = [string]$status.relay.reason_code
        if ([string]$status.local_endpoint -match ':(\d+)$') { $localPort = [int]$Matches[1] }
    } catch {
        # The record intentionally stores only the stable state label, not exception text.
    }

    $agentPort = ([Uri]$AgentStatusUri).Port
    $agentPid = Get-ListenerPid $agentPort
    $agentMemory = 0L
    $agentThreads = 0
    $agentHandles = 0
    if ($agentPid -gt 0) {
        $agentProcess = Get-Process -Id $agentPid -ErrorAction SilentlyContinue
        if ($null -ne $agentProcess) {
            $agentMemory = [long]$agentProcess.WorkingSet64
            $agentThreads = [int]$agentProcess.Threads.Count
            $agentHandles = [int]$agentProcess.HandleCount
        }
    }

    $testMinecraftPid = if ($localPort -gt 0) { Get-ListenerPid $localPort } else { 0 }
    $testJavaCount = 0
    if ($testMinecraftPid -gt 0) {
        $testProcess = Get-Process -Id $testMinecraftPid -ErrorAction SilentlyContinue
        if ($null -ne $testProcess -and $testProcess.ProcessName -eq 'java') { $testJavaCount = 1 }
    }

    $protectedPid = Get-ListenerPid $ProtectedPort
    return [ordered]@{
        timestamp_utc = $now.ToString('o')
        agent_state = $agentState
        minecraft_state = $minecraftState
        relay_state = $relayState
        relay_reason = $relayReason
        coordinator_state = $coordinatorState
        java_process_count = @(Get-Process java -ErrorAction SilentlyContinue).Count
        test_java_process_count = $testJavaCount
        frpc_process_count = @(Get-Process frpc -ErrorAction SilentlyContinue).Count
        agent_pid = $agentPid
        agent_working_set_bytes = $agentMemory
        agent_thread_count = $agentThreads
        agent_handle_count = $agentHandles
        public_25575_reachable = (Test-TcpPort $PublicHost $PublicPort)
        protected_25565_listening = ($protectedPid -gt 0)
        protected_25565_pid = $protectedPid
        agent_log_directory_bytes = (Get-DirectoryBytes $AgentLogDirectory)
    }
}

function Start-Monitor {
    New-OutputDirectory
    if ($null -ne (Get-VerifiedMonitorProcess)) { throw 'Windows durability monitor is already running.' }
    $hostExecutable = (Get-Process -Id $PID).Path
    $arguments = @(
        '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', ('"{0}"' -f $scriptPath),
        '-Mode', 'run', '-IntervalSeconds', $IntervalSeconds,
        '-OutputDirectory', ('"{0}"' -f $OutputDirectory),
        '-AgentStatusUri', ('"{0}"' -f $AgentStatusUri),
        '-PublicHost', ('"{0}"' -f $PublicHost), '-PublicPort', $PublicPort,
        '-ProtectedPort', $ProtectedPort,
        '-AgentLogDirectory', ('"{0}"' -f $AgentLogDirectory),
        '-MaxBytes', $MaxBytes, '-MaxFiles', $MaxFiles
    )
    $process = Start-Process -FilePath $hostExecutable -ArgumentList $arguments -WindowStyle Hidden -PassThru
    Start-Sleep -Milliseconds 300
    if ($process.HasExited) { throw "Windows durability monitor exited with code $($process.ExitCode)." }
    [ordered]@{
        pid = $process.Id
        start_utc = $process.StartTime.ToUniversalTime().ToString('o')
        script = [IO.Path]::GetFullPath($scriptPath)
    } | ConvertTo-Json -Compress | Set-Content -LiteralPath $pidPath -Encoding utf8
    Show-Status
}

function Show-Status {
    $process = Get-VerifiedMonitorProcess
    $latestFile = Get-ChildItem -LiteralPath $OutputDirectory -Filter 'windows-monitor-*.jsonl' -File -ErrorAction SilentlyContinue |
        Sort-Object LastWriteTimeUtc -Descending | Select-Object -First 1
    $latestSample = $null
    if ($null -ne $latestFile) {
        $line = Get-Content -LiteralPath $latestFile.FullName -Tail 1
        if ($line) { $latestSample = $line | ConvertFrom-Json }
    }
    [ordered]@{
        running = ($null -ne $process)
        pid = if ($null -ne $process) { [int]$process.ProcessId } else { 0 }
        latest_file = if ($null -ne $latestFile) { $latestFile.FullName } else { '' }
        latest_sample = $latestSample
    } | ConvertTo-Json -Depth 5
}

function Stop-Monitor {
    $process = Get-VerifiedMonitorProcess
    if ($null -eq $process) {
        [ordered]@{ stopped = $false; reason = 'not_running' } | ConvertTo-Json -Compress
        return
    }
    Stop-Process -Id ([int]$process.ProcessId) -Force
    [ordered]@{ stopped = $true; pid = [int]$process.ProcessId } | ConvertTo-Json -Compress
}

function Collect-Report {
    New-OutputDirectory
    $records = [Collections.Generic.List[object]]::new()
    Get-ChildItem -LiteralPath $OutputDirectory -Filter 'windows-monitor-*.jsonl' -File |
        Sort-Object Name | ForEach-Object {
            Get-Content -LiteralPath $_.FullName | ForEach-Object {
                if ($_ -ne '') { $records.Add(($_ | ConvertFrom-Json)) }
            }
        }
    if ($records.Count -eq 0) { throw 'No Windows durability samples were found.' }
    $summary = [ordered]@{
        generated_utc = [DateTime]::UtcNow.ToString('o')
        sample_count = $records.Count
        first_sample_utc = $records[0].timestamp_utc
        last_sample_utc = $records[$records.Count - 1].timestamp_utc
        online_samples = @($records | Where-Object { $_.agent_state -eq 'ONLINE' }).Count
        non_online_samples = @($records | Where-Object { $_.agent_state -ne 'ONLINE' }).Count
        public_probe_failures = @($records | Where-Object { -not $_.public_25575_reachable }).Count
        protected_25565_failures = @($records | Where-Object { -not $_.protected_25565_listening }).Count
        max_agent_working_set_bytes = ($records | Measure-Object agent_working_set_bytes -Maximum).Maximum
        max_agent_thread_count = ($records | Measure-Object agent_thread_count -Maximum).Maximum
        max_agent_handle_count = ($records | Measure-Object agent_handle_count -Maximum).Maximum
        max_java_process_count = ($records | Measure-Object java_process_count -Maximum).Maximum
        max_frpc_process_count = ($records | Measure-Object frpc_process_count -Maximum).Maximum
    }
    if ([string]::IsNullOrWhiteSpace($SummaryPath)) {
        $script:SummaryPath = Join-Path $OutputDirectory 'windows-summary.json'
    }
    $summary | ConvertTo-Json | Set-Content -LiteralPath $SummaryPath -Encoding utf8
    $summary | ConvertTo-Json
}

switch ($Mode) {
    'start' { Start-Monitor }
    'run' {
        New-OutputDirectory
        while ($true) {
            $sample = Get-Sample
            $sample | ConvertTo-Json -Compress | Add-Content -LiteralPath (Get-CurrentLogFile) -Encoding utf8
            Start-Sleep -Seconds $IntervalSeconds
        }
    }
    'status' { Show-Status }
    'stop' { Stop-Monitor }
    'collect' { Collect-Report }
}
