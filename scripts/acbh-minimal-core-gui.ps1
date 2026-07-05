param(
    [string]$AgentPath,
    [string]$AppDataDir,
    [string]$BodyListen = "127.0.0.1:6120",
    [switch]$SelfTest
)

$ErrorActionPreference = "Stop"

if ($SelfTest) {
    $source = Get-Content -Raw -Path $PSCommandPath
    $checks = @(
        "Refresh-ListenerStatus",
        "Save-ListenerConfig",
        "Configure-Relay",
        "Refresh-RelayStatus",
        "txtPublicEndpoint",
        "txtListenerProcess",
        "txtRelayError",
        "Analyze-Backup",
        "Upload-Backup",
        "Wait-BodyOperation",
        "Refresh-Snapshots",
        "Download-LatestSnapshot",
        "txtBackupSummary",
        "txtSnapshotList",
        "txtRestoreTargetDir",
        "Body API 未连接",
        "Format-ExceptionDetails",
        "Set-ControlText",
        "Fit-FormToWorkingArea",
        "AutoScaleMode",
        "AutoScroll",
        "New-Object System.Drawing.Size(1120, 860)",
        "New-Object System.Drawing.Size(1000, 740)",
        "备份 / 快照",
        "私人实例",
        "当前设备",
        "VPS 地址",
        "监听 / VPS 中转",
        "VPS 中转",
        "访问令牌状态",
        "Refresh health",
        "Load config",
        "Save config",
        "Test connection",
        "Initialize",
        "Analyze backup",
        "Upload backup",
        "List snapshots",
        "Download latest",
        "Download selected",
        '$script:BodyUrl',
        '"/v1/operations/"'
    )
    foreach ($check in $checks) {
        if (-not $source.Contains($check)) {
            throw "GUI self-test failed: missing $check"
        }
    }
    $forbidden = @(
        ("Start" + "Server"),
        ("Stop" + "Server"),
        ("Repair" + "Lock"),
        ("server" + ".lock"),
        ("super" + "visor")
    )
    foreach ($token in $forbidden) {
        if ($source.Contains($token)) {
            throw "GUI self-test failed: forbidden lifecycle/control path found"
        }
    }
    if ($source -match ("Invoke-RestMethod\s+[^\r\n]*" + "txtCoordinator")) {
        throw "GUI self-test failed: direct coordinator REST call found"
    }
    $visibleForbidden = @(
        ('Add-Label "' + 'Group' + ' ID"'),
        ('Add-Label "' + 'Member' + ' ID"'),
        ('Add-Label "' + 'Host' + ' ID"'),
        ('Add-Label "' + 'Host' + ' Token"')
    )
    foreach ($token in $visibleForbidden) {
        if ($source.Contains($token)) {
            throw "GUI self-test failed: legacy identity label found"
        }
    }
    $legacyVisiblePattern = '"[^"]*(?:elec' + 'tion|take' + 'over)[^"]*"'
    foreach ($line in ($source -split "`r?`n")) {
        if ($line -match $legacyVisiblePattern) {
            throw ("GUI self-test failed: forbidden " + "legacy visible text found")
        }
    }
    if ($source -match "Invoke-RestMethod\s+[^\r\n]*coordinatorUrl") {
        throw "GUI self-test failed: direct VPS/coordinator call found"
    }
    Write-Output "ACBH minimal-core GUI self-test ok"
    exit 0
}

Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

if (-not $AgentPath) {
    $AgentPath = Join-Path $PSScriptRoot "..\acbh-agent-windows-amd64.exe"
}
if (-not $AppDataDir) {
    $BundleRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
    if (Test-Path (Join-Path $BundleRoot "portable.flag")) {
        $AppDataDir = Join-Path $BundleRoot "data"
    } else {
        $AppDataDir = Join-Path $env:APPDATA "ACBH"
    }
}

$script:BodyProcess = $null
$script:BodyUrl = "http://$BodyListen"
$script:StartupErrors = New-Object System.Collections.Generic.List[string]

function Format-ExceptionDetails {
    param(
        [System.Management.Automation.ErrorRecord]$ErrorRecord,
        [string]$Context = "Operation failed"
    )
    $line = "unknown"
    $function = "unknown"
    if ($ErrorRecord -and $ErrorRecord.InvocationInfo) {
        if ($ErrorRecord.InvocationInfo.ScriptLineNumber) { $line = [string]$ErrorRecord.InvocationInfo.ScriptLineNumber }
        if ($ErrorRecord.InvocationInfo.MyCommand -and $ErrorRecord.InvocationInfo.MyCommand.Name) {
            $function = $ErrorRecord.InvocationInfo.MyCommand.Name
        }
    }
    $message = "unknown error"
    if ($ErrorRecord -and $ErrorRecord.Exception -and $ErrorRecord.Exception.Message) {
        $message = $ErrorRecord.Exception.Message
    }
    $stack = ""
    if ($ErrorRecord -and $ErrorRecord.ScriptStackTrace) {
        $stack = (($ErrorRecord.ScriptStackTrace -split "`r?`n") | Select-Object -First 4) -join [Environment]::NewLine
    }
    if (-not $stack -and $ErrorRecord -and $ErrorRecord.Exception -and $ErrorRecord.Exception.StackTrace) {
        $stack = (($ErrorRecord.Exception.StackTrace -split "`r?`n") | Select-Object -First 4) -join [Environment]::NewLine
    }
    return @"
$Context
line: $line
function: $function
message: $message
stack:
$stack
"@
}

function Set-ControlText {
    param(
        [object]$Control,
        [AllowNull()][object]$Value
    )
    if ($null -eq $Control) { return }
    $Control.Text = [string]$Value
}

function Redact-Secrets {
    param([string]$Text)
    if ([string]::IsNullOrEmpty($Text)) { return $Text }
    $safe = $Text
    $tokenPattern = "(?i)(hostToken|ownerToken|legacyHostToken|accessKey)\s*[:=]\s*\S+"
    $safe = [regex]::Replace($safe, $tokenPattern, "[hidden]")
    $safe = [regex]::Replace($safe, "ht_[A-Za-z0-9_\-]+", "ht_[hidden]")
    return $safe
}

function Add-Log {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    $line = "[" + (Get-Date -Format "HH:mm:ss") + "] " + (Redact-Secrets $Text)
    if ($null -eq $txtLog) {
        [void]$script:StartupErrors.Add($line)
        return
    }
    $txtLog.AppendText(($line + [Environment]::NewLine))
    $txtLog.SelectionStart = $txtLog.TextLength
    $txtLog.ScrollToCaret()
}

function Show-Error {
    param([string]$Message)
    Add-Log $Message
    $safe = Redact-Secrets $Message
    if ($null -ne $txtErrorDetails) {
        $txtErrorDetails.Text = $safe
    }
    if ($null -ne $lblState) {
        $lblState.Text = "State: needs attention"
    }
    [System.Windows.Forms.MessageBox]::Show($safe, "ACBH", "OK", "Error") | Out-Null
}

function Test-BodyPort {
    try {
        $client = New-Object System.Net.Sockets.TcpClient
        $iar = $client.BeginConnect("127.0.0.1", 6120, $null, $null)
        if (-not $iar.AsyncWaitHandle.WaitOne(250, $false)) {
            $client.Close()
            return $false
        }
        $client.EndConnect($iar)
        $client.Close()
        return $true
    } catch {
        return $false
    }
}

function Start-Body {
    if (Test-BodyPort) { return }
    if (-not (Test-Path $AgentPath)) {
        throw "Body API 未连接；可启动 body runtime，但找不到 ACBH Agent: $AgentPath"
    }
    New-Item -ItemType Directory -Force -Path $AppDataDir | Out-Null
    $args = @("body", "serve", "--listen", $BodyListen, "--app-data-dir", $AppDataDir)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $AgentPath
    $psi.Arguments = (($args | ForEach-Object { '"' + ([string]$_).Replace('"', '\"') + '"' }) -join " ")
    $psi.WorkingDirectory = Split-Path -Parent $AgentPath
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $script:BodyProcess = [System.Diagnostics.Process]::Start($psi)
    Start-Sleep -Milliseconds 600
    if (-not (Test-BodyPort)) {
        throw "Body API 未连接；可启动 body runtime，但未能在 $BodyListen 监听。请检查日志或手动运行 acbh-agent-windows-amd64.exe body serve。"
    }
    Add-Log "Body API started at $script:BodyUrl"
}

function Invoke-BodyJson {
    param(
        [string]$Method,
        [string]$Path,
        [object]$Body
    )
    Start-Body
    $uri = $script:BodyUrl + $Path
    try {
        if ($null -ne $Body) {
            $json = $Body | ConvertTo-Json -Depth 12
            return Invoke-RestMethod -Method $Method -Uri $uri -Body $json -ContentType "application/json"
        }
        return Invoke-RestMethod -Method $Method -Uri $uri
    } catch {
        $detail = $_.Exception.Message
        if ($_.ErrorDetails -and $_.ErrorDetails.Message) {
            $detail += [Environment]::NewLine + $_.ErrorDetails.Message
        }
        throw $detail
    }
}

function Refresh-Health {
    try {
        $health = Invoke-BodyJson -Method "GET" -Path "/v1/body/health"
        Set-ControlText $txtConfigPath $health.configPath
        Set-ControlText $txtBodyApi $health.bodyApi
        if ($health.coordinatorUrl) { Set-ControlText $txtCoordinator $health.coordinatorUrl }
        if ($health.mode) { $cmbMode.SelectedItem = $health.mode }
        if ($health.configError) {
            $lblState.Text = "State: config needs attention"
            Add-Log ("Config error: " + $health.configError.errorCode + " " + $health.configError.message)
        } else {
            $lblState.Text = "State: body ready"
            Add-Log "Body health ok."
        }
    } catch {
        Show-Error ("Health failed: " + $_.Exception.Message)
    }
}

function Load-Config {
    try {
        $cfg = Invoke-BodyJson -Method "GET" -Path "/v1/config"
        if ($cfg.mode) { $cmbMode.SelectedItem = $cfg.mode }
        Set-ControlText $txtCoordinator $cfg.coordinatorUrl
        if ($cfg.instance) {
            Set-ControlText $txtInstanceName $cfg.instance.displayName
            Set-ControlText $txtInstanceId $cfg.instance.instanceId
        }
        if ($cfg.device) {
            Set-ControlText $txtDeviceName $cfg.device.displayName
            Set-ControlText $txtDeviceId $cfg.device.deviceId
        }
        if ($cfg.server) {
            Set-ControlText $txtServerName $cfg.server.displayName
            Set-ControlText $txtServerDir $cfg.server.dir
            Set-ControlText $txtServerId $cfg.server.serverId
        }
        Set-ControlText $txtTokenStatus "not configured"
        if (($cfg.instance -and $cfg.instance.ownerToken -eq "[redacted]") -or ($cfg.compat -and $cfg.compat.legacyHostToken -eq "[redacted]")) {
            Set-ControlText $txtTokenStatus "configured (redacted)"
        }
        if ($cfg.listener) {
            Set-ControlText $txtListenerHost $cfg.listener.localHost
            Set-ControlText $txtListenerPort $cfg.listener.localPort
        }
        if ($cfg.relay) {
            Set-ControlText $txtPublicHost $cfg.relay.publicHost
            Set-ControlText $txtPublicPort $cfg.relay.minecraftPort
        }
        Add-Log "config.json loaded."
    } catch {
        Add-Log ("Config not loaded yet: " + $_.Exception.Message)
    }
}

function Save-Config {
    try {
        $cfg = @{
            schemaVersion = 2
            mode = [string]$cmbMode.SelectedItem
            coordinatorUrl = $txtCoordinator.Text.Trim()
            instance = @{
                instanceId = $txtInstanceId.Text.Trim()
                displayName = $txtInstanceName.Text.Trim()
                ownerToken = "[redacted]"
            }
            device = @{
                deviceId = $txtDeviceId.Text.Trim()
                displayName = $txtDeviceName.Text.Trim()
                platform = "windows"
            }
            server = @{
                serverId = $txtServerId.Text.Trim()
                displayName = $txtServerName.Text.Trim()
                dir = $txtServerDir.Text.Trim()
            }
            compat = @{
                coordinatorProtocol = 2
                legacyHostToken = "[redacted]"
            }
            listener = @{ enabled = $true; localHost = "127.0.0.1"; localPort = 25565 }
            relay = @{ enabled = $true; publicHost = $txtPublicHost.Text.Trim(); coordinatorPort = 6121; minecraftPort = [int]$txtPublicPort.Text.Trim() }
            backup = @{
                profileId = "minecraft-migratable"
                include = @("dir:world","dir:mods","dir:config","dir:defaultconfigs","dir:datapacks","dir:resourcepacks","dir:global_packs","dir:patchouli_books","file:server.properties","file:eula.txt","file:ops.json","file:whitelist.json","file:banned-ips.json","file:banned-players.json","file:server-icon.png","file:manifest.json","file:variables.txt","file:user_jvm_args.txt","file:start.bat","file:start.ps1","file:start.sh","file:run.sh","file:双击直接开服！！！.bat","file:HOW-TO-RUN.md")
                exclude = @("dir:libraries","dir:jre","dir:logs","dir:crash-reports","dir:versions","dir:.cache","dir:cache")
            }
        }
        Invoke-BodyJson -Method "PUT" -Path "/v1/config" -Body $cfg | Out-Null
        Add-Log "config.json saved."
        Refresh-Health
    } catch {
        Show-Error ("Save config failed: " + $_.Exception.Message)
    }
}

function Test-Coordinator {
    try {
        $op = Invoke-BodyJson -Method "GET" -Path "/v1/coordinator/probe"
        $txtOperation.Text = ($op | ConvertTo-Json -Depth 12)
        $txtErrorDetails.Text = ""
        if ($op.state -eq "success") {
            $lblState.Text = "State: coordinator probe ok"
            $txtActualRequestUrl.Text = $op.result.actualRequestUrl
            $txtProtocol.Text = [string]$op.result.capabilities.protocolVersion
            $txtCapabilities.Text = (($op.result.capabilities.capabilities) -join ", ")
            Refresh-Identity
        } else {
            $lblState.Text = "State: coordinator probe failed"
            if ($op.error) {
                $txtActualRequestUrl.Text = $op.error.details.url
                $txtErrorDetails.Text = "errorCode=$($op.error.errorCode) httpStatus=$($op.error.details.httpStatus) responseBody=$($op.error.details.responseBody)"
            }
        }
        Add-Log ("Probe operation: " + $op.operationId + " state=" + $op.state)
    } catch {
        Show-Error ("Coordinator probe failed: " + $_.Exception.Message)
    }
}

function Refresh-Identity {
    try {
        $id = Invoke-BodyJson -Method "GET" -Path "/v1/identity"
        if ($id.instance) {
            Set-ControlText $txtInstanceId $id.instance.instanceId
            Set-ControlText $txtInstanceName $id.instance.displayName
        }
        if ($id.device) {
            Set-ControlText $txtDeviceId $id.device.deviceId
            Set-ControlText $txtDeviceName $id.device.displayName
        }
        if ($id.server) {
            Set-ControlText $txtServerId $id.server.serverId
            Set-ControlText $txtServerName $id.server.displayName
        }
        Set-ControlText $txtTokenStatus "not configured"
        if ($id.compat -and ($id.compat.ownerTokenPresent -or $id.compat.legacyHostTokenPresent)) {
            Set-ControlText $txtTokenStatus "configured (redacted)"
        }
        if ($id.compat) {
            Set-ControlText $txtAdvancedDiagnostics "usesLegacyGroupApi=$($id.compat.usesLegacyGroupApi); legacyGroupIdPresent=$($id.compat.legacyGroupIdPresent); legacyHostIdPresent=$($id.compat.legacyHostIdPresent)"
        }
        Add-Log "Private instance identity refreshed."
    } catch {
        Add-Log ("Identity not loaded yet: " + $_.Exception.Message)
    }
}

function Run-Init {
    try {
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/init"
        $txtOperation.Text = ($op | ConvertTo-Json -Depth 12)
        if ($op.state -eq "success") {
            $lblState.Text = "State: private instance ready"
        } else {
            $lblState.Text = "State: init failed"
        }
        Add-Log ("Init operation: " + $op.operationId + " state=" + $op.state)
    } catch {
        Show-Error ("Init failed: " + $_.Exception.Message)
    }
}

function Save-ListenerConfig {
    try {
        $cfg = @{
            enabled = $true
            localHost = $txtListenerHost.Text.Trim()
            localPort = [int]$txtListenerPort.Text.Trim()
            expectedProcessNames = @("java.exe", "javaw.exe")
            serverDirMatchRequired = $false
        }
        Invoke-BodyJson -Method "PUT" -Path "/v1/listener/config" -Body $cfg | Out-Null
        Add-Log "Listener config saved."
        Refresh-ListenerStatus
    } catch {
        Show-Error ("Save listener config failed: " + $_.Exception.Message)
    }
}

function Set-ListenerFields {
    param([object]$Status)
    if ($null -eq $Status) { return }
    Set-ControlText $txtListening $Status.listening
    Set-ControlText $txtListenerWarnings ""
    if ($Status.warnings) {
        Set-ControlText $txtListenerWarnings (($Status.warnings | ForEach-Object { $_.code + ": " + $_.message }) -join "; ")
    }
    if ($Status.listeners -and $Status.listeners.Count -gt 0) {
        $item = $Status.listeners[0]
        Set-ControlText $txtListenerPid $item.pid
        Set-ControlText $txtListenerProcess $item.processName
        Set-ControlText $txtListenerCommand $item.commandLine
        Set-ControlText $txtServerDirMatched $item.serverDirMatched
    } else {
        Set-ControlText $txtListenerPid ""
        Set-ControlText $txtListenerProcess ""
        Set-ControlText $txtListenerCommand ""
        Set-ControlText $txtServerDirMatched ""
    }
}

function Refresh-ListenerStatus {
    try {
        $status = Invoke-BodyJson -Method "GET" -Path "/v1/listener/status"
        Set-ListenerFields $status
        if (-not $status.listening) {
            Add-Log "ACBH does not start Minecraft. Start your server with MCSL or your own script, then refresh listener status."
        } else {
            Add-Log "Listener status refreshed."
        }
    } catch {
        Show-Error ("Listener status failed: " + $_.Exception.Message)
    }
}

function Probe-Listener {
    try {
        $status = Invoke-BodyJson -Method "POST" -Path "/v1/listener/probe"
        Set-ListenerFields $status
        Add-Log "Listener probe completed."
    } catch {
        Show-Error ("Listener probe failed: " + $_.Exception.Message)
    }
}

function Set-RelayFields {
    param([object]$Relay)
    if ($null -eq $Relay) { return }
    Set-ControlText $txtPublicEndpoint $Relay.publicEndpoint
    Set-ControlText $txtLocalEndpoint $Relay.localEndpoint
    Set-ControlText $txtRelayState "configured=$($Relay.configured) active=$($Relay.active) currentDevice=$($Relay.currentDevice) lastHeartbeatAt=$($Relay.lastHeartbeatAt)"
    Set-ControlText $txtRelayError ""
    if ($Relay.errors) {
        Set-ControlText $txtRelayError (($Relay.errors | ForEach-Object { $_.errorCode + " httpStatus=" + $_.details.httpStatus + " body=" + $_.details.responseBody }) -join "; ")
    }
}

function Configure-Relay {
    try {
        $body = @{
            localMinecraftHost = $txtListenerHost.Text.Trim()
            localMinecraftPort = [int]$txtListenerPort.Text.Trim()
            publicMinecraftPort = [int]$txtPublicPort.Text.Trim()
        }
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/relay/configure" -Body $body
        $txtOperation.Text = ($op | ConvertTo-Json -Depth 12)
        if ($op.state -eq "success") {
            Set-RelayFields $op.result.relay
            Add-Log "Relay configured through body API."
        } elseif ($op.error) {
            $txtRelayError.Text = "errorCode=$($op.error.errorCode) httpStatus=$($op.error.details.httpStatus) responseBody=$($op.error.details.responseBody)"
        }
    } catch {
        Show-Error ("Relay configure failed: " + $_.Exception.Message)
    }
}

function Refresh-RelayStatus {
    try {
        $status = Invoke-BodyJson -Method "GET" -Path "/v1/relay/status"
        Set-RelayFields $status.relay
        Add-Log "Relay status refreshed."
    } catch {
        Show-Error ("Relay status failed: " + $_.Exception.Message)
    }
}

function Set-BackupOperation {
    param([object]$Op)
    $txtOperation.Text = ($Op | ConvertTo-Json -Depth 16)
    if ($Op.state -eq "success") {
        $txtBackupError.Text = ""
    } elseif ($Op.error) {
        $txtBackupError.Text = "errorCode=$($Op.error.errorCode) httpStatus=$($Op.error.details.httpStatus) responseBody=$($Op.error.details.responseBody)"
    }
}

function Wait-BodyOperation {
    param([object]$Op)
    if (-not $Op.operationId) { return $Op }
    while ($Op.state -eq "running") {
        Set-BackupOperation $Op
        [System.Windows.Forms.Application]::DoEvents()
        Start-Sleep -Seconds 2
        $Op = Invoke-BodyJson -Method "GET" -Path ("/v1/operations/" + [uri]::EscapeDataString($Op.operationId))
    }
    Set-BackupOperation $Op
    return $Op
}

function Analyze-Backup {
    try {
        $result = Invoke-BodyJson -Method "POST" -Path "/v1/backup/analyze"
        $txtBackupSummary.Text = "files=$($result.fileCount) roots=$($result.rootCount) size=$($result.logicalSize) profile=$($result.profileId)"
        $txtBackupError.Text = ""
        Add-Log "Backup analysis completed through body API."
    } catch {
        Show-Error ("Backup analyze failed: " + $_.Exception.Message)
    }
}

function Upload-Backup {
    try {
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/backup/upload"
        $op = Wait-BodyOperation $op
        if ($op.state -eq "success") {
            $txtBackupSummary.Text = "snapshot=$($op.result.snapshotId) uploaded=$($op.result.uploadedSize) deduped=$($op.result.deduplicatedSize) actualRequestUrl=$($op.result.actualRequestUrl)"
            Add-Log "Backup uploaded through body API."
        }
    } catch {
        Show-Error ("Backup upload failed: " + $_.Exception.Message)
    }
}

function Refresh-Snapshots {
    try {
        $result = Invoke-BodyJson -Method "GET" -Path "/v1/snapshots"
        $txtSnapshotList.Text = ($result.snapshots | ConvertTo-Json -Depth 8)
        if ($result.snapshots -and $result.snapshots.Count -gt 0) {
            $txtSnapshotId.Text = $result.snapshots[0].snapshotId
        }
        Add-Log "Snapshot list refreshed through body API."
    } catch {
        Show-Error ("Snapshot list failed: " + $_.Exception.Message)
    }
}

function Choose-RestoreDir {
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = "Choose a new empty restore directory"
    if ($txtRestoreTargetDir.Text) { $dialog.SelectedPath = $txtRestoreTargetDir.Text }
    if ($dialog.ShowDialog($form) -eq [System.Windows.Forms.DialogResult]::OK) {
        $txtRestoreTargetDir.Text = $dialog.SelectedPath
    }
}

function Download-LatestSnapshot {
    try {
        $body = @{ targetDir = $txtRestoreTargetDir.Text.Trim(); allowNonEmpty = $chkAllowNonEmpty.Checked }
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/snapshots/latest/download" -Body $body
        $op = Wait-BodyOperation $op
        if ($op.state -eq "success") {
            $txtBackupSummary.Text = "downloaded=$($op.result.downloadedFiles) snapshot=$($op.result.snapshotId) target=$($op.result.targetDir)"
            Add-Log "Latest snapshot downloaded through body API."
        }
    } catch {
        Show-Error ("Latest snapshot download failed: " + $_.Exception.Message)
    }
}

function Download-SelectedSnapshot {
    try {
        $snapshotId = $txtSnapshotId.Text.Trim()
        if (-not $snapshotId) { throw "Snapshot ID is required." }
        $body = @{ targetDir = $txtRestoreTargetDir.Text.Trim(); allowNonEmpty = $chkAllowNonEmpty.Checked }
        $op = Invoke-BodyJson -Method "POST" -Path ("/v1/snapshots/" + [uri]::EscapeDataString($snapshotId) + "/download") -Body $body
        $op = Wait-BodyOperation $op
        if ($op.state -eq "success") {
            $txtBackupSummary.Text = "downloaded=$($op.result.downloadedFiles) snapshot=$($op.result.snapshotId) target=$($op.result.targetDir)"
            Add-Log "Selected snapshot downloaded through body API."
        }
    } catch {
        Show-Error ("Snapshot download failed: " + $_.Exception.Message)
    }
}

function Choose-ServerDir {
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = "Choose Minecraft server directory"
    if ($txtServerDir.Text) { $dialog.SelectedPath = $txtServerDir.Text }
    if ($dialog.ShowDialog($form) -eq [System.Windows.Forms.DialogResult]::OK) {
        $txtServerDir.Text = $dialog.SelectedPath
    }
}

function Add-Label {
    param([string]$Text, [int]$X, [int]$Y)
    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Text
    $label.Location = New-Object System.Drawing.Point($X, $Y)
    $label.Size = New-Object System.Drawing.Size(160, 24)
    $form.Controls.Add($label)
    return $label
}

function Add-TextBox {
    param([int]$X, [int]$Y, [int]$W, [bool]$ReadOnly = $false)
    $box = New-Object System.Windows.Forms.TextBox
    $box.Location = New-Object System.Drawing.Point($X, $Y)
    $box.Size = New-Object System.Drawing.Size($W, 24)
    $box.ReadOnly = $ReadOnly
    $form.Controls.Add($box)
    return $box
}

function Add-Button {
    param([string]$Text, [int]$X, [int]$Y, [scriptblock]$Click)
    $button = New-Object System.Windows.Forms.Button
    $button.Text = $Text
    $button.Location = New-Object System.Drawing.Point($X, $Y)
    $button.Size = New-Object System.Drawing.Size(150, 32)
    $button.Add_Click({ try { & $Click } catch { Show-Error $_.Exception.Message } }.GetNewClosure())
    $form.Controls.Add($button)
    return $button
}

$form = New-Object System.Windows.Forms.Form
$form.Text = "ACBH v0.5 Minimal Core"
$form.StartPosition = "CenterScreen"
$form.Size = New-Object System.Drawing.Size(1120, 860)
$form.MinimumSize = New-Object System.Drawing.Size(1000, 740)
$form.AutoScaleMode = [System.Windows.Forms.AutoScaleMode]::Font
$form.AutoScroll = $true
$form.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 9)

function Fit-FormToWorkingArea {
    param([System.Windows.Forms.Form]$Form)
    if ($null -eq $Form) { return }
    $area = [System.Windows.Forms.Screen]::FromControl($Form).WorkingArea
    $margin = 40
    if ($Form.Width -gt ($area.Width - $margin)) {
        $Form.Width = $area.Width - $margin
    }
    if ($Form.Height -gt ($area.Height - $margin)) {
        $Form.Height = $area.Height - $margin
    }
    if ($Form.Right -gt ($area.Right - 12)) {
        $Form.Left = [Math]::Max($area.Left + 12, ($area.Right - $Form.Width - 12))
    }
    if ($Form.Bottom -gt ($area.Bottom - 12)) {
        $Form.Top = [Math]::Max($area.Top + 12, ($area.Bottom - $Form.Height - 12))
    }
    if ($Form.Left -lt ($area.Left + 12)) {
        $Form.Left = $area.Left + 12
    }
    if ($Form.Top -lt ($area.Top + 12)) {
        $Form.Top = $area.Top + 12
    }
}

$title = New-Object System.Windows.Forms.Label
$title.Text = "ACBH v0.5 Minimal Core"
$title.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 18, [System.Drawing.FontStyle]::Bold)
$title.Location = New-Object System.Drawing.Point(20, 16)
$title.Size = New-Object System.Drawing.Size(420, 36)
$form.Controls.Add($title)

$lblState = New-Object System.Windows.Forms.Label
$lblState.Text = "State: starting"
$lblState.Location = New-Object System.Drawing.Point(620, 24)
$lblState.Size = New-Object System.Drawing.Size(420, 24)
$form.Controls.Add($lblState)

Add-Label "Body API" 24 70 | Out-Null
$txtBodyApi = Add-TextBox 190 68 820 $true
Add-Label "Config path" 24 104 | Out-Null
$txtConfigPath = Add-TextBox 190 102 820 $true
Add-Label "Mode" 24 138 | Out-Null
$cmbMode = New-Object System.Windows.Forms.ComboBox
$cmbMode.Location = New-Object System.Drawing.Point(190, 136)
$cmbMode.Size = New-Object System.Drawing.Size(220, 24)
$cmbMode.DropDownStyle = "DropDownList"
[void]$cmbMode.Items.Add("remote-public")
[void]$cmbMode.Items.Add("local-private")
$cmbMode.SelectedItem = "remote-public"
$form.Controls.Add($cmbMode)

Add-Label "VPS 地址" 24 172 | Out-Null
$txtCoordinator = Add-TextBox 190 170 820
$txtCoordinator.Text = "http://YOUR_VPS_IP:6121"
Add-Label "私人实例" 24 206 | Out-Null
$txtInstanceName = Add-TextBox 190 204 320
$txtInstanceName.Text = "私人 ACBH 实例"
Add-Label "实例 ID" 540 206 | Out-Null
$txtInstanceId = Add-TextBox 650 204 360 $true
Add-Label "当前设备" 24 240 | Out-Null
$txtDeviceName = Add-TextBox 190 238 320
$txtDeviceName.Text = $env:COMPUTERNAME
Add-Label "设备 ID" 540 240 | Out-Null
$txtDeviceId = Add-TextBox 650 238 360 $true
Add-Label "服务端名称" 24 274 | Out-Null
$txtServerName = Add-TextBox 190 272 320
$txtServerName.Text = "Minecraft 服务端"
Add-Label "访问令牌状态" 540 274 | Out-Null
$txtTokenStatus = Add-TextBox 650 272 360 $true
Add-Label "服务端目录" 24 308 | Out-Null
$txtServerDir = Add-TextBox 190 306 630
Add-Button "Choose dir" 840 302 { Choose-ServerDir } | Out-Null

Add-Label "实际请求 URL" 24 342 | Out-Null
$txtActualRequestUrl = Add-TextBox 190 340 820 $true
Add-Label "协议版本" 24 376 | Out-Null
$txtProtocol = Add-TextBox 190 374 160 $true
Add-Label "能力" 380 376 | Out-Null
$txtCapabilities = Add-TextBox 500 374 510 $true
Add-Label "错误详情" 24 410 | Out-Null
$txtErrorDetails = Add-TextBox 190 408 820 $true
$txtServerId = Add-TextBox 24 442 120 $true
$txtServerId.Visible = $false
$txtAdvancedDiagnostics = Add-TextBox 190 442 820 $true

Add-Button "Refresh health" 190 480 { Refresh-Health; Refresh-Identity } | Out-Null
Add-Button "Load config" 350 480 { Load-Config; Refresh-Identity } | Out-Null
Add-Button "Save config" 510 480 { Save-Config } | Out-Null
Add-Button "Test connection" 670 480 { Test-Coordinator } | Out-Null
Add-Button "Initialize" 190 518 { Run-Init } | Out-Null

$txtOperation = New-Object System.Windows.Forms.TextBox
$txtOperation.Location = New-Object System.Drawing.Point(24, 566)
$txtOperation.Size = New-Object System.Drawing.Size(986, 78)
$txtOperation.Multiline = $true
$txtOperation.ScrollBars = "Vertical"
$txtOperation.ReadOnly = $true
$txtOperation.Font = New-Object System.Drawing.Font("Consolas", 9)
$form.Controls.Add($txtOperation)

$grpListenerRelay = New-Object System.Windows.Forms.GroupBox
$grpListenerRelay.Text = "监听 / VPS 中转"
$grpListenerRelay.Location = New-Object System.Drawing.Point(24, 662)
$grpListenerRelay.Size = New-Object System.Drawing.Size(986, 250)
$form.Controls.Add($grpListenerRelay)

function Add-GroupLabel {
    param([string]$Text, [int]$X, [int]$Y)
    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Text
    $label.Location = New-Object System.Drawing.Point($X, $Y)
    $label.Size = New-Object System.Drawing.Size(120, 24)
    $grpListenerRelay.Controls.Add($label)
    return $label
}

function Add-GroupTextBox {
    param([int]$X, [int]$Y, [int]$W, [bool]$ReadOnly = $false)
    $box = New-Object System.Windows.Forms.TextBox
    $box.Location = New-Object System.Drawing.Point($X, $Y)
    $box.Size = New-Object System.Drawing.Size($W, 24)
    $box.ReadOnly = $ReadOnly
    $grpListenerRelay.Controls.Add($box)
    return $box
}

function Add-GroupButton {
    param([string]$Text, [int]$X, [int]$Y, [scriptblock]$Click)
    $button = New-Object System.Windows.Forms.Button
    $button.Text = $Text
    $button.Location = New-Object System.Drawing.Point($X, $Y)
    $button.Size = New-Object System.Drawing.Size(150, 30)
    $button.Add_Click({ try { & $Click } catch { Show-Error $_.Exception.Message } }.GetNewClosure())
    $grpListenerRelay.Controls.Add($button)
    return $button
}

Add-GroupLabel "Local host" 16 28 | Out-Null
$txtListenerHost = Add-GroupTextBox 130 26 220
$txtListenerHost.Text = "127.0.0.1"
Add-GroupLabel "Local port" 380 28 | Out-Null
$txtListenerPort = Add-GroupTextBox 490 26 90
$txtListenerPort.Text = "25565"
Add-GroupLabel "Public host" 610 28 | Out-Null
$txtPublicHost = Add-GroupTextBox 720 26 180
Add-GroupLabel "Public port" 16 64 | Out-Null
$txtPublicPort = Add-GroupTextBox 130 62 90
$txtPublicPort.Text = "25565"

Add-GroupButton "Save listener" 240 62 { Save-ListenerConfig } | Out-Null
Add-GroupButton "Refresh listener" 396 62 { Refresh-ListenerStatus } | Out-Null
Add-GroupButton "Probe listener" 552 62 { Probe-Listener } | Out-Null
Add-GroupButton "Configure relay" 708 62 { Configure-Relay } | Out-Null
Add-GroupButton "Relay status" 824 96 { Refresh-RelayStatus } | Out-Null

Add-GroupLabel "Listening" 16 108 | Out-Null
$txtListening = Add-GroupTextBox 130 106 90 $true
Add-GroupLabel "PID" 248 108 | Out-Null
$txtListenerPid = Add-GroupTextBox 300 106 100 $true
Add-GroupLabel "Process" 430 108 | Out-Null
$txtListenerProcess = Add-GroupTextBox 520 106 250 $true
Add-GroupLabel "Dir matched" 760 108 | Out-Null
$txtServerDirMatched = Add-GroupTextBox 880 106 90 $true
Add-GroupLabel "Local endpoint" 16 142 | Out-Null
$txtLocalEndpoint = Add-GroupTextBox 130 140 270 $true
Add-GroupLabel "Public endpoint" 430 142 | Out-Null
$txtPublicEndpoint = Add-GroupTextBox 560 140 410 $true
Add-GroupLabel "Command" 16 176 | Out-Null
$txtListenerCommand = Add-GroupTextBox 130 174 370 $true
Add-GroupLabel "Warnings" 520 176 | Out-Null
$txtListenerWarnings = Add-GroupTextBox 620 174 350 $true
$txtRelayState = Add-GroupTextBox 16 210 460 $true
$txtRelayError = Add-GroupTextBox 510 210 460 $true

$grpBackup = New-Object System.Windows.Forms.GroupBox
$grpBackup.Text = "备份 / 快照"
$grpBackup.Location = New-Object System.Drawing.Point(24, 930)
$grpBackup.Size = New-Object System.Drawing.Size(986, 250)
$form.Controls.Add($grpBackup)

function Add-BackupLabel {
    param([string]$Text, [int]$X, [int]$Y)
    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Text
    $label.Location = New-Object System.Drawing.Point($X, $Y)
    $label.Size = New-Object System.Drawing.Size(120, 24)
    $grpBackup.Controls.Add($label)
    return $label
}

function Add-BackupTextBox {
    param([int]$X, [int]$Y, [int]$W, [bool]$ReadOnly = $false)
    $box = New-Object System.Windows.Forms.TextBox
    $box.Location = New-Object System.Drawing.Point($X, $Y)
    $box.Size = New-Object System.Drawing.Size($W, 24)
    $box.ReadOnly = $ReadOnly
    $grpBackup.Controls.Add($box)
    return $box
}

function Add-BackupButton {
    param([string]$Text, [int]$X, [int]$Y, [scriptblock]$Click)
    $button = New-Object System.Windows.Forms.Button
    $button.Text = $Text
    $button.Location = New-Object System.Drawing.Point($X, $Y)
    $button.Size = New-Object System.Drawing.Size(170, 30)
    $button.Add_Click({ try { & $Click } catch { Show-Error $_.Exception.Message } }.GetNewClosure())
    $grpBackup.Controls.Add($button)
    return $button
}

Add-BackupButton "Analyze backup" 16 26 { Analyze-Backup } | Out-Null
Add-BackupButton "Upload backup" 198 26 { Upload-Backup } | Out-Null
Add-BackupButton "List snapshots" 380 26 { Refresh-Snapshots } | Out-Null
Add-BackupButton "Download latest" 562 26 { Download-LatestSnapshot } | Out-Null
Add-BackupButton "Download selected" 744 26 { Download-SelectedSnapshot } | Out-Null

Add-BackupLabel "Snapshot ID" 16 66 | Out-Null
$txtSnapshotId = Add-BackupTextBox 130 64 260
Add-BackupLabel "Restore target" 420 66 | Out-Null
$txtRestoreTargetDir = Add-BackupTextBox 540 64 260
Add-BackupButton "Choose target" 810 61 { Choose-RestoreDir } | Out-Null
$chkAllowNonEmpty = New-Object System.Windows.Forms.CheckBox
$chkAllowNonEmpty.Text = "allow non-empty target"
$chkAllowNonEmpty.Location = New-Object System.Drawing.Point(130, 96)
$chkAllowNonEmpty.Size = New-Object System.Drawing.Size(220, 24)
$grpBackup.Controls.Add($chkAllowNonEmpty)

Add-BackupLabel "Summary" 16 128 | Out-Null
$txtBackupSummary = Add-BackupTextBox 130 126 840 $true
Add-BackupLabel "Snapshots" 16 160 | Out-Null
$txtSnapshotList = Add-BackupTextBox 130 158 360 $true
Add-BackupLabel "Error" 510 160 | Out-Null
$txtBackupError = Add-BackupTextBox 610 158 360 $true

$txtLog = New-Object System.Windows.Forms.TextBox
$txtLog.Location = New-Object System.Drawing.Point(24, 1198)
$txtLog.Size = New-Object System.Drawing.Size(986, 120)
$txtLog.Multiline = $true
$txtLog.ScrollBars = "Vertical"
$txtLog.ReadOnly = $true
$txtLog.Font = New-Object System.Drawing.Font("Consolas", 9)
$form.Controls.Add($txtLog)
foreach ($line in $script:StartupErrors) {
    $txtLog.AppendText(($line + [Environment]::NewLine))
}

$form.Add_Shown({
    try {
        Fit-FormToWorkingArea $form
        Start-Body
        Refresh-Health
        Load-Config
        Refresh-Identity
        if ($lblState.Text -eq "State: starting") {
            $lblState.Text = "State: ready"
        }
    } catch {
        Show-Error (Format-ExceptionDetails -ErrorRecord $_ -Context "Startup failed")
    }
})

$form.Add_FormClosed({
    if ($script:BodyProcess -and -not $script:BodyProcess.HasExited) {
        try { $script:BodyProcess.Kill() } catch { }
    }
})

[void]$form.ShowDialog()
