param(
    [string]$AgentPath,
    [string]$AppDataDir,
    [string]$BodyListen = "127.0.0.1:6120",
    [switch]$SelfTest
)

$ErrorActionPreference = "Stop"

function Get-ControlTextSafe {
    param(
        $control,
        [string]$default = ""
    )
    if ($null -eq $control) { return $default }
    try {
        if ($null -eq $control.Text) { return $default }
        return [string]$control.Text
    } catch {
        return $default
    }
}

function Get-TrimmedTextSafe {
    param(
        $control,
        [string]$default = ""
    )
    return (Get-ControlTextSafe $control $default).Trim()
}

function Get-ComboValueSafe {
    param(
        $control,
        [string]$default = ""
    )
    if ($null -eq $control) { return $default }
    try {
        if ($null -ne $control.SelectedItem) {
            return [string]$control.SelectedItem
        }
        if ($null -ne $control.Text -and [string]$control.Text -ne "") {
            return [string]$control.Text
        }
        return $default
    } catch {
        return $default
    }
}

function Set-ControlTextSafe {
    param(
        $control,
        $value
    )
    if ($null -eq $control) { return }
    if ($null -eq $value) { $value = "" }
    $control.Text = [string]$value
    try { $control.SelectionStart = 0 } catch {}
}

function Get-StringSafe {
    param(
        $value,
        [string]$default = ""
    )
    if ($null -eq $value) { return $default }
    return ([string]$value).Trim()
}

function Get-ConfigFieldSafe {
    param(
        $object,
        [string]$property,
        [string]$default = ""
    )
    if ($null -eq $object) { return $default }
    try {
        $value = $object.$property
        if ($null -eq $value) { return $default }
        return [string]$value
    } catch {
        return $default
    }
}

function Get-IntFromTextSafe {
    param(
        [string]$text,
        [int]$default = 25565
    )
    if ([string]::IsNullOrWhiteSpace($text)) { return $default }
    $parsed = 0
    if ([int]::TryParse($text.Trim(), [ref]$parsed)) { return $parsed }
    return $default
}

function Test-GuiNullReferenceMessage {
    param([string]$Message)
    if ([string]::IsNullOrWhiteSpace($Message)) { return $false }
    return ($Message -match "不能对 Null 值表达式调用方法|You cannot call a method on a null-valued expression")
}

function Write-ActionDiagnosticLog {
    param(
        [string]$Action,
        [object]$ErrorRecord
    )
    $entry = New-Object System.Collections.Generic.List[string]
    $entry.Add("action=$Action")
    if ($ErrorRecord) {
        if ($ErrorRecord.Exception) {
            $entry.Add("exception=" + $ErrorRecord.Exception.Message)
        }
        if ($ErrorRecord.InvocationInfo) {
            $entry.Add("scriptLine=" + $ErrorRecord.InvocationInfo.ScriptLineNumber)
            $entry.Add("position=" + $ErrorRecord.InvocationInfo.PositionMessage)
        }
        if ($ErrorRecord.ScriptStackTrace) {
            $entry.Add("stackTrace=" + $ErrorRecord.ScriptStackTrace)
        }
    }
    $text = $entry -join [Environment]::NewLine
    if ($script:SelfTestMode) {
        [Console]::Out.WriteLine("[diagnostic] " + $text)
        return
    }
    if (Get-Command Add-Log -ErrorAction SilentlyContinue) {
        Add-Log ("[诊断] " + $text)
    }
}

function Format-GuiActionErrorMessage {
    param(
        [string]$Action,
        [object]$ErrorRecord
    )
    $message = ""
    if ($ErrorRecord -and $ErrorRecord.Exception) {
        $message = [string]$ErrorRecord.Exception.Message
    } elseif ($ErrorRecord) {
        $message = [string]$ErrorRecord
    }
    if (Test-GuiNullReferenceMessage $message) {
        return ($Action + "失败：GUI 内部字段为空。" + [Environment]::NewLine +
            "请重新打开 ACBH 后再试；详细错误已写入诊断日志。")
    }
    if ([string]::IsNullOrWhiteSpace($message)) {
        return ($Action + "失败：操作未完成，请查看诊断信息。")
    }
    return ($Action + "失败：" + $message)
}

function Normalize-GuiStatusValue {
    param(
        [string]$Kind,
        [object]$Value
    )
    $text = ""
    if ($null -ne $Value) { $text = ([string]$Value).Trim() }
    $key = $text.ToLowerInvariant()
    if ([string]::IsNullOrWhiteSpace($key)) {
        if ($Kind -eq "generic") { return "未知" }
        return "未配置"
    }
    switch ($Kind) {
        "token" {
            switch ($key) {
                { $_ -in @("missing", "empty", "not_configured", "not configured", "none", "false") } { return "未配置" }
                { $_ -in @("configured", "present", "true") } { return "已配置" }
                { $_ -in @("invalid", "auth_invalid", "failed") } { return "无效" }
                { $_ -in @("verified", "valid", "ok", "success") } { return "已验证" }
            }
        }
        "body" {
            switch ($key) {
                { $_ -in @("ok", "ready", "healthy", "connected") } { return "就绪" }
                { $_ -in @("failed", "error", "bad") } { return "失败" }
                { $_ -in @("unreachable", "offline", "timeout", "network_error", "network_timeout") } { return "不可达" }
            }
        }
        "coordinator" {
            switch ($key) {
                { $_ -in @("ok", "ready", "connected", "success") } { return "已连接" }
                { $_ -in @("failed", "error") } { return "失败" }
                { $_ -in @("server_error", "coordinator_server_error", "returned_error") } { return "返回错误" }
                { $_ -in @("unreachable", "offline", "timeout", "network_error", "network_timeout") } { return "不可达" }
                { $_ -in @("version_mismatch", "protocol_mismatch", "coordinator_protocol_mismatch") } { return "版本不匹配" }
                { $_ -in @("not_configured", "missing") } { return "未配置" }
            }
        }
        "relay" {
            switch ($key) {
                { $_ -in @("running", "active", "connected") } { return "运行中" }
                { $_ -in @("stopped", "inactive", "idle") } { return "未运行" }
                { $_ -in @("failed", "error") } { return "失败" }
                { $_ -in @("configured", "ready") } { return "已配置" }
                { $_ -in @("not_configured", "missing") } { return "未配置" }
            }
        }
        "bootstrap" {
            switch ($key) {
                { $_ -in @("ok", "success", "verified", "upserted") } { return "已注册" }
                { $_ -in @("failed", "error") } { return "失败" }
                { $_ -in @("not_configured", "missing", "skipped") } { return "未检测" }
                { $_ -in @("pending", "checking") } { return "检测中" }
            }
        }
        "bool" {
            switch ($key) {
                "true" { return "是" }
                "false" { return "否" }
            }
        }
    }
    return $text
}

function Convert-GuiCoordinatorInput {
    param(
        [string]$InputText,
        [string]$DefaultPort = "6121"
    )
    if ($null -eq $InputText) { $InputText = "" }
    $value = $InputText.Trim()
    if ([string]::IsNullOrWhiteSpace($value)) {
        return [pscustomobject]@{ Url = ""; Host = ""; Port = ""; Warning = "VPS Coordinator 未配置" }
    }
    $hadScheme = $value -match "^[A-Za-z][A-Za-z0-9+.-]*://"
    $explicitPort = $value -match "^[A-Za-z][A-Za-z0-9+.-]*://[^/]+:\d+(/|$)" -or ((-not $hadScheme) -and $value -match "^[^/]+:\d+(/|$)")
    if (-not $hadScheme) {
        $value = "http://" + $value
    }
    try {
        $uri = [System.Uri]$value
    } catch {
        throw "VPS 地址格式无效。请填写例如 http://你的VPS_IP:6121。"
    }
    if ([string]::IsNullOrWhiteSpace($uri.Host)) {
        throw "VPS 地址格式无效。请填写例如 http://你的VPS_IP:6121。"
    }
    $port = ""
    if ($explicitPort) {
        $port = [string]$uri.Port
    } elseif ($uri.Scheme -eq "http") {
        $port = $DefaultPort
    }
    $url = ""
    if ($port) {
        $builder = New-Object System.UriBuilder($uri.Scheme, $uri.Host, [int]$port)
        $url = $builder.Uri.AbsoluteUri.TrimEnd("/")
    } else {
        $url = ($uri.Scheme + "://" + $uri.Host).TrimEnd("/")
    }
    $warning = ""
    if ($port -eq "25565") {
        $warning = "你填写的是 25565，这通常是 Minecraft 进服端口，不是 VPS Coordinator API 端口。Coordinator 默认端口是 6121。"
    }
    return [pscustomobject]@{ Url = $url; Host = $uri.Host; Port = $port; Warning = $warning }
}

function Test-GuiTimestampLikeId {
    param([string]$Value)
    if ($null -eq $Value) { $Value = "" }
    $text = $Value.Trim()
    if ($text -match "^(inst_|dev_)?\d{8}T\d{6}Z$") { return $true }
    if ($text -match "^(inst_|dev_)?\d{5}T\d{6}Z$") { return $true }
    return $false
}

$script:SelfTestMode = [bool]$SelfTest

function Test-GuiStaticChecks {
    $source = Get-Content -Raw -Path $PSCommandPath
    $checks = @(
        "Get-ControlTextSafe",
        "Get-TrimmedTextSafe",
        "Get-ComboValueSafe",
        "Set-ControlTextSafe",
        "Format-GuiActionErrorMessage",
        "Write-ActionDiagnosticLog",
        "Test-GuiNullSafeBehavior",
        "Refresh-ListenerStatus",
        "Save-ListenerConfig",
        "Format-BodyError",
        "Configure-Relay",
        "Set-RelayConfigureDiagnostics",
        "Refresh-RelayStatus",
        "Convert-GuiCoordinatorInput",
        "Normalize-GuiStatusValue",
        "Toggle-AdvancedDiagnostics",
        "Run-LocalInit",
        "/v1/local/init",
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
        "txtLocalBodyStatus",
        "txtCoordinatorCheckStatus",
        "txtTokenValidationStatus",
        "txtRelayCheckStatus",
        "显示高级诊断",
        "一键初始化本机",
        "配置 VPS 中转",
        "charset=utf-8",
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
    $forbiddenButtonTexts = @(
        ("Refresh " + "health"),
        ("Load " + "config"),
        ("Save " + "config"),
        ("Test " + "connection"),
        ("Init" + "ialize"),
        ("Choose " + "dir"),
        ("Save " + "listener"),
        ("Refresh " + "listener"),
        ("Probe " + "listener"),
        ("Configure " + "relay"),
        ("Relay " + "status"),
        ("Init " + "failed"),
        ("Che" + "ck the URL" + ", status and responseBody" + " in details.")
    )
    foreach ($text in $forbiddenButtonTexts) {
        if ($source.Contains('"' + $text + '"')) {
            throw "GUI self-test failed: untranslated visible text found: $text"
        }
    }
    if ((Normalize-GuiStatusValue "token" "") -ne "未配置") { throw "GUI self-test failed: empty token status" }
    if ((Normalize-GuiStatusValue "token" "configured") -ne "已配置") { throw "GUI self-test failed: configured token status" }
    if ((Normalize-GuiStatusValue "token" "invalid") -ne "无效") { throw "GUI self-test failed: invalid token status" }
    if ((Normalize-GuiStatusValue "token" "verified") -ne "已验证") { throw "GUI self-test failed: verified token status" }
    if ((Normalize-GuiStatusValue "coordinator" "coordinator_protocol_mismatch") -ne "版本不匹配") { throw "GUI self-test failed: version mismatch status" }
    $norm = Convert-GuiCoordinatorInput "203.0.113.10"
    if ($norm.Url -ne "http://203.0.113.10:6121") { throw "GUI self-test failed: coordinator IP normalization" }
    $norm = Convert-GuiCoordinatorInput "http://203.0.113.10"
    if ($norm.Url -ne "http://203.0.113.10:6121") { throw "GUI self-test failed: coordinator URL default port" }
    $norm = Convert-GuiCoordinatorInput "http://203.0.113.10:6121/"
    if ($norm.Url -ne "http://203.0.113.10:6121") { throw "GUI self-test failed: coordinator URL trailing slash" }
    $norm = Convert-GuiCoordinatorInput "203.0.113.10:25565"
    if ($norm.Warning -notmatch "25565") { throw "GUI self-test failed: coordinator port warning" }
    if (-not (Test-GuiTimestampLikeId "20260706T025526Z")) { throw "GUI self-test failed: timestamp-like ID detection" }
    if (-not $source.Contains("MinimumSize = New-Object System.Drawing.Size(1100, 800)")) {
        throw "GUI self-test failed: minimum layout size is too small"
    }
    if (-not $source.Contains('$txtListenerHost = Add-GroupTextBox 150 54 260') -or -not $source.Contains('$txtListenerPort = Add-GroupTextBox 520 54 180') -or -not $source.Contains('$txtPublicHost = Add-GroupTextBox 150 96 260')) {
        throw "GUI self-test failed: listener/relay text boxes are too narrow"
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
    $norm = Convert-GuiCoordinatorInput $null
    if ($norm.Url -ne "" -or $norm.Warning -ne "VPS Coordinator 未配置") {
        throw "GUI self-test failed: null coordinator input"
    }
    $nullMsg = Format-GuiActionErrorMessage "保存配置" ([System.Management.Automation.ErrorRecord]::new(
        [System.NullReferenceException]::new("不能对 Null 值表达式调用方法。"), "保存配置", [System.Management.Automation.ErrorCategory]::InvalidOperation, $null))
    if ($nullMsg -match "不能对 Null 值表达式调用方法|You cannot call a method on a null-valued expression") {
        throw "GUI self-test failed: null reference message leaked to user"
    }
    if ($nullMsg -notmatch "GUI 内部字段为空") {
        throw "GUI self-test failed: friendly null reference message missing"
    }
    $relayDetails = [pscustomobject]@{
        error = [pscustomobject]@{
            errorCode = "coordinator_route_missing"
            details = [pscustomobject]@{
                url = "http://203.0.113.10:6121/v1/groups/grp_test/lease/ensure-active"
                httpStatus = 404
                responseBody = '{"message":"Route POST:/v1/groups/grp_test/lease/ensure-active not found"}'
                traceId = "tr_gui_selftest"
            }
        }
    }
    $relayDiag = Set-RelayConfigureDiagnostics $relayDetails
    if ($relayDiag -notmatch "实际请求 URL" -or $relayDiag -notmatch "HTTP 状态码" -or $relayDiag -notmatch "服务器返回内容") {
        throw "GUI self-test failed: relay configure diagnostics missing URL/status/body"
    }
}

function New-MockTextControl {
    param([string]$Text = "")
    return [pscustomobject]@{ Text = $Text }
}

function New-MockComboControl {
    param(
        [object]$SelectedItem = $null,
        [string]$Text = ""
    )
    return [pscustomobject]@{ SelectedItem = $SelectedItem; Text = $Text }
}

function Install-MockGuiControls {
    param([hashtable]$Overrides = @{})

    function Resolve-MockControl {
        param([string]$Name, $Default)
        if ($Overrides.ContainsKey($Name)) { return $Overrides[$Name] }
        return $Default
    }

    $script:txtCoordinator = Resolve-MockControl "txtCoordinator" (New-MockTextControl "")
    $script:txtAccessToken = Resolve-MockControl "txtAccessToken" (New-MockTextControl "")
    $script:txtInstanceName = Resolve-MockControl "txtInstanceName" (New-MockTextControl "")
    $script:txtDeviceName = Resolve-MockControl "txtDeviceName" (New-MockTextControl "")
    $script:txtServerName = Resolve-MockControl "txtServerName" (New-MockTextControl "")
    $script:txtServerDir = Resolve-MockControl "txtServerDir" (New-MockTextControl "")
    $script:txtListenerHost = Resolve-MockControl "txtListenerHost" (New-MockTextControl "")
    $script:txtListenerPort = Resolve-MockControl "txtListenerPort" (New-MockTextControl "")
    $script:txtPublicHost = Resolve-MockControl "txtPublicHost" (New-MockTextControl "")
    $script:txtPublicPort = Resolve-MockControl "txtPublicPort" (New-MockTextControl "")
    $script:cmbMode = Resolve-MockControl "cmbMode" (New-MockComboControl "remote-public" "")
    $script:txtInstanceId = Resolve-MockControl "txtInstanceId" $null
    $script:txtDeviceId = Resolve-MockControl "txtDeviceId" $null
    $script:txtServerId = Resolve-MockControl "txtServerId" $null
}

function Test-GuiNullSafeBehavior {
    function Assert-NullSafeTest {
        param([string]$Name, [scriptblock]$Block)
        try {
            & $Block
            Write-Output ("PASS: " + $Name)
        } catch {
            throw ("GUI self-test failed: " + $Name + " - " + $_.Exception.Message)
        }
    }

    Assert-NullSafeTest "null advanced controls save config" {
        Install-MockGuiControls @{
            txtInstanceId = $null
            txtDeviceId = $null
            txtServerId = $null
            cmbMode = (New-MockComboControl $null "")
        }
        $cfg = Build-ConfigFromForm
        if ($null -eq $cfg) { throw "config is null" }
        if ($cfg.mode -ne "remote-public") { throw "unexpected mode: $($cfg.mode)" }
    }

    Assert-NullSafeTest "empty access token save config" {
        Install-MockGuiControls @{ txtAccessToken = (New-MockTextControl "") }
        $cfg = Build-ConfigFromForm
        if ($cfg.instance.ownerToken -ne "[redacted]") { throw "token not redacted" }
        if ((Normalize-GuiStatusValue "token" "") -ne "未配置") { throw "token status not 未配置" }
    }

    Assert-NullSafeTest "empty VPS save config" {
        Install-MockGuiControls @{ txtCoordinator = (New-MockTextControl "") }
        $cfg = Build-ConfigFromForm
        if ($cfg.coordinatorUrl -ne "") { throw "coordinatorUrl should be empty" }
    }

    Assert-NullSafeTest "empty server dir save config" {
        Install-MockGuiControls @{ txtServerDir = (New-MockTextControl "") }
        $cfg = Build-ConfigFromForm
        if ($cfg.server.dir -ne "") { throw "server dir should be empty" }
    }

    Assert-NullSafeTest "null combo selected item save config" {
        Install-MockGuiControls @{ cmbMode = (New-MockComboControl $null "") }
        $cfg = Build-ConfigFromForm
        if ($cfg.mode -ne "remote-public") { throw "mode default missing" }
    }

    Assert-NullSafeTest "empty status values render safely" {
        if ((Normalize-GuiStatusValue "generic" $null) -ne "未知") { throw "generic null status" }
        if ((Normalize-GuiStatusValue "token" $null) -ne "未配置") { throw "token null status" }
        if ((Normalize-GuiStatusValue "body" "") -ne "未配置") { throw "body empty status" }
        if ((Normalize-GuiStatusValue "coordinator" "") -ne "未配置") { throw "coordinator empty status" }
    }

    Assert-NullSafeTest "save defaults applied" {
        Install-MockGuiControls @{}
        $cfg = Build-ConfigFromForm
        if ($cfg.instance.displayName -ne "私人 ACBH 实例") { throw "instance default missing" }
        if ($cfg.device.displayName -ne $env:COMPUTERNAME) { throw "device default missing" }
        if ($cfg.server.displayName -ne "Minecraft 服务端") { throw "server default missing" }
        if ($cfg.listener.localHost -ne "127.0.0.1") { throw "listener host default missing" }
        if ($cfg.listener.localPort -ne 25565) { throw "listener port default missing" }
    }

    $testAppData = Join-Path $env:TEMP ("acbh-gui-selftest-" + [guid]::NewGuid().ToString("N"))
    $savedAppData = $AppDataDir
    $savedAgentPath = $AgentPath
    try {
        New-Item -ItemType Directory -Force -Path $testAppData | Out-Null
        $AppDataDir = $testAppData
        $AgentPath = Join-Path $PSScriptRoot "..\acbh-agent-windows-amd64.exe"
        if (-not (Test-Path $AgentPath)) {
            $built = Get-ChildItem -Path (Join-Path $PSScriptRoot "..\agent") -Filter "acbh-agent*.exe" -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
            if ($built) { $AgentPath = $built.FullName }
        }
        if (Test-Path $AgentPath) {
            if ($script:BodyProcess -and -not $script:BodyProcess.HasExited) {
                try { $script:BodyProcess.Kill() } catch {}
            }
            $script:BodyProcess = $null
            try {
                Get-NetTCPConnection -LocalPort 6120 -State Listen -ErrorAction SilentlyContinue |
                    Select-Object -ExpandProperty OwningProcess -Unique |
                    ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }
            } catch {}
            Start-Sleep -Milliseconds 400
            Install-MockGuiControls @{
                txtCoordinator = (New-MockTextControl "")
                txtAccessToken = (New-MockTextControl "")
                txtServerDir = (New-MockTextControl "")
            }
            [void](Sync-FormConfig -Quiet)
            $op = Invoke-BodyJson -Method "POST" -Path "/v1/local/init"
            if ($op.state -ne "success") { throw "local init failed: $($op | ConvertTo-Json -Compress)" }
            $configPath = Join-Path $testAppData "config.json"
            if (-not (Test-Path $configPath)) { throw "config.json not created" }
            $configRaw = Get-Content -Raw -Path $configPath -Encoding UTF8
            if ($configRaw -notmatch "acbh_instance_") { throw "instance id missing" }
            if ($configRaw -notmatch "acbh_device_") { throw "device id missing" }
            if ($script:BodyProcess -and -not $script:BodyProcess.HasExited) {
                try { $script:BodyProcess.Kill() } catch {}
            }
            $script:BodyProcess = $null
            Write-Output "PASS: local init creates config with empty VPS/token/server dir"
        } else {
            Write-Output "SKIP: local init integration (agent binary missing)"
        }
    } finally {
        $AppDataDir = $savedAppData
        $AgentPath = $savedAgentPath
        if ($script:BodyProcess -and -not $script:BodyProcess.HasExited) {
            try { $script:BodyProcess.Kill() } catch {}
        }
        $script:BodyProcess = $null
        if (Test-Path $testAppData) {
            Remove-Item -LiteralPath $testAppData -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
}

Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()
[System.Windows.Forms.Application]::SetCompatibleTextRenderingDefault($false)

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
    $jsonTokenPattern = '(?i)("(?:hostToken|ownerToken|legacyHostToken|accessKey|accessToken|authToken|relayToken|coordinatorToken|token|secret|privateKey)"\s*:\s*")[^"]+(")'
    $safe = [regex]::Replace($safe, $jsonTokenPattern, '$1[hidden]$2')
    $tokenPattern = "(?i)(hostToken|ownerToken|legacyHostToken|accessKey|accessToken|authToken|relayToken|coordinatorToken|token|secret|privateKey)\s*[:=]\s*\S+"
    $safe = [regex]::Replace($safe, $tokenPattern, "[hidden]")
    $safe = [regex]::Replace($safe, "ht_[A-Za-z0-9_\-]+", "ht_[hidden]")
    return $safe
}

function Format-ErrorDetails {
    param([object]$Details)
    if ($null -eq $Details) { return "" }
    $lines = New-Object System.Collections.Generic.List[string]
    if ($Details.url) { $lines.Add("实际请求 URL：" + (Redact-Secrets ([string]$Details.url))) }
    if ($Details.httpStatus) { $lines.Add("HTTP 状态码：" + [string]$Details.httpStatus) }
    if ($Details.responseBody) { $lines.Add("服务器返回内容：" + (Redact-Secrets ([string]$Details.responseBody))) }
    if ($Details.traceId) { $lines.Add("traceId：" + [string]$Details.traceId) }
    if ($Details.configPath) { $lines.Add("配置文件：" + [string]$Details.configPath) }
    return ($lines -join [Environment]::NewLine)
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
    $detailText = Format-ErrorDetails $err.details

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
            if ($message -match "coordinatorUrl") {
                $text = "缺少 VPS 协调器地址 coordinatorUrl。" +
                    [Environment]::NewLine + "请在「VPS 地址」中填写，例如：" +
                    [Environment]::NewLine + "http://<VPS公网IP>:6121" +
                    [Environment]::NewLine + "然后点击「保存配置」或「初始化」。"
            } else {
                $text = "配置还不完整或格式不正确"
                if ($message) { $text += "：" + $message }
                if ($suggestion) { $text += [Environment]::NewLine + $suggestion }
            }
            if ($configPath) { $text += [Environment]::NewLine + "配置文件：" + $configPath }
            return $text
        }
        "identity_incomplete" {
            $text = "访问令牌或私有实例身份尚未配置完整。" +
                [Environment]::NewLine + "请点击「生成/注册身份」，或导入包含访问令牌的旧配置。"
            if ($configPath) { $text += [Environment]::NewLine + "配置文件：" + $configPath }
            return $text
        }
        "coordinator_server_error" {
            $text = "VPS Coordinator 返回错误。" +
                [Environment]::NewLine + [Environment]::NewLine +
                "可能原因：" + [Environment]::NewLine +
                "- VPS 地址填写错误" + [Environment]::NewLine +
                "- Coordinator 服务未启动" + [Environment]::NewLine +
                "- VPS 防火墙或安全组未放行 6121" + [Environment]::NewLine +
                "- 访问令牌无效" + [Environment]::NewLine +
                "- Windows 客户端与 VPS Coordinator 版本不匹配" + [Environment]::NewLine + [Environment]::NewLine +
                "请检查：" + [Environment]::NewLine +
                "- 实际请求 URL" + [Environment]::NewLine +
                "- HTTP 状态码" + [Environment]::NewLine +
                "- 服务器返回内容" + [Environment]::NewLine +
                "- VPS Coordinator 日志"
            if ($detailText) { $text += [Environment]::NewLine + [Environment]::NewLine + $detailText }
            return $text
        }
        "coordinator_protocol_mismatch" {
            $text = "VPS Coordinator 版本不匹配。" +
                [Environment]::NewLine + "Windows 客户端与 VPS Coordinator 版本不一致。请使用同一 release 中的 Windows zip 和 coordinator bundle。"
            if ($detailText) { $text += [Environment]::NewLine + [Environment]::NewLine + $detailText }
            return $text
        }
        "coordinator_unreachable" {
            $text = "VPS Coordinator 不可达。请检查 VPS 地址、6121 端口、防火墙或安全组。"
            if ($detailText) { $text += [Environment]::NewLine + [Environment]::NewLine + $detailText }
            return $text
        }
        "network_error" {
            $text = "网络请求失败。请检查 VPS 地址、网络连接和 Coordinator 服务状态。"
            if ($detailText) { $text += [Environment]::NewLine + [Environment]::NewLine + $detailText }
            return $text
        }
        "network_timeout" {
            $text = "连接 VPS Coordinator 超时。请检查 6121 端口是否放行。"
            if ($detailText) { $text += [Environment]::NewLine + [Environment]::NewLine + $detailText }
            return $text
        }
        "auth_missing" {
            $text = "访问令牌未配置。请填写访问令牌后再测试连接。"
            if ($detailText) { $text += [Environment]::NewLine + [Environment]::NewLine + $detailText }
            return $text
        }
        "auth_invalid" {
            $text = "访问令牌无效。请确认令牌与 VPS Coordinator 中的实例一致。"
            if ($detailText) { $text += [Environment]::NewLine + [Environment]::NewLine + $detailText }
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
    $safe = ("[" + (Get-Date -Format "HH:mm:ss") + "] " + (Redact-Secrets $Text))
    if ($null -eq $txtLog) {
        if ($script:SelfTestMode) { [Console]::Out.WriteLine("[log] " + $safe) }
        return
    }
    $txtLog.AppendText($safe + [Environment]::NewLine)
    $txtLog.SelectionStart = $txtLog.TextLength
    $txtLog.ScrollToCaret()
}

function Set-ControlText {
    param(
        [object]$Control,
        [object]$Value,
        [string]$EmptyText = ""
    )
    if ($null -eq $Control) { return }
    $text = ""
    if ($null -ne $Value) { $text = [string]$Value }
    if ([string]::IsNullOrWhiteSpace($text)) { $text = $EmptyText }
    Set-ControlTextSafe $Control $text
    if ($Control -is [System.Windows.Forms.TextBox]) {
        try {
            $Control.SelectionLength = 0
        } catch {}
    }
}

function Set-Status {
    param(
        [object]$Control,
        [object]$Value,
        [string]$Kind = "generic"
    )
    Set-ControlText $Control (Normalize-GuiStatusValue $Kind $Value) "未知"
}

function Set-Diagnostics {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    Set-ControlText $txtErrorDetails (Redact-Secrets $Text)
}

function Show-Error {
    param([string]$Message)
    $safe = Redact-Secrets $Message
    Add-Log $safe
    Set-Diagnostics $safe
    $brief = ($safe -split "(\r?\n)") | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -First 1
    if ([string]::IsNullOrWhiteSpace($brief)) { $brief = "操作失败，请查看诊断信息。" }
    [System.Windows.Forms.MessageBox]::Show($brief, "ACBH", "OK", "Error") | Out-Null
}

function Show-ActionError {
    param(
        [string]$Action,
        [object]$ErrorRecord
    )
    Write-ActionDiagnosticLog $Action $ErrorRecord
    Show-Error (Format-GuiActionErrorMessage $Action $ErrorRecord)
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
        throw "找不到 ACBH Agent：$AgentPath"
    }
    New-Item -ItemType Directory -Force -Path $AppDataDir | Out-Null
    $bodyArgs = @("body", "serve", "--listen", $BodyListen, "--app-data-dir", $AppDataDir)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $AgentPath
    $argList = $null
    try { $argList = $psi.ArgumentList } catch {}
    if ($null -ne $argList) {
        foreach ($arg in $bodyArgs) {
            [void]$argList.Add($arg)
        }
    } else {
        $psi.Arguments = ($bodyArgs | ForEach-Object {
            if ($_ -match '\s') { '"' + $_ + '"' } else { $_ }
        }) -join ' '
    }
    $psi.WorkingDirectory = Split-Path -Parent $AgentPath
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $script:BodyProcess = [System.Diagnostics.Process]::Start($psi)
    Start-Sleep -Milliseconds 600
    if (-not (Test-BodyPort)) {
        throw "本地 Body API 未能在 $BodyListen 启动"
    }
    Add-Log "本地 Body API 已启动：$script:BodyUrl"
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
            $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
            return Invoke-RestMethod -Method $Method -Uri $uri -Body $bytes -ContentType "application/json; charset=utf-8"
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
        Set-Status $txtLocalBodyStatus "ok" "body"
        if ($health.coordinatorUrl) { Set-ControlText $txtCoordinator $health.coordinatorUrl }
        if ($health.mode -and $cmbMode) { $cmbMode.SelectedItem = $health.mode }
        if ($health.configError) {
            $lblState.Text = "状态：本地 Body API 就绪，配置需处理"
            Set-Diagnostics ("配置提示：" + $health.configError.errorCode + " " + $health.configError.message)
            Add-Log ("配置提示：" + $health.configError.errorCode + " " + $health.configError.message)
        } else {
            $lblState.Text = "状态：本地 Body API 就绪"
            Add-Log "本地 Body API 就绪。"
        }
    } catch {
        Set-Status $txtLocalBodyStatus "failed" "body"
        Show-ActionError "刷新状态" $_
    }
}

function Load-Config {
    try {
        $cfg = Invoke-BodyJson -Method "GET" -Path "/v1/config"
        if ($cmbMode) {
            $mode = Get-ConfigFieldSafe $cfg "mode" "remote-public"
            if ($mode) { $cmbMode.SelectedItem = $mode }
        }
        Set-ControlText $txtCoordinator (Get-ConfigFieldSafe $cfg "coordinatorUrl")
        Set-ControlText $txtInstanceName (Get-ConfigFieldSafe $cfg.instance "displayName")
        Set-ControlText $txtInstanceId (Get-ConfigFieldSafe $cfg.instance "instanceId")
        Set-ControlText $txtDeviceName (Get-ConfigFieldSafe $cfg.device "displayName")
        Set-ControlText $txtDeviceId (Get-ConfigFieldSafe $cfg.device "deviceId")
        Set-ControlText $txtServerName (Get-ConfigFieldSafe $cfg.server "displayName")
        Set-ControlText $txtServerDir (Get-ConfigFieldSafe $cfg.server "dir")
        Set-Status $txtTokenStatus "not_configured" "token"
        $ownerToken = Get-ConfigFieldSafe $cfg.instance "ownerToken"
        $legacyHostToken = Get-ConfigFieldSafe $cfg.compat "legacyHostToken"
        if ($ownerToken -eq "[redacted]" -or $legacyHostToken -eq "[redacted]") {
            Set-Status $txtTokenStatus "configured" "token"
        }
        if ($cfg.listener) {
            Set-ControlText $txtListenerHost (Get-ConfigFieldSafe $cfg.listener "localHost" "127.0.0.1")
            Set-ControlText $txtListenerPort (Get-ConfigFieldSafe $cfg.listener "localPort" "25565")
        }
        if ($cfg.relay) {
            Set-ControlText $txtPublicHost (Get-ConfigFieldSafe $cfg.relay "publicHost")
            Set-ControlText $txtPublicPort (Get-ConfigFieldSafe $cfg.relay "minecraftPort" "25565")
        }
        Add-Log "配置已加载。"
    } catch {
        Add-Log ("配置尚未加载：" + $_.Exception.Message)
    }
}

function Build-ConfigFromForm {
    $coordInput = Get-TrimmedTextSafe $txtCoordinator
    $coord = Convert-GuiCoordinatorInput $coordInput
    Set-ControlTextSafe $txtCoordinator $coord.Url
    if ($coord.Warning) {
        if (Get-Command Set-Diagnostics -ErrorAction SilentlyContinue) { Set-Diagnostics $coord.Warning }
        if (Get-Command Add-Log -ErrorAction SilentlyContinue) { Add-Log $coord.Warning }
    }
    $ownerToken = Get-TrimmedTextSafe $txtAccessToken
    if ([string]::IsNullOrWhiteSpace($ownerToken)) { $ownerToken = "[redacted]" }
    $listenerPort = Get-IntFromTextSafe (Get-TrimmedTextSafe $txtListenerPort) 25565
    $publicPort = Get-IntFromTextSafe (Get-TrimmedTextSafe $txtPublicPort) $listenerPort
    $instanceName = Get-TrimmedTextSafe $txtInstanceName
    if ([string]::IsNullOrWhiteSpace($instanceName)) { $instanceName = "私人 ACBH 实例" }
    $deviceName = Get-TrimmedTextSafe $txtDeviceName
    if ([string]::IsNullOrWhiteSpace($deviceName)) { $deviceName = $env:COMPUTERNAME }
    $serverName = Get-TrimmedTextSafe $txtServerName
    if ([string]::IsNullOrWhiteSpace($serverName)) { $serverName = "Minecraft 服务端" }
    $listenerHost = Get-TrimmedTextSafe $txtListenerHost
    if ([string]::IsNullOrWhiteSpace($listenerHost)) { $listenerHost = "127.0.0.1" }
    return @{
        schemaVersion = 2
        mode = Get-ComboValueSafe $cmbMode "remote-public"
        coordinatorUrl = $coord.Url
        instance = @{
            instanceId = Get-TrimmedTextSafe $txtInstanceId
            displayName = $instanceName
            ownerToken = $ownerToken
        }
        device = @{
            deviceId = Get-TrimmedTextSafe $txtDeviceId
            displayName = $deviceName
            platform = "windows"
        }
        server = @{
            serverId = Get-TrimmedTextSafe $txtServerId
            displayName = $serverName
            dir = Get-TrimmedTextSafe $txtServerDir
        }
        compat = @{
            coordinatorProtocol = 2
            legacyGroupId = ""
            legacyHostId = ""
            legacyHostToken = $ownerToken
        }
        listener = @{
            enabled = $true
            localHost = $listenerHost
            localPort = $listenerPort
            expectedProcessNames = @("java.exe", "javaw.exe")
            serverDirMatchRequired = $false
        }
        relay = @{
            enabled = $true
            publicHost = Get-TrimmedTextSafe $txtPublicHost
            coordinatorPort = 6121
            minecraftPort = $publicPort
        }
        backup = @{
            profileId = "minecraft-migratable"
            include = @("dir:world","dir:mods","dir:config","dir:defaultconfigs","dir:datapacks","dir:resourcepacks","dir:global_packs","dir:patchouli_books","file:server.properties","file:eula.txt","file:ops.json","file:whitelist.json","file:banned-ips.json","file:banned-players.json","file:server-icon.png","file:manifest.json","file:variables.txt","file:user_jvm_args.txt","file:start.bat","file:start.ps1","file:start.sh","file:run.sh","file:双击直接开服！！！.bat","file:HOW-TO-RUN.md")
            exclude = @("dir:libraries","dir:jre","dir:logs","dir:crash-reports","dir:versions","dir:.cache","dir:cache")
        }
    }
}

function Sync-FormConfig {
    param([switch]$Quiet)
    try {
        $cfg = Build-ConfigFromForm
        Invoke-BodyJson -Method "PUT" -Path "/v1/config" -Body $cfg | Out-Null
        if (-not $Quiet) { Add-Log "config.json saved." }
        Refresh-Health
        Refresh-Identity
        return $true
    } catch {
        Write-ActionDiagnosticLog "保存配置" $_
        if (-not $Quiet) { Show-ActionError "保存配置" $_ }
        return $false
    }
}

function Save-Config {
    [void](Sync-FormConfig)
}

function Test-Coordinator {
    try {
        Set-ControlText $txtLocalBodyStatus "检测中"
        Set-ControlText $txtCoordinatorCheckStatus "检测中"
        Set-ControlText $txtTokenValidationStatus "检测中"
        Set-ControlText $txtBootstrapStatus "检测中"
        Set-ControlText $txtRelayCheckStatus "检测中"
        if (-not (Sync-FormConfig -Quiet)) { throw "保存当前表单配置失败，无法测试连接。" }
        Set-Status $txtLocalBodyStatus "ok" "body"
        $op = Invoke-BodyJson -Method "GET" -Path "/v1/coordinator/probe"
        Set-ControlText $txtOperation ($op | ConvertTo-Json -Depth 12)
        Set-ControlText $txtErrorDetails ""
        if ($op.state -eq "success") {
            $lblState.Text = "状态：VPS Coordinator 可连接"
            Set-Status $txtCoordinatorCheckStatus "ok" "coordinator"
            Set-ControlText $txtActualRequestUrl $op.result.actualRequestUrl
            Set-ControlText $txtProtocol ([string]$op.result.capabilities.protocolVersion)
            Set-ControlText $txtCapabilities (($op.result.capabilities.capabilities) -join ", ")
            if ($op.result.capabilities.coordinatorVersion) {
                Set-ControlText $txtCoordinatorVersion $op.result.capabilities.coordinatorVersion
            }
            if ($op.result.capabilities.buildCommit) {
                Set-ControlText $txtCoordinatorCommit $op.result.capabilities.buildCommit
            }
            Refresh-Identity
            Test-TokenValidation
            Test-BootstrapStatus
            Test-RelayStatus
        } else {
            $lblState.Text = "状态：VPS Coordinator 连接失败"
            Set-Status $txtCoordinatorCheckStatus "failed" "coordinator"
            Set-ControlText $txtTokenValidationStatus (Get-TrimmedTextSafe $txtTokenStatus "未配置")
            Set-Status $txtBootstrapStatus "not_configured" "bootstrap"
            Set-ControlText $txtRelayCheckStatus "未检测"
            if ($op.error) {
                Set-ControlText $txtActualRequestUrl $op.error.details.url
                Set-Diagnostics ("errorCode=$($op.error.errorCode)" + [Environment]::NewLine + (Format-ErrorDetails $op.error.details))
                if ($op.error.errorCode -eq "coordinator_protocol_mismatch") {
                    Set-Status $txtCoordinatorCheckStatus "version_mismatch" "coordinator"
                } elseif ($op.error.errorCode -eq "coordinator_capability_missing") {
                    Set-Status $txtCoordinatorCheckStatus "version_mismatch" "coordinator"
                    Set-Diagnostics "VPS Coordinator 版本不支持 token-only relay。请升级 Coordinator 后再试。"
                } elseif ($op.error.errorCode -eq "coordinator_unreachable") {
                    Set-Status $txtCoordinatorCheckStatus "unreachable" "coordinator"
                } elseif ($op.error.errorCode -eq "coordinator_server_error") {
                    Set-Status $txtCoordinatorCheckStatus "server_error" "coordinator"
                }
            }
        }
        Add-Log ("连接测试：" + $op.operationId + " state=" + $op.state)
    } catch {
        $msg = $_.Exception.Message
        if ($msg -match "protocol|版本不匹配") {
            Set-Status $txtCoordinatorCheckStatus "version_mismatch" "coordinator"
        } elseif ($msg -match "不可达|timeout|超时|network") {
            Set-Status $txtCoordinatorCheckStatus "unreachable" "coordinator"
        } elseif ($msg -match "server error|返回错误") {
            Set-Status $txtCoordinatorCheckStatus "server_error" "coordinator"
        } else {
            Set-Status $txtCoordinatorCheckStatus "failed" "coordinator"
        }
        Show-ActionError "测试连接" $_
    }
}

function Test-TokenValidation {
    if ((Get-TrimmedTextSafe $txtTokenStatus "未配置") -ne "已配置") {
        Set-Status $txtTokenStatus "not_configured" "token"
        Set-Status $txtTokenValidationStatus "not_configured" "token"
        Set-Status $txtBootstrapStatus "not_configured" "bootstrap"
        return
    }
    try {
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/init"
        if ($op.state -eq "success") {
            Set-Status $txtTokenStatus "verified" "token"
            Set-Status $txtTokenValidationStatus "verified" "token"
            Set-Status $txtBootstrapStatus "ok" "bootstrap"
        } elseif ($op.error -and ($op.error.errorCode -eq "auth_invalid" -or $op.error.errorCode -eq "auth_missing")) {
            Set-Status $txtTokenStatus "invalid" "token"
            Set-Status $txtTokenValidationStatus "invalid" "token"
            Set-Status $txtBootstrapStatus "failed" "bootstrap"
            if ($op.error.message) { Set-Diagnostics ("访问令牌无效：" + (Redact-Secrets $op.error.message)) }
        } elseif ($op.error -and $op.error.errorCode -eq "coordinator_capability_missing") {
            Set-Status $txtBootstrapStatus "failed" "bootstrap"
            Set-Diagnostics "VPS Coordinator 版本不支持 token-only relay。"
        } else {
            Set-ControlText $txtTokenValidationStatus (Get-TrimmedTextSafe $txtTokenStatus "未配置")
            Set-Status $txtBootstrapStatus "not_configured" "bootstrap"
        }
    } catch {
        if ($_.Exception.Message -match "401|auth|token|令牌|访问令牌无效") {
            Set-Status $txtTokenStatus "invalid" "token"
            Set-Status $txtTokenValidationStatus "invalid" "token"
            Set-Status $txtBootstrapStatus "failed" "bootstrap"
        } else {
            Set-ControlText $txtTokenValidationStatus (Get-TrimmedTextSafe $txtTokenStatus "未配置")
            Set-Status $txtBootstrapStatus "not_configured" "bootstrap"
            Add-Log ("访问令牌验证跳过：" + $_.Exception.Message)
        }
    }
}

function Test-BootstrapStatus {
    if ((Get-TrimmedTextSafe $txtBootstrapStatus "未检测") -eq "已注册") { return }
    if ((Get-TrimmedTextSafe $txtTokenValidationStatus "未配置") -eq "已验证") {
        Set-Status $txtBootstrapStatus "ok" "bootstrap"
    }
}

function Test-RelayStatus {
    try {
        $status = Invoke-BodyJson -Method "GET" -Path "/v1/relay/status"
        Set-RelayFields $status.relay
        if ($status.relay.active) {
            Set-Status $txtRelayProbeStatus "running" "relay"
        } elseif ($status.relay.configured) {
            Set-Status $txtRelayProbeStatus "failed" "relay"
        } else {
            Set-Status $txtRelayProbeStatus "not_configured" "relay"
        }
    } catch {
        Set-Status $txtRelayCheckStatus "not_configured" "relay"
        Set-Status $txtRelayProbeStatus "not_configured" "relay"
        Add-Log ("中转状态尚未就绪：" + $_.Exception.Message)
    }
}

function Refresh-Identity {
    try {
        $id = Invoke-BodyJson -Method "GET" -Path "/v1/identity"
        Set-ControlText $txtInstanceId $id.instance.instanceId
        Set-ControlText $txtInstanceName $id.instance.displayName
        Set-ControlText $txtDeviceId $id.device.deviceId
        Set-ControlText $txtDeviceName $id.device.displayName
        Set-ControlText $txtServerId $id.server.serverId
        Set-ControlText $txtServerName $id.server.displayName
        Set-Status $txtTokenStatus "not_configured" "token"
        if ($id.compat.ownerTokenPresent -or $id.compat.legacyHostTokenPresent) {
            Set-Status $txtTokenStatus "configured" "token"
        }
        Set-ControlText $txtAdvancedDiagnostics "token-only relay 模式：使用访问令牌 + 实例/设备 ID。"
        Add-Log "本机身份已刷新。"
    } catch {
        Add-Log ("本机身份尚未加载：" + $_.Exception.Message)
    }
}

function Run-LocalInit {
    try {
        $saved = $false
        try {
            $saved = Sync-FormConfig -Quiet
        } catch {
            Write-ActionDiagnosticLog "保存配置" $_
            Add-Log ("保存表单时出现问题，将继续尝试本机初始化：" + $_.Exception.Message)
        }
        if (-not $saved) {
            Add-Log "表单未完整保存，直接调用本机初始化 API。"
        }
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/local/init"
        Set-ControlText $txtOperation ($op | ConvertTo-Json -Depth 12)
        if ($op.state -eq "success") {
            $lblState.Text = "状态：本机初始化完成"
            Refresh-Health
            Refresh-Identity
            Update-StepStatus
            [System.Windows.Forms.MessageBox]::Show("本机初始化完成。下一步请点击「测试连接」检查 VPS Coordinator。", "ACBH", "OK", "Information") | Out-Null
        } else {
            $lblState.Text = "状态：本机初始化失败"
            if ($op.error) {
                Set-Diagnostics ("errorCode=$($op.error.errorCode)" + [Environment]::NewLine + (Format-ErrorDetails $op.error.details))
            }
        }
        Add-Log ("本机初始化：" + $op.operationId + " state=" + $op.state)
    } catch {
        Show-ActionError "本机初始化" $_
    }
}

function Run-Init {
    Run-LocalInit
}

function Register-Identity {
    Add-Log "正在保存配置并刷新本机身份。"
    Run-LocalInit
}

function Save-ListenerConfig {
    try {
        $cfg = @{
            enabled = $true
            localHost = Get-TrimmedTextSafe $txtListenerHost "127.0.0.1"
            localPort = Get-IntFromTextSafe (Get-TrimmedTextSafe $txtListenerPort) 25565
            expectedProcessNames = @("java.exe", "javaw.exe")
            serverDirMatchRequired = $false
        }
        Invoke-BodyJson -Method "PUT" -Path "/v1/listener/config" -Body $cfg | Out-Null
        Add-Log "监听配置已保存。"
        Refresh-ListenerStatus
    } catch {
        Show-ActionError "保存监听" $_
    }
}

function Set-ListenerFields {
    param([object]$Status)
    Set-Status $txtListening ([string]$Status.listening) "bool"
    Set-ControlText $txtListenerWarnings ""
    if ($Status.warnings) {
        Set-ControlText $txtListenerWarnings (($Status.warnings | ForEach-Object { $_.code + ": " + $_.message }) -join "; ")
    }
    if ($Status.listeners -and $Status.listeners.Count -gt 0) {
        $item = $Status.listeners[0]
        Set-ControlText $txtListenerPid ([string]$item.pid)
        Set-ControlText $txtListenerProcess ([string]$item.processName)
        Set-ControlText $txtListenerCommand ([string]$item.commandLine)
        Set-Status $txtServerDirMatched ([string]$item.serverDirMatched) "bool"
    } else {
        Set-ControlText $txtListenerPid "未运行"
        Set-ControlText $txtListenerProcess "未运行"
        Set-ControlText $txtListenerCommand ""
        Set-ControlText $txtServerDirMatched "未知"
    }
}

function Refresh-ListenerStatus {
    try {
        $status = Invoke-BodyJson -Method "GET" -Path "/v1/listener/status"
        Set-ListenerFields $status
        if (-not $status.listening) {
            Add-Log "未检测到 Minecraft 服务端监听。请先用你的启动器或脚本启动服务端，再刷新监听状态。"
        } else {
            Add-Log "监听状态已刷新。"
        }
    } catch {
        Show-ActionError "刷新监听" $_
    }
}

function Probe-Listener {
    try {
        $status = Invoke-BodyJson -Method "POST" -Path "/v1/listener/probe"
        Set-ListenerFields $status
        Add-Log "监听探测完成。"
    } catch {
        Show-ActionError "探测监听" $_
    }
}

function Set-RelayConfigureDiagnostics {
    param(
        [object]$ErrorPayload,
        [string]$FallbackMessage = "配置 VPS 中转失败"
    )
    $text = $FallbackMessage
    if ($ErrorPayload -is [System.Management.Automation.ErrorRecord]) {
        $text = Format-BodyError $ErrorPayload
    } elseif ($null -ne $ErrorPayload) {
        if ($ErrorPayload.error) {
            $text = "errorCode=$($ErrorPayload.error.errorCode)" + [Environment]::NewLine + (Format-ErrorDetails $ErrorPayload.error.details)
        } elseif ($ErrorPayload.errorCode) {
            $text = "errorCode=$($ErrorPayload.errorCode)" + [Environment]::NewLine + (Format-ErrorDetails $ErrorPayload.details)
        }
    }
    if ([string]::IsNullOrWhiteSpace($text)) { $text = $FallbackMessage }
    $safe = Redact-Secrets $text
    Set-ControlText $txtRelayError $safe
    Set-Diagnostics $safe
    return $safe
}

function Format-RelayBoolStatus {
    param([object]$Value, [string]$RunningLabel = "运行中", [string]$StoppedLabel = "停止")
    if ($null -eq $Value) { return "未知" }
    if ($Value -is [bool]) {
        if ($Value) { return $RunningLabel }
        return $StoppedLabel
    }
    $text = [string]$Value
    if ($text -eq "True" -or $text -eq "true") { return $RunningLabel }
    if ($text -eq "False" -or $text -eq "false") { return $StoppedLabel }
    return (Normalize-GuiStatusValue $text "relay")
}

function Set-RelayFields {
    param([object]$Relay)
    Set-ControlText $txtPublicEndpoint $Relay.publicEndpoint
    Set-ControlText $txtLocalEndpoint $Relay.localEndpoint
    $stateParts = @(
        "configured=$($Relay.configured)",
        "active=$($Relay.active)",
        "currentHost=$($Relay.currentHost)",
        "currentDevice=$($Relay.currentDevice)",
        "heartbeatRunning=$($Relay.heartbeatRunning)",
        "heartbeatOk=$($Relay.heartbeatOk)",
        "leaseRenewRunning=$($Relay.leaseRenewRunning)",
        "leaseActive=$($Relay.leaseActive)",
        "tunnelConnected=$($Relay.tunnelConnected)",
        "sessionPumpRunning=$($Relay.sessionPumpRunning)",
        "publicListenerReady=$($Relay.publicListenerReady)",
        "localMinecraftReachable=$($Relay.localMinecraftReachable)",
        "lastHeartbeatAt=$($Relay.lastHeartbeatAt)",
        "leaseExpiresAt=$($Relay.leaseExpiresAt)",
        "activeUntil=$($Relay.activeUntil)",
        "lastDisconnectReason=$($Relay.lastDisconnectReason)"
    )
    Set-ControlText $txtRelayState ($stateParts -join " ")
    if ($Relay.active) {
        Set-Status $txtRelayCheckStatus "running" "relay"
        if ($txtRelayEntryStatus) { Set-Status $txtRelayEntryStatus "running" "relay" }
        $lblState.Text = "状态：公网入口可用（数据面已连通）"
    } elseif ($Relay.configured) {
        Set-Status $txtRelayCheckStatus "failed" "relay"
        if ($txtRelayEntryStatus) { Set-Status $txtRelayEntryStatus "failed" "relay" }
        $reason = if ($Relay.lastDisconnectReason) { $Relay.lastDisconnectReason } else { "relay_not_active" }
        $lblState.Text = "状态：中转未就绪（$reason）"
    } else {
        Set-Status $txtRelayCheckStatus "not_configured" "relay"
        if ($txtRelayEntryStatus) { Set-Status $txtRelayEntryStatus "not_configured" "relay" }
    }
    if ($txtHeartbeatStatus) {
        Set-ControlText $txtHeartbeatStatus (Format-RelayBoolStatus $Relay.heartbeatRunning "运行中" "停止")
    }
    if ($txtLeaseStatus) {
        Set-ControlText $txtLeaseStatus (Format-RelayBoolStatus $Relay.leaseActive "有效" "过期")
    }
    if ($txtTunnelStatus) {
        Set-ControlText $txtTunnelStatus (Format-RelayBoolStatus $Relay.tunnelConnected "已连接" "未连接")
    }
    if ($txtSessionPumpStatus) {
        Set-ControlText $txtSessionPumpStatus (Format-RelayBoolStatus $Relay.sessionPumpRunning "运行中" "停止")
    }
    if ($txtPublicListenerStatus) {
        Set-ControlText $txtPublicListenerStatus (Format-RelayBoolStatus $Relay.publicListenerReady "已监听" "未监听")
    }
    if ($txtLocalMinecraftStatus) {
        Set-ControlText $txtLocalMinecraftStatus (Format-RelayBoolStatus $Relay.localMinecraftReachable "已监听" "未监听")
    }
    Set-ControlText $txtRelayError ""
    if ($Relay.lastTunnelError) {
        Set-ControlText $txtRelayError ("lastTunnelError=" + (Redact-Secrets ([string]$Relay.lastTunnelError)))
    }
    if ($Relay.errors) {
        $errText = ($Relay.errors | ForEach-Object { $_.errorCode + " httpStatus=" + $_.details.httpStatus + " body=" + (Redact-Secrets ([string]$_.details.responseBody)) }) -join "; "
        if ($Relay.lastTunnelError) { $errText = (Get-TrimmedTextSafe $txtRelayError) + "; " + $errText }
        Set-ControlText $txtRelayError $errText
    }
    if (-not $Relay.active -and $Relay.lastDisconnectReason) {
        Set-Diagnostics ("中转未激活：" + $Relay.lastDisconnectReason)
    }
}

function Configure-Relay {
    try {
        if (-not (Sync-FormConfig -Quiet)) { throw "保存当前表单配置失败，无法配置 VPS 中转。" }
        $body = @{
            localMinecraftHost = Get-TrimmedTextSafe $txtListenerHost "127.0.0.1"
            localMinecraftPort = Get-IntFromTextSafe (Get-TrimmedTextSafe $txtListenerPort) 25565
            publicMinecraftPort = Get-IntFromTextSafe (Get-TrimmedTextSafe $txtPublicPort) 25565
        }
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/relay/configure" -Body $body
        Set-ControlText $txtOperation ($op | ConvertTo-Json -Depth 12)
        if ($op.state -eq "success") {
            Set-RelayFields $op.result.relay
            if (-not $op.result.relay.active) {
                Set-RelayConfigureDiagnostics $op.result.relay ("配置已提交，但公网入口尚未可用。原因：" + $op.result.relay.lastDisconnectReason)
            }
            Update-StepStatus
            Add-Log "VPS 中转已配置。"
        } elseif ($op.error) {
            Set-Status $txtRelayCheckStatus "failed" "relay"
            Set-RelayConfigureDiagnostics $op
        }
    } catch {
        Set-Status $txtRelayCheckStatus "failed" "relay"
        Set-RelayConfigureDiagnostics $_
        Show-ActionError "配置 VPS 中转" $_
    }
}

function Refresh-RelayStatus {
    try {
        $status = Invoke-BodyJson -Method "GET" -Path "/v1/relay/status"
        Set-RelayFields $status.relay
        if ($status.relay.active) {
            Set-Status $txtRelayCheckStatus "running" "relay"
        } elseif ($status.relay.configured) {
            Set-Status $txtRelayCheckStatus "configured" "relay"
        } else {
            Set-Status $txtRelayCheckStatus "not_configured" "relay"
        }
        Add-Log "中转状态已刷新。"
    } catch {
        Show-ActionError "中转状态" $_
    }
}

function Set-BackupOperation {
    param([object]$Op)
    Set-ControlText $txtOperation ($Op | ConvertTo-Json -Depth 16)
    if ($Op.state -eq "success") {
        Set-ControlText $txtBackupError ""
    } elseif ($Op.error) {
        Set-ControlText $txtBackupError ("errorCode=$($Op.error.errorCode)" + [Environment]::NewLine + (Format-ErrorDetails $Op.error.details))
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
        Set-ControlText $txtBackupSummary "files=$($result.fileCount) roots=$($result.rootCount) size=$($result.logicalSize) profile=$($result.profileId)"
        Set-ControlText $txtBackupError ""
        Add-Log "备份分析完成。"
    } catch {
        Show-ActionError "分析备份" $_
    }
}

function Upload-Backup {
    try {
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/backup/upload"
        $op = Wait-BodyOperation $op
        if ($op.state -eq "success") {
            Set-ControlText $txtBackupSummary "snapshot=$($op.result.snapshotId) uploaded=$($op.result.uploadedSize) deduped=$($op.result.deduplicatedSize) actualRequestUrl=$($op.result.actualRequestUrl)"
            Add-Log "备份上传完成。"
        }
    } catch {
        Show-ActionError "上传备份" $_
    }
}

function Refresh-Snapshots {
    try {
        $result = Invoke-BodyJson -Method "GET" -Path "/v1/snapshots"
        Set-ControlText $txtSnapshotList ($result.snapshots | ConvertTo-Json -Depth 8)
        if ($result.snapshots -and $result.snapshots.Count -gt 0) {
            Set-ControlText $txtSnapshotId $result.snapshots[0].snapshotId
        }
        Add-Log "快照列表已刷新。"
    } catch {
        Show-ActionError "刷新快照" $_
    }
}

function Choose-RestoreDir {
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = "选择一个新的空恢复目录"
    $restoreDir = Get-TrimmedTextSafe $txtRestoreTargetDir
    if ($restoreDir) { $dialog.SelectedPath = $restoreDir }
    if ($dialog.ShowDialog($form) -eq [System.Windows.Forms.DialogResult]::OK) {
        Set-ControlText $txtRestoreTargetDir $dialog.SelectedPath
    }
}

function Download-LatestSnapshot {
    try {
        $body = @{
            targetDir = Get-TrimmedTextSafe $txtRestoreTargetDir
            allowNonEmpty = $(if ($chkAllowNonEmpty) { $chkAllowNonEmpty.Checked } else { $false })
        }
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/snapshots/latest/download" -Body $body
        $op = Wait-BodyOperation $op
        if ($op.state -eq "success") {
            Set-ControlText $txtBackupSummary "downloaded=$($op.result.downloadedFiles) snapshot=$($op.result.snapshotId) target=$($op.result.targetDir)"
            Add-Log "最新快照下载完成。"
        }
    } catch {
        Show-ActionError "下载最新快照" $_
    }
}

function Download-SelectedSnapshot {
    try {
        $snapshotId = Get-TrimmedTextSafe $txtSnapshotId
        if (-not $snapshotId) { throw "Snapshot ID is required." }
        $body = @{
            targetDir = Get-TrimmedTextSafe $txtRestoreTargetDir
            allowNonEmpty = $(if ($chkAllowNonEmpty) { $chkAllowNonEmpty.Checked } else { $false })
        }
        $op = Invoke-BodyJson -Method "POST" -Path ("/v1/snapshots/" + [uri]::EscapeDataString($snapshotId) + "/download") -Body $body
        $op = Wait-BodyOperation $op
        if ($op.state -eq "success") {
            Set-ControlText $txtBackupSummary "downloaded=$($op.result.downloadedFiles) snapshot=$($op.result.snapshotId) target=$($op.result.targetDir)"
            Add-Log "所选快照下载完成。"
        }
    } catch {
        Show-ActionError "下载所选快照" $_
    }
}

function Choose-ServerDir {
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = "选择 Minecraft 服务端目录"
    $serverDir = Get-TrimmedTextSafe $txtServerDir
    if ($serverDir) { $dialog.SelectedPath = $serverDir }
    if ($dialog.ShowDialog($form) -eq [System.Windows.Forms.DialogResult]::OK) {
        Set-ControlText $txtServerDir $dialog.SelectedPath
        Update-StepStatus
    }
}

function Add-Label {
    param([string]$Text, [int]$X, [int]$Y, [object]$Parent = $form, [int]$W = 140)
    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Text
    $label.Location = New-Object System.Drawing.Point($X, $Y)
    $label.Size = New-Object System.Drawing.Size($W, 22)
    $Parent.Controls.Add($label)
    return $label
}

function Add-TextBox {
    param(
        [int]$X,
        [int]$Y,
        [int]$W,
        [bool]$ReadOnly = $false,
        [object]$Parent = $form,
        [bool]$Password = $false,
        [int]$H = 24,
        [bool]$MultiLine = $false
    )
    $box = New-Object System.Windows.Forms.TextBox
    $box.Location = New-Object System.Drawing.Point($X, $Y)
    $box.Size = New-Object System.Drawing.Size($W, $H)
    $box.ReadOnly = $ReadOnly
    $box.Multiline = $MultiLine
    if ($MultiLine) { $box.ScrollBars = "Vertical" }
    if ($Password) { $box.UseSystemPasswordChar = $true }
    $Parent.Controls.Add($box)
    return $box
}

function Add-Button {
    param([string]$Text, [int]$X, [int]$Y, [scriptblock]$Click, [object]$Parent = $form, [int]$W = 170, [int]$H = 30)
    $button = New-Object System.Windows.Forms.Button
    $button.Text = $Text
    $button.Location = New-Object System.Drawing.Point($X, $Y)
    $button.Size = New-Object System.Drawing.Size($W, $H)
    $button.Add_Click({ try { & $Click } catch { Show-Error $_.Exception.Message } }.GetNewClosure())
    $Parent.Controls.Add($button)
    return $button
}

function Add-GroupBox {
    param([string]$Text, [int]$X, [int]$Y, [int]$W, [int]$H, [object]$Parent = $form)
    $group = New-Object System.Windows.Forms.GroupBox
    $group.Text = $Text
    $group.Location = New-Object System.Drawing.Point($X, $Y)
    $group.Size = New-Object System.Drawing.Size($W, $H)
    $Parent.Controls.Add($group)
    return $group
}

function Register-AdvancedControl {
    param([object]$Control)
    $script:AdvancedControls += $Control
}

function Set-AdvancedVisible {
    param([bool]$Visible)
    $script:AdvancedVisible = $Visible
    foreach ($control in $script:AdvancedControls) {
        if ($null -ne $control) { $control.Visible = $Visible }
    }
    if ($Visible) {
        $btnToggleAdvanced.Text = "隐藏高级诊断"
    } else {
        $btnToggleAdvanced.Text = "显示高级诊断"
    }
}

function Toggle-AdvancedDiagnostics {
    Set-AdvancedVisible (-not $script:AdvancedVisible)
}

function Set-StepLabel {
    param([object]$Label, [string]$Text, [string]$State)
    if ($null -eq $Label) { return }
    $Label.Text = $Text + "：" + $State
}

function Update-StepStatus {
    $step1 = "需要处理"
    $coordText = Get-TrimmedTextSafe $txtCoordinator
    $tokenStatus = Get-TrimmedTextSafe $txtTokenStatus "未配置"
    if (-not [string]::IsNullOrWhiteSpace($coordText) -and $tokenStatus -ne "未配置") { $step1 = "已完成" }
    $step2 = "需要处理"
    $serverDirText = Get-TrimmedTextSafe $txtServerDir
    if (-not [string]::IsNullOrWhiteSpace($serverDirText)) {
        $step2 = "已完成"
    } else {
        $step2 = "服务器目录未配置"
    }
    $step3 = "未完成"
    $instanceId = Get-TrimmedTextSafe $txtInstanceId
    $deviceId = Get-TrimmedTextSafe $txtDeviceId
    if ($instanceId -like "acbh_instance_*" -and $deviceId -like "acbh_device_*" -and $instanceId -ne $deviceId) { $step3 = "已完成" }
    $step4 = "未完成"
    $coordinatorStatus = Get-TrimmedTextSafe $txtCoordinatorCheckStatus "未配置"
    if ($coordinatorStatus -in @("已连接", "不可达", "失败", "返回错误", "版本不匹配")) { $step4 = "已完成" }
    $step5 = "未完成"
    $relayEntryStatus = if ($txtRelayEntryStatus) { Get-TrimmedTextSafe $txtRelayEntryStatus "未配置" } else { Get-TrimmedTextSafe $txtRelayCheckStatus "未配置" }
    if ($relayEntryStatus -eq "运行中") { $step5 = "已完成" }
    elseif ($relayEntryStatus -eq "失败") { $step5 = "已配置但未激活" }
    $step6 = "未完成"
    if ($relayEntryStatus -eq "运行中") { $step6 = "可连接" }
    Set-StepLabel $lblStep1 "步骤 1：填写 VPS 地址和访问令牌" $step1
    Set-StepLabel $lblStep2 "步骤 2：选择 Minecraft 服务端目录" $step2
    Set-StepLabel $lblStep3 "步骤 3：点击「一键初始化本机」" $step3
    Set-StepLabel $lblStep4 "步骤 4：点击「测试连接」" $step4
    Set-StepLabel $lblStep5 "步骤 5：点击「配置 VPS 中转」" $step5
    Set-StepLabel $lblStep6 "步骤 6：启动服务端并让玩家连接公网地址" $step6
}

$form = New-Object System.Windows.Forms.Form
$form.Text = "ACBH v0.5 Minimal Core"
$form.StartPosition = "CenterScreen"
$form.Size = New-Object System.Drawing.Size(1280, 980)
$form.MinimumSize = New-Object System.Drawing.Size(1100, 800)
$form.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 9)
$form.AutoScaleMode = [System.Windows.Forms.AutoScaleMode]::Dpi
$form.AutoScroll = $true

$script:AdvancedControls = @()
$script:AdvancedVisible = $false
$toolTip = New-Object System.Windows.Forms.ToolTip

$title = New-Object System.Windows.Forms.Label
$title.Text = "ACBH minimal-core"
$title.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 18, [System.Drawing.FontStyle]::Bold)
$title.Location = New-Object System.Drawing.Point(20, 16)
$title.Size = New-Object System.Drawing.Size(360, 36)
$form.Controls.Add($title)

$lblState = New-Object System.Windows.Forms.Label
$lblState.Text = "状态：启动中"
$lblState.Location = New-Object System.Drawing.Point(400, 24)
$lblState.Size = New-Object System.Drawing.Size(760, 24)
$form.Controls.Add($lblState)

$grpSteps = Add-GroupBox "首次使用步骤" 24 58 1210 92
$lblStep1 = Add-Label "步骤 1：填写 VPS 地址和访问令牌：需要处理" 18 26 $grpSteps 540
$lblStep2 = Add-Label "步骤 2：选择 Minecraft 服务端目录：需要处理" 18 56 $grpSteps 540
$lblStep3 = Add-Label "步骤 3：点击「一键初始化本机」：未完成" 600 26 $grpSteps 560
$lblStep4 = Add-Label "步骤 4：点击「测试连接」：未完成" 600 56 $grpSteps 300
$lblStep5 = Add-Label "步骤 5：点击「配置 VPS 中转」：未完成" 900 56 $grpSteps 280
$lblStep6 = Add-Label "步骤 6：启动服务端并让玩家连接公网地址：未完成" 900 26 $grpSteps 300

$grpConnection = Add-GroupBox "连接 VPS Coordinator" 24 160 1210 126
Add-Label "VPS 地址" 18 34 $grpConnection 120 | Out-Null
$txtCoordinator = Add-TextBox 150 32 540 $false $grpConnection
$toolTip.SetToolTip($txtCoordinator, "填写 Coordinator API 地址，例如 http://你的VPS_IP:6121。这里不是 Minecraft 进服端口 25565。")
$lblCoordinatorHint = Add-Label "示例：http://你的VPS_IP:6121（不是 Minecraft 进服端口 25565）" 710 34 $grpConnection 460
Add-Label "访问令牌" 18 76 $grpConnection 120 | Out-Null
$txtAccessToken = Add-TextBox 150 74 540 $false $grpConnection $true
$toolTip.SetToolTip($txtAccessToken, "访问令牌会脱敏保存；不在界面明文显示完整 token。")
Add-Label "令牌状态" 710 76 $grpConnection 100 | Out-Null
$txtTokenStatus = Add-TextBox 820 74 150 $true $grpConnection
Set-Status $txtTokenStatus "not_configured" "token"
Add-Label "VPS 状态" 990 76 $grpConnection 90 | Out-Null
$txtCoordinatorCheckStatus = Add-TextBox 1080 74 110 $true $grpConnection
Set-Status $txtCoordinatorCheckStatus "not_configured" "coordinator"

$grpIdentity = Add-GroupBox "本机身份" 24 296 1210 82
Add-Label "私人实例名称" 18 36 $grpIdentity 120 | Out-Null
$txtInstanceName = Add-TextBox 150 34 390 $false $grpIdentity
Set-ControlText $txtInstanceName "私人 ACBH 实例"
Add-Label "当前设备名称" 620 36 $grpIdentity 120 | Out-Null
$txtDeviceName = Add-TextBox 750 34 390 $false $grpIdentity
Set-ControlText $txtDeviceName $env:COMPUTERNAME

$grpServer = Add-GroupBox "Minecraft 服务端" 24 388 1210 102
Add-Label "服务器名称" 18 34 $grpServer 120 | Out-Null
$txtServerName = Add-TextBox 150 32 390 $false $grpServer
Set-ControlText $txtServerName "Minecraft 服务端"
Add-Label "服务器目录" 18 70 $grpServer 120 | Out-Null
$txtServerDir = Add-TextBox 150 68 820 $false $grpServer
Add-Button "选择目录" 990 65 { Choose-ServerDir } $grpServer 150 30 | Out-Null

$grpListenerRelay = Add-GroupBox "监听 / VPS 中转" 24 500 1210 214

function Add-GroupLabel {
    param([string]$Text, [int]$X, [int]$Y, [int]$W = 120)
    return Add-Label $Text $X $Y $grpListenerRelay $W
}

function Add-GroupTextBox {
    param([int]$X, [int]$Y, [int]$W, [bool]$ReadOnly = $false, [int]$H = 24, [bool]$MultiLine = $false)
    return Add-TextBox $X $Y $W $ReadOnly $grpListenerRelay $false $H $MultiLine
}

function Add-GroupButton {
    param([string]$Text, [int]$X, [int]$Y, [scriptblock]$Click, [int]$W = 130)
    return Add-Button $Text $X $Y $Click $grpListenerRelay $W 28
}

Add-GroupLabel "本地监听" 18 26 200 | Out-Null
Add-GroupLabel "本地地址" 18 56 | Out-Null
$txtListenerHost = Add-GroupTextBox 150 54 260
Set-ControlText $txtListenerHost "127.0.0.1"
Add-GroupLabel "本地端口" 390 56 | Out-Null
$txtListenerPort = Add-GroupTextBox 520 54 180
Set-ControlText $txtListenerPort "25565"
Add-GroupLabel "公网中转" 18 98 200 | Out-Null
Add-GroupLabel "公网地址" 18 98 | Out-Null
$txtPublicHost = Add-GroupTextBox 150 96 260
Add-GroupLabel "公网端口" 390 98 | Out-Null
$txtPublicPort = Add-GroupTextBox 520 96 180
Set-ControlText $txtPublicPort "25565"
Add-GroupLabel "监听状态" 740 56 | Out-Null
$txtListening = Add-GroupTextBox 850 54 120 $true
Set-ControlText $txtListening "未知"
Add-GroupLabel "公网端点" 740 98 | Out-Null
$txtPublicEndpoint = Add-GroupTextBox 850 96 330 $true

Add-GroupButton "保存监听" 150 136 { Save-ListenerConfig } 130 | Out-Null
Add-GroupButton "刷新监听" 290 136 { Refresh-ListenerStatus } 130 | Out-Null
Add-GroupButton "探测监听" 430 136 { Probe-Listener } 130 | Out-Null
Add-GroupButton "配置 VPS 中转" 570 136 { Configure-Relay } 160 | Out-Null
Add-GroupButton "中转状态" 740 136 { Refresh-RelayStatus } 130 | Out-Null

Add-GroupLabel "本地端点" 18 174 | Out-Null
$txtLocalEndpoint = Add-GroupTextBox 150 172 260 $true
Add-GroupLabel "中转状态" 430 174 | Out-Null
$txtRelayCheckStatus = Add-GroupTextBox 520 172 180 $true
Set-Status $txtRelayCheckStatus "not_configured" "relay"
Add-GroupLabel "警告" 740 174 | Out-Null
$txtListenerWarnings = Add-GroupTextBox 850 172 330 $true

$grpActions = Add-GroupBox "操作" 24 724 1210 82
Add-Button "一键初始化本机" 18 32 { Run-LocalInit } $grpActions 170 32 | Out-Null
Add-Button "保存配置" 200 32 { Save-Config; Update-StepStatus } $grpActions 140 32 | Out-Null
Add-Button "测试连接" 352 32 { Test-Coordinator; Update-StepStatus } $grpActions 140 32 | Out-Null
Add-Button "配置 VPS 中转" 504 32 { Configure-Relay; Update-StepStatus } $grpActions 160 32 | Out-Null
Add-Button "刷新状态" 676 32 { Refresh-Health; Load-Config; Refresh-Identity; Refresh-RelayStatus; Update-StepStatus } $grpActions 140 32 | Out-Null


$grpStatus = Add-GroupBox "状态" 24 816 1210 186
Add-Label "本地 Body API" 18 34 $grpStatus 120 | Out-Null
$txtLocalBodyStatus = Add-TextBox 150 32 160 $true $grpStatus
Set-Status $txtLocalBodyStatus "unreachable" "body"
Add-Label "访问令牌" 340 34 $grpStatus 90 | Out-Null
$txtTokenValidationStatus = Add-TextBox 430 32 120 $true $grpStatus
Set-Status $txtTokenValidationStatus "not_configured" "token"
Add-Label "远端初始化" 570 34 $grpStatus 90 | Out-Null
$txtBootstrapStatus = Add-TextBox 660 32 120 $true $grpStatus
Set-Status $txtBootstrapStatus "not_configured" "bootstrap"
Add-Label "中转接口" 800 34 $grpStatus 90 | Out-Null
$txtRelayProbeStatus = Add-TextBox 890 32 120 $true $grpStatus
Set-Status $txtRelayProbeStatus "not_configured" "relay"
Add-Label "Heartbeat" 18 74 $grpStatus 120 | Out-Null
$txtHeartbeatStatus = Add-TextBox 150 72 120 $true $grpStatus
Set-ControlText $txtHeartbeatStatus "未知"
Add-Label "Lease" 290 74 $grpStatus 60 | Out-Null
$txtLeaseStatus = Add-TextBox 350 72 120 $true $grpStatus
Set-ControlText $txtLeaseStatus "未知"
Add-Label "Tunnel" 490 74 $grpStatus 60 | Out-Null
$txtTunnelStatus = Add-TextBox 550 72 120 $true $grpStatus
Set-ControlText $txtTunnelStatus "未知"
Add-Label "Session Pump" 690 74 $grpStatus 100 | Out-Null
$txtSessionPumpStatus = Add-TextBox 790 72 120 $true $grpStatus
Set-ControlText $txtSessionPumpStatus "未知"
Add-Label "VPS 25565" 930 74 $grpStatus 80 | Out-Null
$txtPublicListenerStatus = Add-TextBox 1010 72 120 $true $grpStatus
Set-ControlText $txtPublicListenerStatus "未知"
Add-Label "本地 MC" 18 114 $grpStatus 70 | Out-Null
$txtLocalMinecraftStatus = Add-TextBox 150 112 120 $true $grpStatus
Set-ControlText $txtLocalMinecraftStatus "未知"
Add-Label "公网入口" 290 114 $grpStatus 60 | Out-Null
$txtRelayEntryStatus = Add-TextBox 350 112 120 $true $grpStatus
Set-Status $txtRelayEntryStatus "not_configured" "relay"
Add-Label "协议版本" 490 114 $grpStatus 90 | Out-Null
$txtProtocol = Add-TextBox 580 112 70 $true $grpStatus
Add-Label "Coordinator 版本" 670 114 $grpStatus 120 | Out-Null
$txtCoordinatorVersion = Add-TextBox 790 112 410 $true $grpStatus
Add-Label "诊断提示" 18 154 $grpStatus 120 | Out-Null
$txtErrorDetails = Add-TextBox 150 152 1030 $true $grpStatus $false 24 $true

$txtLog = New-Object System.Windows.Forms.TextBox
$txtLog.Location = New-Object System.Drawing.Point(24, 976)
$txtLog.Size = New-Object System.Drawing.Size(1210, 72)
$txtLog.Multiline = $true
$txtLog.ScrollBars = "Vertical"
$txtLog.ReadOnly = $true
$txtLog.Font = New-Object System.Drawing.Font("Consolas", 9)
$form.Controls.Add($txtLog)

$btnToggleAdvanced = Add-Button "显示高级诊断" 24 1062 { Toggle-AdvancedDiagnostics } $form 170 32

$grpAdvanced = Add-GroupBox "高级诊断" 24 1106 1210 360
Register-AdvancedControl $grpAdvanced

Add-Label "Body API 地址" 18 34 $grpAdvanced 120 | Out-Null
$txtBodyApi = Add-TextBox 150 32 430 $true $grpAdvanced
Add-Label "配置文件" 610 34 $grpAdvanced 100 | Out-Null
$txtConfigPath = Add-TextBox 720 32 460 $true $grpAdvanced
Add-Label "模式" 18 74 $grpAdvanced 120 | Out-Null
$cmbMode = New-Object System.Windows.Forms.ComboBox
$cmbMode.Location = New-Object System.Drawing.Point(150, 72)
$cmbMode.Size = New-Object System.Drawing.Size(180, 24)
$cmbMode.DropDownStyle = "DropDownList"
[void]$cmbMode.Items.Add("remote-public")
[void]$cmbMode.Items.Add("local-private")
$cmbMode.SelectedItem = "remote-public"
$grpAdvanced.Controls.Add($cmbMode)
Add-Label "实例 ID" 360 74 $grpAdvanced 80 | Out-Null
$txtInstanceId = Add-TextBox 450 72 300 $true $grpAdvanced
Add-Label "设备 ID" 780 74 $grpAdvanced 80 | Out-Null
$txtDeviceId = Add-TextBox 870 72 310 $true $grpAdvanced
$txtServerId = Add-TextBox 930 112 250 $true $grpAdvanced
$txtServerId.Visible = $false
Add-Label "实际请求 URL" 18 154 $grpAdvanced 120 | Out-Null
$txtActualRequestUrl = Add-TextBox 150 152 1030 $true $grpAdvanced
Add-Label "构建" 18 194 $grpAdvanced 120 | Out-Null
$txtCoordinatorCommit = Add-TextBox 150 192 300 $true $grpAdvanced
Add-Label "能力" 480 194 $grpAdvanced 120 | Out-Null
$txtCapabilities = Add-TextBox 610 192 570 $true $grpAdvanced
Add-Label "进程 ID" 18 234 $grpAdvanced 120 | Out-Null
$txtListenerPid = Add-TextBox 150 232 120 $true $grpAdvanced
Add-Label "进程" 300 234 $grpAdvanced 80 | Out-Null
$txtListenerProcess = Add-TextBox 380 232 240 $true $grpAdvanced
Add-Label "目录匹配" 650 234 $grpAdvanced 100 | Out-Null
$txtServerDirMatched = Add-TextBox 750 232 120 $true $grpAdvanced
Add-Label "启动命令" 18 274 $grpAdvanced 120 | Out-Null
$txtListenerCommand = Add-TextBox 150 272 1030 $true $grpAdvanced
Add-Label "中转原始状态" 18 314 $grpAdvanced 120 | Out-Null
$txtRelayState = Add-TextBox 150 312 460 $true $grpAdvanced
Add-Label "中转错误" 640 314 $grpAdvanced 90 | Out-Null
$txtRelayError = Add-TextBox 730 312 450 $true $grpAdvanced

$txtAdvancedDiagnostics = Add-TextBox 24 1480 1210 $true $form $false 46 $true
Register-AdvancedControl $txtAdvancedDiagnostics

$txtOperation = Add-TextBox 24 1540 1210 $true $form $false 86 $true
$txtOperation.Font = New-Object System.Drawing.Font("Consolas", 9)
Register-AdvancedControl $txtOperation

$grpBackup = Add-GroupBox "备份 / 快照" 24 1640 1210 220
Register-AdvancedControl $grpBackup

function Add-BackupLabel {
    param([string]$Text, [int]$X, [int]$Y)
    return Add-Label $Text $X $Y $grpBackup 120
}

function Add-BackupTextBox {
    param([int]$X, [int]$Y, [int]$W, [bool]$ReadOnly = $false, [int]$H = 24, [bool]$MultiLine = $false)
    return Add-TextBox $X $Y $W $ReadOnly $grpBackup $false $H $MultiLine
}

function Add-BackupButton {
    param([string]$Text, [int]$X, [int]$Y, [scriptblock]$Click)
    return Add-Button $Text $X $Y $Click $grpBackup 130 28
}

Add-BackupButton "分析备份" 16 26 { Analyze-Backup } | Out-Null
Add-BackupButton "上传备份" 156 26 { Upload-Backup } | Out-Null
Add-BackupButton "列出快照" 296 26 { Refresh-Snapshots } | Out-Null
Add-BackupButton "下载最新" 436 26 { Download-LatestSnapshot } | Out-Null
Add-BackupButton "下载所选" 576 26 { Download-SelectedSnapshot } | Out-Null

Add-BackupLabel "快照 ID" 16 66 | Out-Null
$txtSnapshotId = Add-BackupTextBox 118 64 260
Add-BackupLabel "恢复目录" 410 66 | Out-Null
$txtRestoreTargetDir = Add-BackupTextBox 520 64 460
Add-BackupButton "选择目录" 1000 61 { Choose-RestoreDir } | Out-Null
$chkAllowNonEmpty = New-Object System.Windows.Forms.CheckBox
$chkAllowNonEmpty.Text = "允许非空目录"
$chkAllowNonEmpty.Location = New-Object System.Drawing.Point(118, 96)
$chkAllowNonEmpty.Size = New-Object System.Drawing.Size(180, 24)
$grpBackup.Controls.Add($chkAllowNonEmpty)

Add-BackupLabel "摘要" 16 128 | Out-Null
$txtBackupSummary = Add-BackupTextBox 118 126 1060 $true
Add-BackupLabel "快照" 16 160 | Out-Null
$txtSnapshotList = Add-BackupTextBox 118 158 520 $true
Add-BackupLabel "错误" 660 160 | Out-Null
$txtBackupError = Add-BackupTextBox 720 158 458 $true

Set-AdvancedVisible $false
Update-StepStatus

$form.Add_Shown({
    try {
        Start-Body
        Refresh-Health
        Load-Config
        Refresh-Identity
        Update-StepStatus
    } catch {
        Show-ActionError "启动" $_
    }
})

$form.Add_FormClosed({
    if ($script:BodyProcess -and -not $script:BodyProcess.HasExited) {
        try { $script:BodyProcess.Kill() } catch { }
    }
})

if ($SelfTest) {
    Test-GuiStaticChecks
    Test-GuiNullSafeBehavior
    if ($script:BodyProcess -and -not $script:BodyProcess.HasExited) {
        try { $script:BodyProcess.Kill() } catch {}
    }
    $script:BodyProcess = $null
    Write-Output "ACBH minimal-core GUI self-test ok"
    exit 0
}

[void]$form.ShowDialog()
