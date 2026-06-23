param(
    [string]$AgentPath,
    [string]$CoordinatorPath,
    [string]$AppDataDir,
    [string]$Port = "6121",
    [switch]$SelfTest
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

if (-not $AgentPath) {
    $AgentPath = Join-Path $PSScriptRoot "..\acbh-agent-windows-amd64.exe"
}
if (-not $CoordinatorPath) {
    $CoordinatorPath = Join-Path $PSScriptRoot "..\coordinator\dist\index.js"
}
if (-not $AppDataDir) {
    $BundleRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
    if (Test-Path (Join-Path $BundleRoot "portable.flag")) {
        $AppDataDir = Join-Path $BundleRoot "data"
    } else {
        $AppDataDir = Join-Path $env:APPDATA "ACBH"
    }
}

if ($SelfTest) {
    Write-Output "ACBH desktop GUI self-test ok"
    exit 0
}

$script:ToolTip = New-Object System.Windows.Forms.ToolTip
$script:LastStatus = $null
$script:Running = $false
$script:CurrentState = "Unconfigured"

function Redact-Secrets {
    param([string]$Text)
    if ([string]::IsNullOrEmpty($Text)) { return $Text }
    $safe = $Text
    $safe = [regex]::Replace($safe, '(?i)(accessKey|hostToken|joinToken|relayToken|proxyPassword|rcon\.password|ACBH_RCON_PASSWORD)(\s*[:=]\s*)[^\s,;}]+', '$1$2[已隐藏]')
    $safe = [regex]::Replace($safe, 'ak_[A-Za-z0-9_\-]+', 'ak_[已隐藏]')
    $safe = [regex]::Replace($safe, 'ht_[A-Za-z0-9_\-]+', 'ht_[已隐藏]')
    $safe = [regex]::Replace($safe, 'ACBH-[A-Fa-f0-9]{6}-[A-Fa-f0-9]{6}', 'ACBH-[邀请码已隐藏]')
    return $safe
}

function Add-GuiLog {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    $safe = Redact-Secrets $Text
    if ($script:LogBox -ne $null) {
        $script:LogBox.AppendText(("[" + (Get-Date -Format "HH:mm:ss") + "] " + $safe + [Environment]::NewLine))
        $script:LogBox.SelectionStart = $script:LogBox.TextLength
        $script:LogBox.ScrollToCaret()
    } else {
        Write-Host $safe
    }
}

function Show-GuiError {
    param([string]$Message)
    $safe = Redact-Secrets $Message
    Add-GuiLog $safe
    [System.Windows.Forms.MessageBox]::Show($safe, "ACBH", "OK", "Error") | Out-Null
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

function Invoke-AgentProcess {
    param(
        [Alias("Args")][string[]]$CommandArgs,
        [hashtable]$ExtraEnv
    )
    if (-not (Test-Path $AgentPath)) {
        throw "找不到 ACBH Agent：$AgentPath"
    }
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $AgentPath
    $psi.Arguments = Join-ProcessArguments $CommandArgs
    $psi.WorkingDirectory = Split-Path -Parent $AgentPath
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    if ($AppDataDir) {
        $psi.Environment["ACBH_APP_DATA_DIR"] = $AppDataDir
    }
    if ($ExtraEnv) {
        foreach ($key in $ExtraEnv.Keys) {
            $psi.Environment[$key] = [string]$ExtraEnv[$key]
        }
    }
    $process = [System.Diagnostics.Process]::Start($psi)
    $stdout = $process.StandardOutput.ReadToEnd()
    $stderr = $process.StandardError.ReadToEnd()
    $process.WaitForExit()
    return [pscustomobject]@{
        ExitCode = $process.ExitCode
        Stdout = Redact-Secrets ($stdout.Trim())
        Stderr = Redact-Secrets ($stderr.Trim())
        Output = Redact-Secrets ((($stdout, $stderr) -join [Environment]::NewLine).Trim())
    }
}

function ConvertFrom-JsonSafe {
    param([string]$Text, [string]$ActionName)
    $trimmed = ($Text | Out-String).Trim()
    if ([string]::IsNullOrWhiteSpace($trimmed)) {
        throw "$ActionName 没有返回 JSON。"
    }
    if (-not ($trimmed.StartsWith("{") -or $trimmed.StartsWith("["))) {
        Add-GuiLog "$ActionName 输出不是 JSON：$trimmed"
        throw "$ActionName 没有返回纯 JSON。"
    }
    try {
        return $trimmed | ConvertFrom-Json
    } catch {
        Add-GuiLog "$ActionName JSON 解析失败，原始输出：$trimmed"
        throw "$ActionName JSON 解析失败：$($_.Exception.Message)"
    }
}

function Invoke-AgentJson {
    param([Alias("Args")][string[]]$CommandArgs, [hashtable]$ExtraEnv)
    $result = Invoke-AgentProcess -CommandArgs $CommandArgs -ExtraEnv $ExtraEnv
    $text = if ($result.Stdout) { $result.Stdout } else { $result.Output }
    if ($result.ExitCode -ne 0) {
        $errText = if ($result.Stderr) { $result.Stderr } else { $result.Output }
        throw $errText
    }
    return ConvertFrom-JsonSafe -Text $text -ActionName ($CommandArgs -join " ")
}

function Invoke-AgentCommandSafe {
    param(
        [string]$ActionName,
        [Alias("Args")][string[]]$CommandArgs,
        [hashtable]$ExtraEnv,
        [switch]$Json,
        [switch]$Refresh
    )
    try {
        Set-Busy $true "$ActionName ..."
        $result = Invoke-AgentProcess -CommandArgs $CommandArgs -ExtraEnv $ExtraEnv
        if ($result.Output) { Add-GuiLog $result.Output }
        if ($result.ExitCode -ne 0) {
            Show-GuiError "$ActionName 失败。请查看日志区。"
            return $null
        }
        Add-GuiLog "$ActionName 完成。"
        if ($Refresh) { Refresh-Status }
        if ($Json) {
            return ConvertFrom-JsonSafe -Text $result.Stdout -ActionName $ActionName
        }
        return $true
    } catch {
        Show-GuiError "$ActionName 异常：$($_.Exception.Message)"
        return $null
    } finally {
        Set-Busy $false ""
    }
}

function Set-Busy {
    param([bool]$Busy, [string]$Text)
    $script:Running = $Busy
    if ($form -ne $null) { $form.UseWaitCursor = $Busy }
    if ($lblBusy -ne $null) { $lblBusy.Text = $Text }
    [System.Windows.Forms.Application]::DoEvents()
}

function Add-SafeClick {
    param([System.Windows.Forms.Control]$Control, [scriptblock]$Click)
    $safeClick = {
        try { & $Click } catch { Show-GuiError $_.Exception.Message }
    }.GetNewClosure()
    $Control.Add_Click($safeClick)
}

function Set-Checklist {
    param([object]$Report)
    $items = @(
        @("系统兼容", "platform"),
        @("数据目录可写", "atomic_write"),
        @("ACBH 运行组件完整", "agent_exe"),
        @("GUI 脚本完整", "gui_script"),
        @("安全存储可用", "dpapi"),
        @("私有 Node runtime", "private_node")
    )
    for ($i = 0; $i -lt $items.Count; $i++) {
        $label = $checkLabels[$i]
        $name = $items[$i][0]
        $id = $items[$i][1]
        $check = $Report.checks | Where-Object { $_.id -eq $id } | Select-Object -First 1
        if ($check -and $check.status -eq "passed") {
            $label.Text = "✓ $name"
            $label.ForeColor = [System.Drawing.Color]::DarkGreen
        } elseif ($check -and $check.status -eq "warning") {
            $label.Text = "△ $name：" + $check.message
            $label.ForeColor = [System.Drawing.Color]::DarkOrange
        } elseif ($check) {
            $label.Text = "✕ $name：" + $check.message
            $label.ForeColor = [System.Drawing.Color]::Firebrick
        } else {
            $label.Text = "○ $name"
            $label.ForeColor = [System.Drawing.Color]::DimGray
        }
    }
    $checkLabels[6].Text = "○ 公网服务器：等待配置"
    $checkLabels[7].Text = "○ Minecraft 服务端：等待选择"
}

function Run-EnvironmentCheck {
    $report = Invoke-AgentCommandSafe -ActionName "环境检查" -Args @("desktop", "environment", "check", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port) -Json
    if ($null -ne $report) {
        $script:CurrentState = $report.state
        Set-Checklist $report
        $lblState.Text = "状态：" + $report.state
        if (-not $report.ok -and $report.requiredPackages.Count -gt 0) {
            Show-GuiError "当前缺少必要运行环境，并且无法连接环境下载源。请在其他设备下载 ACBH 离线环境包后导入。"
        }
    }
}

function Configure-Network {
    $hostText = $txtHost.Text.Trim()
    if (-not $hostText) {
        Show-GuiError "请输入公网服务器 IP 或域名。"
        return
    }
    $result = Invoke-AgentCommandSafe -ActionName "公网服务器检查" -Args @("desktop", "setup", "configure-network", "--app-data-dir", $AppDataDir, "--host-name", $hostText, "--coordinator-port", "6121", "--public-game-port", "25565") -Json
    if ($null -ne $result) {
        $txtCoordinator.Text = $result.coordinatorUrl
        $txtPlayerAddress.Text = $result.playerAddress
        $checkLabels[6].Text = "✓ 公网服务器：" + $result.coordinatorUrl
        $checkLabels[6].ForeColor = [System.Drawing.Color]::DarkGreen
        if ($result.warnings) { $result.warnings | ForEach-Object { Add-GuiLog $_ } }
    }
}

function Create-Group {
    $coord = $txtCoordinator.Text.Trim()
    if (-not $coord) {
        Configure-Network
        $coord = $txtCoordinator.Text.Trim()
    }
    if (-not $coord) { return }
    $args = @("desktop", "setup", "create-group", "--app-data-dir", $AppDataDir, "--group-name", $txtGroupName.Text, "--display-name", $txtDisplayName.Text, "--coordinator-url", $coord)
    $result = Invoke-AgentCommandSafe -ActionName "创建 Group" -Args $args -Json
    if ($null -ne $result) {
        $lblGroupResult.Text = "Group 已创建，本机已注册。邀请码：" + $(if ($result.inviteCode) { $result.inviteCode } else { "当前 Coordinator 不支持" })
        Add-GuiLog "Group 已创建。本机 Host ID: $($result.hostId)"
    }
}

function Join-Group {
    $coord = $txtCoordinator.Text.Trim()
    if (-not $coord) {
        Show-GuiError "请先输入并检查公网服务器。"
        return
    }
    $code = $txtInvite.Text.Trim()
    if (-not $code) {
        Show-GuiError "请输入邀请码。"
        return
    }
    $args = @("desktop", "setup", "join-group", "--app-data-dir", $AppDataDir, "--invite-code", $code, "--display-name", $txtDisplayName.Text, "--coordinator-url", $coord)
    $result = Invoke-AgentCommandSafe -ActionName "加入 Group" -Args $args -Json
    if ($null -ne $result) {
        $lblGroupResult.Text = "已加入 Group，本机已注册。"
    }
}

function Choose-ServerDir {
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = "选择 Minecraft 服务端根目录"
    if ($dialog.ShowDialog($form) -ne [System.Windows.Forms.DialogResult]::OK) { return }
    $txtServerDir.Text = $dialog.SelectedPath
    $result = Invoke-AgentCommandSafe -ActionName "检测 Minecraft 服务端" -Args @("desktop", "setup", "inspect-server", "--app-data-dir", $AppDataDir, "--server-dir", $dialog.SelectedPath) -Json
    if ($null -ne $result) {
        $txtServerSummary.Text = "类型：" + $result.report.serverType + " / 启动入口：" + $result.report.launchEntry + " / Java：" + $result.javaVersion
        $checkLabels[7].Text = "✓ Minecraft 服务端：" + $result.report.serverType
        $checkLabels[7].ForeColor = [System.Drawing.Color]::DarkGreen
    }
}

function Complete-Setup {
    $result = Invoke-AgentCommandSafe -ActionName "完成配置" -Args @("desktop", "setup", "complete", "--app-data-dir", $AppDataDir) -Json
    if ($null -ne $result -and $result.ok) {
        $script:CurrentState = "Ready"
        $lblState.Text = "状态：Ready"
        Refresh-Status
    } elseif ($null -ne $result) {
        Show-GuiError $result.message
    }
}

function Refresh-Status {
    try {
        $status = Invoke-AgentJson -Args @("desktop", "server", "status", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port)
        $script:LastStatus = $status
        if ($status.state -eq "Running") {
            $lblServerStatus.Text = "服务器正在此电脑运行"
            $btnMain.Text = "停止服务器"
        } else {
            $lblServerStatus.Text = "当前无人运行服务器"
            $btnMain.Text = "在此电脑启动"
        }
        if ($status.status) {
            $txtPlayerAddress.Text = $(if ($status.status.publicEntryMessage) { $status.status.publicEntryMessage } else { $txtPlayerAddress.Text })
            $lblState.Text = "状态：" + $status.state
        }
        Add-GuiLog "状态已刷新。"
    } catch {
        Add-GuiLog "状态刷新失败：" + $_.Exception.Message
    }
}

function Invoke-MainAction {
    if ($btnMain.Text -eq "停止服务器") {
        $result = Invoke-AgentCommandSafe -ActionName "停止服务器" -Args @("desktop", "server", "stop-auto", "--app-data-dir", $AppDataDir) -Json
    } else {
        $result = Invoke-AgentCommandSafe -ActionName "在此电脑启动" -Args @("desktop", "server", "start-auto", "--app-data-dir", $AppDataDir) -Json
    }
    if ($null -ne $result) {
        if ($result.steps) { $result.steps | ForEach-Object { Add-GuiLog $_ } }
        if ($result.warnings) { $result.warnings | ForEach-Object { Add-GuiLog "提示：" + $_ } }
        if (-not $result.ok) { Show-GuiError $result.message }
        Refresh-Status
    }
}

function Import-OfflinePack {
    $dialog = New-Object System.Windows.Forms.OpenFileDialog
    $dialog.Filter = "ACBH 环境包 (*.zip)|*.zip|All files (*.*)|*.*"
    if ($dialog.ShowDialog($form) -ne [System.Windows.Forms.DialogResult]::OK) { return }
    $result = Invoke-AgentCommandSafe -ActionName "导入离线环境包" -Args @("desktop", "environment", "import-pack", "--app-data-dir", $AppDataDir, "--file", $dialog.FileName) -Json
    if ($null -ne $result -and $result.ok) {
        Add-GuiLog "离线环境包已导入：" + $result.package.packageId
        Run-EnvironmentCheck
    } elseif ($null -ne $result) {
        Show-GuiError $result.message
    }
}

function Open-LogDir {
    $logDir = Join-Path $AppDataDir "logs"
    New-Item -ItemType Directory -Force -Path $logDir | Out-Null
    Start-Process $logDir
}

function Add-Button {
    param([System.Windows.Forms.Control]$Parent, [string]$Text, [int]$X, [int]$Y, [scriptblock]$Click, [string]$Tip = "")
    $button = New-Object System.Windows.Forms.Button
    $button.Text = $Text
    $button.Location = New-Object System.Drawing.Point($X, $Y)
    $button.Size = New-Object System.Drawing.Size(170, 30)
    Add-SafeClick $button $Click
    if ($Tip) { $script:ToolTip.SetToolTip($button, $Tip) }
    $Parent.Controls.Add($button)
    return $button
}

$form = New-Object System.Windows.Forms.Form
$form.Text = "ACBH"
$form.StartPosition = "CenterScreen"
$form.Size = New-Object System.Drawing.Size(1040, 760)
$form.MinimumSize = New-Object System.Drawing.Size(980, 700)
$form.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 9)

$title = New-Object System.Windows.Forms.Label
$title.Text = "ACBH"
$title.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 22, [System.Drawing.FontStyle]::Bold)
$title.Location = New-Object System.Drawing.Point(22, 16)
$title.Size = New-Object System.Drawing.Size(220, 42)
$form.Controls.Add($title)

$lblState = New-Object System.Windows.Forms.Label
$lblState.Text = "状态：Unconfigured"
$lblState.Location = New-Object System.Drawing.Point(250, 28)
$lblState.Size = New-Object System.Drawing.Size(280, 24)
$form.Controls.Add($lblState)

$lblBusy = New-Object System.Windows.Forms.Label
$lblBusy.Location = New-Object System.Drawing.Point(550, 28)
$lblBusy.Size = New-Object System.Drawing.Size(420, 24)
$form.Controls.Add($lblBusy)

$bootstrapPanel = New-Object System.Windows.Forms.GroupBox
$bootstrapPanel.Text = "正在准备 ACBH"
$bootstrapPanel.Location = New-Object System.Drawing.Point(20, 70)
$bootstrapPanel.Size = New-Object System.Drawing.Size(470, 190)
$form.Controls.Add($bootstrapPanel)

$checkLabels = @()
foreach ($i in 0..7) {
    $label = New-Object System.Windows.Forms.Label
    $label.Text = "○ 等待检查"
    $label.Location = New-Object System.Drawing.Point(16, (24 + $i * 20))
    $label.Size = New-Object System.Drawing.Size(430, 18)
    $bootstrapPanel.Controls.Add($label)
    $checkLabels += $label
}
Add-Button $bootstrapPanel "重新检查环境" 280 150 { Run-EnvironmentCheck } "重新执行第 0 步环境检查。"

$setupPanel = New-Object System.Windows.Forms.GroupBox
$setupPanel.Text = "四步配置"
$setupPanel.Location = New-Object System.Drawing.Point(510, 70)
$setupPanel.Size = New-Object System.Drawing.Size(490, 300)
$form.Controls.Add($setupPanel)

$lbl1 = New-Object System.Windows.Forms.Label
$lbl1.Text = "第 1 步：创建或加入 Group"
$lbl1.Location = New-Object System.Drawing.Point(16, 24)
$lbl1.Size = New-Object System.Drawing.Size(230, 20)
$setupPanel.Controls.Add($lbl1)
$txtGroupName = New-Object System.Windows.Forms.TextBox
$txtGroupName.Text = "ACBH Server"
$txtGroupName.Location = New-Object System.Drawing.Point(18, 48)
$txtGroupName.Size = New-Object System.Drawing.Size(140, 24)
$setupPanel.Controls.Add($txtGroupName)
$txtDisplayName = New-Object System.Windows.Forms.TextBox
$txtDisplayName.Text = $env:USERNAME
$txtDisplayName.Location = New-Object System.Drawing.Point(166, 48)
$txtDisplayName.Size = New-Object System.Drawing.Size(130, 24)
$setupPanel.Controls.Add($txtDisplayName)
$txtInvite = New-Object System.Windows.Forms.TextBox
$txtInvite.Location = New-Object System.Drawing.Point(304, 48)
$txtInvite.Size = New-Object System.Drawing.Size(160, 24)
$setupPanel.Controls.Add($txtInvite)
Add-Button $setupPanel "创建 Group" 18 78 { Create-Group } ""
Add-Button $setupPanel "加入已有 Group" 198 78 { Join-Group } ""
$lblGroupResult = New-Object System.Windows.Forms.Label
$lblGroupResult.Location = New-Object System.Drawing.Point(18, 112)
$lblGroupResult.Size = New-Object System.Drawing.Size(445, 20)
$setupPanel.Controls.Add($lblGroupResult)

$lbl2 = New-Object System.Windows.Forms.Label
$lbl2.Text = "第 2 步：公网服务器 IP 或域名"
$lbl2.Location = New-Object System.Drawing.Point(16, 138)
$lbl2.Size = New-Object System.Drawing.Size(220, 20)
$setupPanel.Controls.Add($lbl2)
$txtHost = New-Object System.Windows.Forms.TextBox
$txtHost.Location = New-Object System.Drawing.Point(18, 162)
$txtHost.Size = New-Object System.Drawing.Size(160, 24)
$setupPanel.Controls.Add($txtHost)
$txtCoordinator = New-Object System.Windows.Forms.TextBox
$txtCoordinator.Location = New-Object System.Drawing.Point(186, 162)
$txtCoordinator.Size = New-Object System.Drawing.Size(278, 24)
$txtCoordinator.ReadOnly = $true
$setupPanel.Controls.Add($txtCoordinator)
Add-Button $setupPanel "检查公网服务器" 18 192 { Configure-Network } ""

$lbl3 = New-Object System.Windows.Forms.Label
$lbl3.Text = "第 3 步：Minecraft 服务端目录"
$lbl3.Location = New-Object System.Drawing.Point(16, 226)
$lbl3.Size = New-Object System.Drawing.Size(220, 20)
$setupPanel.Controls.Add($lbl3)
$txtServerDir = New-Object System.Windows.Forms.TextBox
$txtServerDir.Location = New-Object System.Drawing.Point(18, 250)
$txtServerDir.Size = New-Object System.Drawing.Size(278, 24)
$txtServerDir.ReadOnly = $true
$setupPanel.Controls.Add($txtServerDir)
Add-Button $setupPanel "选择目录" 304 248 { Choose-ServerDir } ""

$txtServerSummary = New-Object System.Windows.Forms.Label
$txtServerSummary.Location = New-Object System.Drawing.Point(18, 276)
$txtServerSummary.Size = New-Object System.Drawing.Size(445, 20)
$setupPanel.Controls.Add($txtServerSummary)

$mainPanel = New-Object System.Windows.Forms.GroupBox
$mainPanel.Text = "主界面"
$mainPanel.Location = New-Object System.Drawing.Point(20, 280)
$mainPanel.Size = New-Object System.Drawing.Size(470, 180)
$form.Controls.Add($mainPanel)

$lblServerStatus = New-Object System.Windows.Forms.Label
$lblServerStatus.Text = "当前无人运行服务器"
$lblServerStatus.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 12, [System.Drawing.FontStyle]::Bold)
$lblServerStatus.Location = New-Object System.Drawing.Point(18, 28)
$lblServerStatus.Size = New-Object System.Drawing.Size(420, 28)
$mainPanel.Controls.Add($lblServerStatus)

$txtPlayerAddress = New-Object System.Windows.Forms.TextBox
$txtPlayerAddress.Location = New-Object System.Drawing.Point(20, 66)
$txtPlayerAddress.Size = New-Object System.Drawing.Size(300, 24)
$txtPlayerAddress.ReadOnly = $true
$mainPanel.Controls.Add($txtPlayerAddress)

$btnMain = Add-Button $mainPanel "在此电脑启动" 20 104 { Invoke-MainAction } "自动申请主机资格、同步、启动 MC 和公网中转。"
Add-Button $mainPanel "完成并进入 ACBH" 204 104 { Complete-Setup } "保存 setupComplete=true。"
Add-Button $mainPanel "复制玩家地址" 20 140 { if ($txtPlayerAddress.Text) { [System.Windows.Forms.Clipboard]::SetText($txtPlayerAddress.Text); Add-GuiLog "玩家地址已复制。" } } ""
Add-Button $mainPanel "打开日志" 204 140 { Open-LogDir } ""

$advancedPanel = New-Object System.Windows.Forms.GroupBox
$advancedPanel.Text = "高级诊断"
$advancedPanel.Location = New-Object System.Drawing.Point(510, 385)
$advancedPanel.Size = New-Object System.Drawing.Size(490, 96)
$advancedPanel.Visible = $false
$form.Controls.Add($advancedPanel)

Add-Button $advancedPanel "导入离线环境包" 14 24 { Import-OfflinePack } ""
Add-Button $advancedPanel "运行环境修复" 192 24 { Invoke-AgentCommandSafe -ActionName "环境修复" -Args @("desktop", "environment", "repair", "--app-data-dir", $AppDataDir) -Json } ""
Add-Button $advancedPanel "查看高级状态" 14 58 { Invoke-AgentCommandSafe -ActionName "高级状态" -Args @("desktop", "status", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port, "--json") -Refresh } ""
Add-Button $advancedPanel "清理 runtime cache" 192 58 { Invoke-AgentCommandSafe -ActionName "清理 runtime cache" -Args @("desktop", "environment", "clear-cache", "--app-data-dir", $AppDataDir) -Json } ""

$btnAdvanced = Add-Button $form "高级诊断" 830 28 { $advancedPanel.Visible = -not $advancedPanel.Visible } "默认隐藏高级 CLI 能力。"

$script:LogBox = New-Object System.Windows.Forms.TextBox
$script:LogBox.Location = New-Object System.Drawing.Point(20, 500)
$script:LogBox.Size = New-Object System.Drawing.Size(980, 200)
$script:LogBox.Multiline = $true
$script:LogBox.ScrollBars = "Vertical"
$script:LogBox.ReadOnly = $true
$script:LogBox.Font = New-Object System.Drawing.Font("Consolas", 9)
$form.Controls.Add($script:LogBox)

$form.Add_Shown({
    try {
        Run-EnvironmentCheck
        Refresh-Status
    } catch {
        Show-GuiError "初始化失败：$($_.Exception.Message)"
    }
})

[void]$form.ShowDialog()
