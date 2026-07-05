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
        "Format-BodyError",
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
        "备份 / 快照",
        "私人实例",
        "当前设备",
        "VPS 中转",
        "访问令牌状态",
        '$script:BodyUrl'
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
        ("super" + "visor"),
        ("take over " + "elec" + "tion")
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

function Redact-Secrets {
    param([string]$Text)
    if ([string]::IsNullOrEmpty($Text)) { return $Text }
    $safe = $Text
    $tokenPattern = "(?i)(hostToken|ownerToken|legacyHostToken|accessKey)\s*[:=]\s*\S+"
    $safe = [regex]::Replace($safe, $tokenPattern, "[hidden]")
    $safe = [regex]::Replace($safe, "ht_[A-Za-z0-9_\-]+", "ht_[hidden]")
    return $safe
}

function Format-BodyError {
    param([object]$ErrorRecord)
    $fallback = $ErrorRecord.Exception.Message
    $rawBody = $null
    if ($ErrorRecord.ErrorDetails -and $ErrorRecord.ErrorDetails.Message) {
        $rawBody = $ErrorRecord.ErrorDetails.Message
    }
    if ([string]::IsNullOrWhiteSpace($rawBody)) {
        return $fallback
    }

    try {
        $payload = $rawBody | ConvertFrom-Json
    } catch {
        return $fallback
    }

    $err = $payload
    if ($payload.error) {
        $err = $payload.error
    }
    $code = [string]$err.errorCode
    $message = [string]$err.message
    $suggestion = [string]$err.suggestion
    $configPath = ""
    if ($err.details -and $err.details.configPath) {
        $configPath = [string]$err.details.configPath
    }

    switch ($code) {
        "config_missing" {
            $text = "未找到配置文件，程序已尝试自动创建。请检查配置目录权限"
            if ($configPath) { $text += "：" + [Environment]::NewLine + $configPath }
            return $text
        }
        "config_write_failed" {
            $text = "无法写入配置文件。请检查配置目录权限"
            if ($configPath) { $text += "：" + [Environment]::NewLine + $configPath }
            if ($message) { $text += [Environment]::NewLine + $message }
            return $text
        }
        "config_parse_error" {
            $text = "配置文件格式错误，程序不会自动覆盖现有文件。请修复 config.json 后重试"
            if ($configPath) { $text += "：" + [Environment]::NewLine + $configPath }
            return $text
        }
        "config_invalid" {
            $text = "配置还不完整或格式不正确"
            if ($message) { $text += "：" + $message }
            if ($configPath) { $text += [Environment]::NewLine + $configPath }
            if ($suggestion) { $text += [Environment]::NewLine + $suggestion }
            return $text
        }
        "identity_incomplete" {
            $text = "访问令牌或私有实例身份尚未配置完整。请先保存或迁移包含令牌的配置。"
            if ($configPath) { $text += [Environment]::NewLine + $configPath }
            return $text
        }
        default {
            if ($code -or $message) {
                $text = "请求失败"
                if ($code) { $text += "（" + $code + "）" }
                if ($message) { $text += "：" + $message }
                if ($suggestion) { $text += [Environment]::NewLine + $suggestion }
                return $text
            }
        }
    }

    return $fallback
}

function Add-Log {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    $txtLog.AppendText(("[" + (Get-Date -Format "HH:mm:ss") + "] " + (Redact-Secrets $Text) + [Environment]::NewLine))
    $txtLog.SelectionStart = $txtLog.TextLength
    $txtLog.ScrollToCaret()
}

function Show-Error {
    param([string]$Message)
    Add-Log $Message
    [System.Windows.Forms.MessageBox]::Show((Redact-Secrets $Message), "ACBH", "OK", "Error") | Out-Null
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
        throw "Cannot find ACBH Agent: $AgentPath"
    }
    New-Item -ItemType Directory -Force -Path $AppDataDir | Out-Null
    $args = @("body", "serve", "--listen", $BodyListen, "--app-data-dir", $AppDataDir)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $AgentPath
    foreach ($arg in $args) {
        [void]$psi.ArgumentList.Add($arg)
    }
    $psi.WorkingDirectory = Split-Path -Parent $AgentPath
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $script:BodyProcess = [System.Diagnostics.Process]::Start($psi)
    Start-Sleep -Milliseconds 600
    if (-not (Test-BodyPort)) {
        throw "Body API did not start on $BodyListen"
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
        throw (Format-BodyError $_)
    }
}

function Refresh-Health {
    try {
        $health = Invoke-BodyJson -Method "GET" -Path "/v1/body/health"
        $txtConfigPath.Text = $health.configPath
        $txtBodyApi.Text = $health.bodyApi
        if ($health.coordinatorUrl) { $txtCoordinator.Text = $health.coordinatorUrl }
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
        $cmbMode.SelectedItem = $cfg.mode
        $txtCoordinator.Text = $cfg.coordinatorUrl
        $txtInstanceName.Text = $cfg.instance.displayName
        $txtInstanceId.Text = $cfg.instance.instanceId
        $txtDeviceName.Text = $cfg.device.displayName
        $txtDeviceId.Text = $cfg.device.deviceId
        $txtServerName.Text = $cfg.server.displayName
        $txtServerDir.Text = $cfg.server.dir
        $txtTokenStatus.Text = "not configured"
        if ($cfg.instance.ownerToken -eq "[redacted]" -or $cfg.compat.legacyHostToken -eq "[redacted]") {
            $txtTokenStatus.Text = "configured (redacted)"
        }
        if ($cfg.listener) {
            $txtListenerHost.Text = $cfg.listener.localHost
            $txtListenerPort.Text = [string]$cfg.listener.localPort
        }
        if ($cfg.relay) {
            $txtPublicHost.Text = $cfg.relay.publicHost
            $txtPublicPort.Text = [string]$cfg.relay.minecraftPort
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
        $txtInstanceId.Text = $id.instance.instanceId
        $txtInstanceName.Text = $id.instance.displayName
        $txtDeviceId.Text = $id.device.deviceId
        $txtDeviceName.Text = $id.device.displayName
        $txtServerId.Text = $id.server.serverId
        $txtServerName.Text = $id.server.displayName
        $txtTokenStatus.Text = "not configured"
        if ($id.compat.ownerTokenPresent -or $id.compat.legacyHostTokenPresent) {
            $txtTokenStatus.Text = "configured (redacted)"
        }
        $txtAdvancedDiagnostics.Text = "usesLegacyGroupApi=$($id.compat.usesLegacyGroupApi); legacyGroupIdPresent=$($id.compat.legacyGroupIdPresent); legacyHostIdPresent=$($id.compat.legacyHostIdPresent)"
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
    $txtListening.Text = [string]$Status.listening
    $txtListenerWarnings.Text = ""
    if ($Status.warnings) {
        $txtListenerWarnings.Text = (($Status.warnings | ForEach-Object { $_.code + ": " + $_.message }) -join "; ")
    }
    if ($Status.listeners -and $Status.listeners.Count -gt 0) {
        $item = $Status.listeners[0]
        $txtListenerPid.Text = [string]$item.pid
        $txtListenerProcess.Text = [string]$item.processName
        $txtListenerCommand.Text = [string]$item.commandLine
        $txtServerDirMatched.Text = [string]$item.serverDirMatched
    } else {
        $txtListenerPid.Text = ""
        $txtListenerProcess.Text = ""
        $txtListenerCommand.Text = ""
        $txtServerDirMatched.Text = ""
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
    $txtPublicEndpoint.Text = [string]$Relay.publicEndpoint
    $txtLocalEndpoint.Text = [string]$Relay.localEndpoint
    $txtRelayState.Text = "configured=$($Relay.configured) active=$($Relay.active) currentDevice=$($Relay.currentDevice) lastHeartbeatAt=$($Relay.lastHeartbeatAt)"
    $txtRelayError.Text = ""
    if ($Relay.errors) {
        $txtRelayError.Text = (($Relay.errors | ForEach-Object { $_.errorCode + " httpStatus=" + $_.details.httpStatus + " body=" + $_.details.responseBody }) -join "; ")
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
    $label.Size = New-Object System.Drawing.Size(140, 22)
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
    $button.Size = New-Object System.Drawing.Size(150, 30)
    $button.Add_Click({ try { & $Click } catch { Show-Error $_.Exception.Message } }.GetNewClosure())
    $form.Controls.Add($button)
    return $button
}

$form = New-Object System.Windows.Forms.Form
$form.Text = "ACBH v0.5 Minimal Core"
$form.StartPosition = "CenterScreen"
$form.Size = New-Object System.Drawing.Size(920, 1240)
$form.MinimumSize = New-Object System.Drawing.Size(880, 900)
$form.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 9)

$title = New-Object System.Windows.Forms.Label
$title.Text = "ACBH v0.5 Minimal Core"
$title.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 18, [System.Drawing.FontStyle]::Bold)
$title.Location = New-Object System.Drawing.Point(20, 16)
$title.Size = New-Object System.Drawing.Size(420, 36)
$form.Controls.Add($title)

$lblState = New-Object System.Windows.Forms.Label
$lblState.Text = "State: starting"
$lblState.Location = New-Object System.Drawing.Point(460, 24)
$lblState.Size = New-Object System.Drawing.Size(380, 24)
$form.Controls.Add($lblState)

Add-Label "Body API" 24 70 | Out-Null
$txtBodyApi = Add-TextBox 170 68 650 $true
Add-Label "Config path" 24 104 | Out-Null
$txtConfigPath = Add-TextBox 170 102 650 $true
Add-Label "Mode" 24 138 | Out-Null
$cmbMode = New-Object System.Windows.Forms.ComboBox
$cmbMode.Location = New-Object System.Drawing.Point(170, 136)
$cmbMode.Size = New-Object System.Drawing.Size(180, 24)
$cmbMode.DropDownStyle = "DropDownList"
[void]$cmbMode.Items.Add("remote-public")
[void]$cmbMode.Items.Add("local-private")
$cmbMode.SelectedItem = "remote-public"
$form.Controls.Add($cmbMode)

Add-Label "VPS 地址" 24 172 | Out-Null
$txtCoordinator = Add-TextBox 170 170 650
$txtCoordinator.Text = "http://YOUR_VPS_IP:6121"
Add-Label "私人实例" 24 206 | Out-Null
$txtInstanceName = Add-TextBox 170 204 250
$txtInstanceName.Text = "私人 ACBH 实例"
Add-Label "实例 ID" 450 206 | Out-Null
$txtInstanceId = Add-TextBox 560 204 260 $true
Add-Label "当前设备" 24 240 | Out-Null
$txtDeviceName = Add-TextBox 170 238 250
$txtDeviceName.Text = $env:COMPUTERNAME
Add-Label "设备 ID" 450 240 | Out-Null
$txtDeviceId = Add-TextBox 560 238 260 $true
Add-Label "服务端名称" 24 274 | Out-Null
$txtServerName = Add-TextBox 170 272 250
$txtServerName.Text = "Minecraft 服务端"
Add-Label "访问令牌状态" 450 274 | Out-Null
$txtTokenStatus = Add-TextBox 560 272 260 $true
Add-Label "服务端目录" 24 308 | Out-Null
$txtServerDir = Add-TextBox 170 306 500
Add-Button "Choose dir" 680 303 { Choose-ServerDir } | Out-Null

Add-Label "实际请求 URL" 24 342 | Out-Null
$txtActualRequestUrl = Add-TextBox 170 340 650 $true
Add-Label "协议版本" 24 376 | Out-Null
$txtProtocol = Add-TextBox 170 374 120 $true
Add-Label "能力" 310 376 | Out-Null
$txtCapabilities = Add-TextBox 420 374 400 $true
Add-Label "错误详情" 24 410 | Out-Null
$txtErrorDetails = Add-TextBox 170 408 650 $true
$txtServerId = Add-TextBox 24 442 120 $true
$txtServerId.Visible = $false
$txtAdvancedDiagnostics = Add-TextBox 170 442 650 $true

Add-Button "Refresh health" 170 450 { Refresh-Health; Refresh-Identity } | Out-Null
Add-Button "Load config" 330 450 { Load-Config; Refresh-Identity } | Out-Null
Add-Button "Save config" 490 450 { Save-Config } | Out-Null
Add-Button "Test connection" 170 488 { Test-Coordinator } | Out-Null
Add-Button "Initialize" 330 488 { Run-Init } | Out-Null

$txtOperation = New-Object System.Windows.Forms.TextBox
$txtOperation.Location = New-Object System.Drawing.Point(24, 528)
$txtOperation.Size = New-Object System.Drawing.Size(840, 70)
$txtOperation.Multiline = $true
$txtOperation.ScrollBars = "Vertical"
$txtOperation.ReadOnly = $true
$txtOperation.Font = New-Object System.Drawing.Font("Consolas", 9)
$form.Controls.Add($txtOperation)

$txtLog = New-Object System.Windows.Forms.TextBox
$txtLog.Location = New-Object System.Drawing.Point(24, 612)
$txtLog.Size = New-Object System.Drawing.Size(840, 48)
$txtLog.Multiline = $true
$txtLog.ScrollBars = "Vertical"
$txtLog.ReadOnly = $true
$txtLog.Font = New-Object System.Drawing.Font("Consolas", 9)
$form.Controls.Add($txtLog)

$grpListenerRelay = New-Object System.Windows.Forms.GroupBox
$grpListenerRelay.Text = "监听 / VPS 中转"
$grpListenerRelay.Location = New-Object System.Drawing.Point(24, 674)
$grpListenerRelay.Size = New-Object System.Drawing.Size(840, 236)
$form.Controls.Add($grpListenerRelay)

function Add-GroupLabel {
    param([string]$Text, [int]$X, [int]$Y)
    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Text
    $label.Location = New-Object System.Drawing.Point($X, $Y)
    $label.Size = New-Object System.Drawing.Size(120, 22)
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
    $button.Size = New-Object System.Drawing.Size(130, 28)
    $button.Add_Click({ try { & $Click } catch { Show-Error $_.Exception.Message } }.GetNewClosure())
    $grpListenerRelay.Controls.Add($button)
    return $button
}

Add-GroupLabel "Local host" 16 28 | Out-Null
$txtListenerHost = Add-GroupTextBox 118 26 140
$txtListenerHost.Text = "127.0.0.1"
Add-GroupLabel "Local port" 270 28 | Out-Null
$txtListenerPort = Add-GroupTextBox 368 26 80
$txtListenerPort.Text = "25565"
Add-GroupLabel "Public host" 470 28 | Out-Null
$txtPublicHost = Add-GroupTextBox 568 26 140
Add-GroupLabel "Public port" 16 62 | Out-Null
$txtPublicPort = Add-GroupTextBox 118 60 80
$txtPublicPort.Text = "25565"

Add-GroupButton "Save listener" 220 58 { Save-ListenerConfig } | Out-Null
Add-GroupButton "Refresh listener" 360 58 { Refresh-ListenerStatus } | Out-Null
Add-GroupButton "Probe listener" 500 58 { Probe-Listener } | Out-Null
Add-GroupButton "Configure relay" 640 58 { Configure-Relay } | Out-Null
Add-GroupButton "Relay status" 640 92 { Refresh-RelayStatus } | Out-Null

Add-GroupLabel "Listening" 16 96 | Out-Null
$txtListening = Add-GroupTextBox 118 94 80 $true
Add-GroupLabel "PID" 214 96 | Out-Null
$txtListenerPid = Add-GroupTextBox 258 94 80 $true
Add-GroupLabel "Process" 350 96 | Out-Null
$txtListenerProcess = Add-GroupTextBox 430 94 190 $true
Add-GroupLabel "Dir matched" 16 130 | Out-Null
$txtServerDirMatched = Add-GroupTextBox 118 128 80 $true
Add-GroupLabel "Local endpoint" 214 130 | Out-Null
$txtLocalEndpoint = Add-GroupTextBox 318 128 160 $true
Add-GroupLabel "Public endpoint" 490 130 | Out-Null
$txtPublicEndpoint = Add-GroupTextBox 600 128 210 $true
Add-GroupLabel "Command" 16 160 | Out-Null
$txtListenerCommand = Add-GroupTextBox 118 158 350 $true
Add-GroupLabel "Warnings" 482 160 | Out-Null
$txtListenerWarnings = Add-GroupTextBox 560 158 250 $true
$txtRelayState = Add-GroupTextBox 16 190 390 $true
$txtRelayError = Add-GroupTextBox 420 190 390 $true

$grpBackup = New-Object System.Windows.Forms.GroupBox
$grpBackup.Text = "备份 / 快照"
$grpBackup.Location = New-Object System.Drawing.Point(24, 924)
$grpBackup.Size = New-Object System.Drawing.Size(840, 230)
$form.Controls.Add($grpBackup)

function Add-BackupLabel {
    param([string]$Text, [int]$X, [int]$Y)
    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Text
    $label.Location = New-Object System.Drawing.Point($X, $Y)
    $label.Size = New-Object System.Drawing.Size(120, 22)
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
    $button.Size = New-Object System.Drawing.Size(130, 28)
    $button.Add_Click({ try { & $Click } catch { Show-Error $_.Exception.Message } }.GetNewClosure())
    $grpBackup.Controls.Add($button)
    return $button
}

Add-BackupButton "Analyze backup" 16 26 { Analyze-Backup } | Out-Null
Add-BackupButton "Upload backup" 156 26 { Upload-Backup } | Out-Null
Add-BackupButton "List snapshots" 296 26 { Refresh-Snapshots } | Out-Null
Add-BackupButton "Download latest" 436 26 { Download-LatestSnapshot } | Out-Null
Add-BackupButton "Download selected" 576 26 { Download-SelectedSnapshot } | Out-Null

Add-BackupLabel "Snapshot ID" 16 66 | Out-Null
$txtSnapshotId = Add-BackupTextBox 118 64 210
Add-BackupLabel "Restore target" 344 66 | Out-Null
$txtRestoreTargetDir = Add-BackupTextBox 450 64 220
Add-BackupButton "Choose target" 680 61 { Choose-RestoreDir } | Out-Null
$chkAllowNonEmpty = New-Object System.Windows.Forms.CheckBox
$chkAllowNonEmpty.Text = "allow non-empty target"
$chkAllowNonEmpty.Location = New-Object System.Drawing.Point(118, 96)
$chkAllowNonEmpty.Size = New-Object System.Drawing.Size(180, 24)
$grpBackup.Controls.Add($chkAllowNonEmpty)

Add-BackupLabel "Summary" 16 128 | Out-Null
$txtBackupSummary = Add-BackupTextBox 118 126 690 $true
Add-BackupLabel "Snapshots" 16 160 | Out-Null
$txtSnapshotList = Add-BackupTextBox 118 158 340 $true
Add-BackupLabel "Error" 470 160 | Out-Null
$txtBackupError = Add-BackupTextBox 530 158 278 $true

$form.Add_Shown({
    try {
        Start-Body
        Refresh-Health
        Load-Config
        Refresh-Identity
    } catch {
        Show-Error ("Startup failed: " + $_.Exception.Message)
    }
})

$form.Add_FormClosed({
    if ($script:BodyProcess -and -not $script:BodyProcess.HasExited) {
        try { $script:BodyProcess.Kill() } catch { }
    }
})

[void]$form.ShowDialog()
