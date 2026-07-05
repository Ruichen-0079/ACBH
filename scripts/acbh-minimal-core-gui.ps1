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
        "txtTunnelConnected",
        "txtPublicListenerActive",
        "txtActiveConnections",
        "启动公网中转",
        "停止公网中转",
        "刷新中转状态",
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
        "使用方式",
        "刷新状态",
        "加载配置",
        "保存配置",
        "测试连接",
        "初始化",
        "分析备份",
        "上传备份",
        "列出快照",
        "下载最新",
        "下载所选",
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
        [string]$Context = "操作失败"
    )
    $line = "未知"
    $function = "未知"
    if ($ErrorRecord -and $ErrorRecord.InvocationInfo) {
        if ($ErrorRecord.InvocationInfo.ScriptLineNumber) { $line = [string]$ErrorRecord.InvocationInfo.ScriptLineNumber }
        if ($ErrorRecord.InvocationInfo.MyCommand -and $ErrorRecord.InvocationInfo.MyCommand.Name) {
            $function = $ErrorRecord.InvocationInfo.MyCommand.Name
        }
    }
    $message = "未知错误"
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
脚本行号: $line
函数: $function
异常信息: $message
调用栈:
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
        $lblState.Text = "状态：需要处理"
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
        throw "Body API 未连接；可启动本地 Body 运行时，但找不到 ACBH Agent 程序：$AgentPath"
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
        throw "Body API 未连接；可启动本地 Body 运行时，但未能在 $BodyListen 监听。请检查日志或手动运行 acbh-agent-windows-amd64.exe body serve。"
    }
    Add-Log "Body API 已启动：$script:BodyUrl"
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
        Set-ControlText $txtConfigPath $health.configPath
        Set-ControlText $txtBodyApi $health.bodyApi
        if ($health.coordinatorUrl) { Set-ControlText $txtCoordinator $health.coordinatorUrl }
        if ($health.mode) { $cmbMode.SelectedItem = $health.mode }
        if ($health.configError) {
            $lblState.Text = "状态：配置需要处理"
            Add-Log ("配置错误：" + $health.configError.errorCode + " " + $health.configError.message)
        } else {
            $lblState.Text = "状态：Body API 就绪"
            Add-Log "Body API 健康检查通过。"
        }
    } catch {
        Show-Error ("健康检查失败：" + $_.Exception.Message)
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
        Set-ControlText $txtTokenStatus "未配置"
        if (($cfg.instance -and $cfg.instance.ownerToken -eq "[redacted]") -or ($cfg.compat -and $cfg.compat.legacyHostToken -eq "[redacted]")) {
            Set-ControlText $txtTokenStatus "已配置（已隐藏）"
        }
        if ($cfg.listener) {
            Set-ControlText $txtListenerHost $cfg.listener.localHost
            Set-ControlText $txtListenerPort $cfg.listener.localPort
        }
        if ($cfg.relay) {
            Set-ControlText $txtPublicHost $cfg.relay.publicHost
            Set-ControlText $txtPublicPort $cfg.relay.minecraftPort
        }
        Add-Log "config.json 已加载。"
    } catch {
        Add-Log ("配置暂未加载：" + $_.Exception.Message)
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
        Add-Log "config.json 已保存。"
        Refresh-Health
    } catch {
        Show-Error ("保存配置失败：" + $_.Exception.Message)
    }
}

function Test-Coordinator {
    try {
        $op = Invoke-BodyJson -Method "GET" -Path "/v1/coordinator/probe"
        $txtOperation.Text = ($op | ConvertTo-Json -Depth 12)
        $txtErrorDetails.Text = ""
        if ($op.state -eq "success") {
            $lblState.Text = "状态：VPS 连接正常"
            $txtActualRequestUrl.Text = $op.result.actualRequestUrl
            $txtProtocol.Text = [string]$op.result.capabilities.protocolVersion
            $txtCapabilities.Text = (($op.result.capabilities.capabilities) -join ", ")
            Refresh-Identity
        } else {
            $lblState.Text = "状态：VPS 连接失败"
            if ($op.error) {
                $txtActualRequestUrl.Text = $op.error.details.url
                $txtErrorDetails.Text = "errorCode=$($op.error.errorCode) httpStatus=$($op.error.details.httpStatus) responseBody=$($op.error.details.responseBody)"
            }
        }
        Add-Log ("连接测试操作：" + $op.operationId + " 状态=" + $op.state)
    } catch {
        Show-Error ("VPS 连接测试失败：" + $_.Exception.Message)
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
        Set-ControlText $txtTokenStatus "未配置"
        if ($id.compat -and ($id.compat.ownerTokenPresent -or $id.compat.legacyHostTokenPresent)) {
            Set-ControlText $txtTokenStatus "已配置（已隐藏）"
        }
        if ($id.compat) {
            Set-ControlText $txtAdvancedDiagnostics "usesLegacyGroupApi=$($id.compat.usesLegacyGroupApi); legacyGroupIdPresent=$($id.compat.legacyGroupIdPresent); legacyHostIdPresent=$($id.compat.legacyHostIdPresent)"
        }
        Add-Log "私人实例身份已刷新。"
    } catch {
        Add-Log ("身份信息暂未加载：" + $_.Exception.Message)
    }
}

function Run-Init {
    try {
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/init"
        $txtOperation.Text = ($op | ConvertTo-Json -Depth 12)
        if ($op.state -eq "success") {
            $lblState.Text = "状态：私人实例已就绪"
        } else {
            $lblState.Text = "状态：初始化失败"
        }
        Add-Log ("初始化操作：" + $op.operationId + " 状态=" + $op.state)
    } catch {
        Show-Error ("初始化失败：" + $_.Exception.Message)
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
        Add-Log "监听配置已保存。"
        Refresh-ListenerStatus
    } catch {
        Show-Error ("保存监听配置失败：" + $_.Exception.Message)
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
            Add-Log "ACBH 不会启动 Minecraft 服务端。请先用 MCSL 或你自己的脚本启动服务端，然后刷新监听状态。"
        } else {
            Add-Log "监听状态已刷新。"
        }
    } catch {
        Show-Error ("刷新监听状态失败：" + $_.Exception.Message)
    }
}

function Probe-Listener {
    try {
        $status = Invoke-BodyJson -Method "POST" -Path "/v1/listener/probe"
        Set-ListenerFields $status
        Add-Log "监听探测已完成。"
    } catch {
        Show-Error ("监听探测失败：" + $_.Exception.Message)
    }
}

function Set-RelayFields {
    param([object]$Relay)
    if ($null -eq $Relay) { return }
    Set-ControlText $txtPublicEndpoint $Relay.publicEndpoint
    Set-ControlText $txtLocalEndpoint $Relay.localEndpoint
    Set-ControlText $txtLocalServerListening $Relay.localServerListening
    Set-ControlText $txtTunnelConnected $Relay.tunnelConnected
    Set-ControlText $txtPublicListenerActive $Relay.publicListenerActive
    Set-ControlText $txtActiveConnections $Relay.activeConnections
    Set-ControlText $txtRelayState "已配置=$($Relay.configured) 本地服务端监听=$($Relay.localServerListening) 隧道已连接=$($Relay.tunnelConnected) VPS端口监听=$($Relay.publicListenerActive) 当前连接数=$($Relay.activeConnections)"
    Set-ControlText $txtRelayError ""
    if ($Relay.lastError) {
        Set-ControlText $txtRelayError $Relay.lastError
    }
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
            Add-Log "中转配置已通过 Body API 保存。"
        } elseif ($op.error) {
            $txtRelayError.Text = "errorCode=$($op.error.errorCode) httpStatus=$($op.error.details.httpStatus) responseBody=$($op.error.details.responseBody)"
        }
    } catch {
        Show-Error ("配置中转失败：" + $_.Exception.Message)
    }
}

function Start-RelayTunnel {
    try {
        $body = @{
            localMinecraftHost = $txtListenerHost.Text.Trim()
            localMinecraftPort = [int]$txtListenerPort.Text.Trim()
            publicMinecraftPort = [int]$txtPublicPort.Text.Trim()
        }
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/relay/start" -Body $body
        $txtOperation.Text = ($op | ConvertTo-Json -Depth 12)
        if ($op.state -eq "success") {
            Set-RelayFields $op.result.relay
            Add-Log "公网中转隧道已通过 Body API 启动。"
        } elseif ($op.error) {
            Set-ControlText $txtRelayError "errorCode=$($op.error.errorCode) httpStatus=$($op.error.details.httpStatus) responseBody=$($op.error.details.responseBody)"
        }
    } catch {
        Show-Error ("启动公网中转失败：" + $_.Exception.Message)
    }
}

function Stop-RelayTunnel {
    try {
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/relay/stop"
        $txtOperation.Text = ($op | ConvertTo-Json -Depth 12)
        if ($op.state -eq "success") {
            Set-RelayFields $op.result.relay
            Add-Log "公网中转隧道已通过 Body API 停止。"
        } elseif ($op.error) {
            Set-ControlText $txtRelayError "errorCode=$($op.error.errorCode) httpStatus=$($op.error.details.httpStatus) responseBody=$($op.error.details.responseBody)"
        }
    } catch {
        Show-Error ("停止公网中转失败：" + $_.Exception.Message)
    }
}

function Refresh-RelayStatus {
    try {
        $status = Invoke-BodyJson -Method "GET" -Path "/v1/relay/status"
        Set-RelayFields $status.relay
        Add-Log "中转状态已刷新。"
    } catch {
        Show-Error ("刷新中转状态失败：" + $_.Exception.Message)
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
        $txtBackupSummary.Text = "文件数=$($result.fileCount) 根目录=$($result.rootCount) 大小=$($result.logicalSize) 备份配置=$($result.profileId)"
        $txtBackupError.Text = ""
        Add-Log "备份分析已通过 Body API 完成。"
    } catch {
        Show-Error ("分析备份失败：" + $_.Exception.Message)
    }
}

function Upload-Backup {
    try {
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/backup/upload"
        $op = Wait-BodyOperation $op
        if ($op.state -eq "success") {
            $txtBackupSummary.Text = "快照=$($op.result.snapshotId) 已上传=$($op.result.uploadedSize) 去重节省=$($op.result.deduplicatedSize) 实际请求URL=$($op.result.actualRequestUrl)"
            Add-Log "备份已通过 Body API 上传。"
        }
    } catch {
        Show-Error ("上传备份失败：" + $_.Exception.Message)
    }
}

function Refresh-Snapshots {
    try {
        $result = Invoke-BodyJson -Method "GET" -Path "/v1/snapshots"
        $txtSnapshotList.Text = ($result.snapshots | ConvertTo-Json -Depth 8)
        if ($result.snapshots -and $result.snapshots.Count -gt 0) {
            $txtSnapshotId.Text = $result.snapshots[0].snapshotId
        }
        Add-Log "快照列表已通过 Body API 刷新。"
    } catch {
        Show-Error ("刷新快照列表失败：" + $_.Exception.Message)
    }
}

function Choose-RestoreDir {
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = "选择一个新的空目录作为恢复目标"
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
            $txtBackupSummary.Text = "已下载文件=$($op.result.downloadedFiles) 快照=$($op.result.snapshotId) 目标目录=$($op.result.targetDir)"
            Add-Log "最新快照已通过 Body API 下载。"
        }
    } catch {
        Show-Error ("下载最新快照失败：" + $_.Exception.Message)
    }
}

function Download-SelectedSnapshot {
    try {
        $snapshotId = $txtSnapshotId.Text.Trim()
        if (-not $snapshotId) { throw "请先填写快照 ID。" }
        $body = @{ targetDir = $txtRestoreTargetDir.Text.Trim(); allowNonEmpty = $chkAllowNonEmpty.Checked }
        $op = Invoke-BodyJson -Method "POST" -Path ("/v1/snapshots/" + [uri]::EscapeDataString($snapshotId) + "/download") -Body $body
        $op = Wait-BodyOperation $op
        if ($op.state -eq "success") {
            $txtBackupSummary.Text = "已下载文件=$($op.result.downloadedFiles) 快照=$($op.result.snapshotId) 目标目录=$($op.result.targetDir)"
            Add-Log "所选快照已通过 Body API 下载。"
        }
    } catch {
        Show-Error ("下载快照失败：" + $_.Exception.Message)
    }
}

function Choose-ServerDir {
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = "选择 Minecraft 服务端目录"
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
$form.Text = "ACBH v0.5 最小核心"
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
$title.Text = "ACBH v0.5 最小核心"
$title.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 18, [System.Drawing.FontStyle]::Bold)
$title.Location = New-Object System.Drawing.Point(20, 16)
$title.Size = New-Object System.Drawing.Size(420, 36)
$form.Controls.Add($title)

$lblState = New-Object System.Windows.Forms.Label
$lblState.Text = "状态：启动中"
$lblState.Location = New-Object System.Drawing.Point(620, 24)
$lblState.Size = New-Object System.Drawing.Size(420, 24)
$form.Controls.Add($lblState)

Add-Label "本地 Body API" 24 70 | Out-Null
$txtBodyApi = Add-TextBox 190 68 820 $true
Add-Label "配置文件路径" 24 104 | Out-Null
$txtConfigPath = Add-TextBox 190 102 820 $true
Add-Label "运行模式" 24 138 | Out-Null
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
Add-Button "选择目录" 840 302 { Choose-ServerDir } | Out-Null

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

Add-Button "刷新状态" 190 480 { Refresh-Health; Refresh-Identity } | Out-Null
Add-Button "加载配置" 350 480 { Load-Config; Refresh-Identity } | Out-Null
Add-Button "保存配置" 510 480 { Save-Config } | Out-Null
Add-Button "测试连接" 670 480 { Test-Coordinator } | Out-Null
Add-Button "初始化" 190 518 { Run-Init } | Out-Null

$grpUsage = New-Object System.Windows.Forms.GroupBox
$grpUsage.Text = "使用方式"
$grpUsage.Location = New-Object System.Drawing.Point(24, 552)
$grpUsage.Size = New-Object System.Drawing.Size(986, 112)
$form.Controls.Add($grpUsage)

$txtUsage = New-Object System.Windows.Forms.TextBox
$txtUsage.Location = New-Object System.Drawing.Point(16, 24)
$txtUsage.Size = New-Object System.Drawing.Size(952, 76)
$txtUsage.Multiline = $true
$txtUsage.ReadOnly = $true
$txtUsage.BorderStyle = [System.Windows.Forms.BorderStyle]::None
$txtUsage.BackColor = $form.BackColor
$txtUsage.Text = "1. 先启动本机 Minecraft 服务端，并确认它监听 127.0.0.1:25565。`r`n2. 点击【加载配置 / 保存配置】，填写 VPS 地址与服务端目录，再点击【测试连接】和【初始化】。`r`n3. 点击【刷新监听】，确认本地 MC 已监听；再点击【启动公网中转】，玩家连接 VPS 公网地址:25565。`r`n4. GUI 只调用本机 Body API，不直接访问 VPS，也不负责启动或停止 Minecraft 服务端。"
$grpUsage.Controls.Add($txtUsage)

$txtOperation = New-Object System.Windows.Forms.TextBox
$txtOperation.Location = New-Object System.Drawing.Point(24, 682)
$txtOperation.Size = New-Object System.Drawing.Size(986, 78)
$txtOperation.Multiline = $true
$txtOperation.ScrollBars = "Vertical"
$txtOperation.ReadOnly = $true
$txtOperation.Font = New-Object System.Drawing.Font("Consolas", 9)
$form.Controls.Add($txtOperation)

$grpListenerRelay = New-Object System.Windows.Forms.GroupBox
$grpListenerRelay.Text = "监听 / VPS 中转"
$grpListenerRelay.Location = New-Object System.Drawing.Point(24, 778)
$grpListenerRelay.Size = New-Object System.Drawing.Size(986, 304)
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

Add-GroupLabel "本地地址" 16 28 | Out-Null
$txtListenerHost = Add-GroupTextBox 130 26 220
$txtListenerHost.Text = "127.0.0.1"
Add-GroupLabel "本地端口" 380 28 | Out-Null
$txtListenerPort = Add-GroupTextBox 490 26 90
$txtListenerPort.Text = "25565"
Add-GroupLabel "公网地址" 610 28 | Out-Null
$txtPublicHost = Add-GroupTextBox 720 26 180
Add-GroupLabel "公网端口" 16 64 | Out-Null
$txtPublicPort = Add-GroupTextBox 130 62 90
$txtPublicPort.Text = "25565"

Add-GroupButton "保存监听" 240 62 { Save-ListenerConfig } | Out-Null
Add-GroupButton "刷新监听" 396 62 { Refresh-ListenerStatus } | Out-Null
Add-GroupButton "探测监听" 552 62 { Probe-Listener } | Out-Null
Add-GroupButton "配置中转" 708 62 { Configure-Relay } | Out-Null
Add-GroupButton "启动公网中转" 16 96 { Start-RelayTunnel } | Out-Null
Add-GroupButton "停止公网中转" 178 96 { Stop-RelayTunnel } | Out-Null
Add-GroupButton "刷新中转状态" 340 96 { Refresh-RelayStatus } | Out-Null

Add-GroupLabel "已监听" 16 138 | Out-Null
$txtListening = Add-GroupTextBox 130 136 90 $true
Add-GroupLabel "本地 MC" 248 138 | Out-Null
$txtLocalServerListening = Add-GroupTextBox 340 136 90 $true
Add-GroupLabel "隧道" 458 138 | Out-Null
$txtTunnelConnected = Add-GroupTextBox 540 136 90 $true
Add-GroupLabel "VPS 端口" 658 138 | Out-Null
$txtPublicListenerActive = Add-GroupTextBox 750 136 90 $true
Add-GroupLabel "连接数" 16 172 | Out-Null
$txtActiveConnections = Add-GroupTextBox 130 170 90 $true
Add-GroupLabel "进程 ID" 248 172 | Out-Null
$txtListenerPid = Add-GroupTextBox 300 170 100 $true
Add-GroupLabel "进程名" 430 172 | Out-Null
$txtListenerProcess = Add-GroupTextBox 520 170 250 $true
Add-GroupLabel "目录匹配" 760 172 | Out-Null
$txtServerDirMatched = Add-GroupTextBox 880 170 90 $true
Add-GroupLabel "本地端点" 16 206 | Out-Null
$txtLocalEndpoint = Add-GroupTextBox 130 204 270 $true
Add-GroupLabel "公网端点" 430 206 | Out-Null
$txtPublicEndpoint = Add-GroupTextBox 560 204 410 $true
Add-GroupLabel "命令行" 16 240 | Out-Null
$txtListenerCommand = Add-GroupTextBox 130 238 370 $true
Add-GroupLabel "警告" 520 240 | Out-Null
$txtListenerWarnings = Add-GroupTextBox 620 238 350 $true
$txtRelayState = Add-GroupTextBox 16 270 460 $true
$txtRelayError = Add-GroupTextBox 510 270 460 $true

$grpBackup = New-Object System.Windows.Forms.GroupBox
$grpBackup.Text = "备份 / 快照"
$grpBackup.Location = New-Object System.Drawing.Point(24, 1100)
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

Add-BackupButton "分析备份" 16 26 { Analyze-Backup } | Out-Null
Add-BackupButton "上传备份" 198 26 { Upload-Backup } | Out-Null
Add-BackupButton "列出快照" 380 26 { Refresh-Snapshots } | Out-Null
Add-BackupButton "下载最新" 562 26 { Download-LatestSnapshot } | Out-Null
Add-BackupButton "下载所选" 744 26 { Download-SelectedSnapshot } | Out-Null

Add-BackupLabel "快照 ID" 16 66 | Out-Null
$txtSnapshotId = Add-BackupTextBox 130 64 260
Add-BackupLabel "恢复目标" 420 66 | Out-Null
$txtRestoreTargetDir = Add-BackupTextBox 540 64 260
Add-BackupButton "选择目标" 810 61 { Choose-RestoreDir } | Out-Null
$chkAllowNonEmpty = New-Object System.Windows.Forms.CheckBox
$chkAllowNonEmpty.Text = "允许恢复到非空目录"
$chkAllowNonEmpty.Location = New-Object System.Drawing.Point(130, 96)
$chkAllowNonEmpty.Size = New-Object System.Drawing.Size(220, 24)
$grpBackup.Controls.Add($chkAllowNonEmpty)

Add-BackupLabel "摘要" 16 128 | Out-Null
$txtBackupSummary = Add-BackupTextBox 130 126 840 $true
Add-BackupLabel "快照列表" 16 160 | Out-Null
$txtSnapshotList = Add-BackupTextBox 130 158 360 $true
Add-BackupLabel "错误" 510 160 | Out-Null
$txtBackupError = Add-BackupTextBox 610 158 360 $true

$txtLog = New-Object System.Windows.Forms.TextBox
$txtLog.Location = New-Object System.Drawing.Point(24, 1368)
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
        if ($lblState.Text -eq "状态：启动中") {
            $lblState.Text = "状态：就绪"
        }
    } catch {
        Show-Error (Format-ExceptionDetails -ErrorRecord $_ -Context "启动失败")
    }
})

$form.Add_FormClosed({
    if ($script:BodyProcess -and -not $script:BodyProcess.HasExited) {
        try { $script:BodyProcess.Kill() } catch { }
    }
})

[void]$form.ShowDialog()
