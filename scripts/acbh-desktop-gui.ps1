param(
    [string]$AgentPath,
    [string]$CoordinatorPath,
    [string]$AppDataDir,
    [string]$Port = "6121"
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

$script:ServerDir = ""

function Invoke-Agent {
    param([string[]]$Args)
    if (-not (Test-Path $AgentPath)) {
        throw "找不到本地主机代理：$AgentPath"
    }
    $oldAppData = $env:ACBH_APP_DATA_DIR
    try {
        $env:ACBH_APP_DATA_DIR = $AppDataDir
        $output = & $AgentPath @Args 2>&1 | Out-String
        return $output.Trim()
    } finally {
        $env:ACBH_APP_DATA_DIR = $oldAppData
    }
}

function Append-Log {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    $logBox.AppendText(("[" + (Get-Date -Format "HH:mm:ss") + "] " + $Text + [Environment]::NewLine))
}

function Set-StatusText {
    param([object]$Status)
    $coordinator = if ($Status.healthOk) { "运行中" } elseif ($Status.coordinatorPid) { "启动中/未响应" } else { "未启动" }
    $agent = if ($Status.hostId) { "已登录 / heartbeat 可用" } else { "未登录" }
    $private = if ($Status.privateMode) { "私人模式：已启用，仅建议本机/可信局域网使用" } else { "私人模式：未初始化" }

    $lblCoordinator.Text = "控制端状态：$coordinator"
    $lblAgent.Text = "本地主机代理：$agent"
    $lblPrivate.Text = $private
    $lblGroup.Text = "服务器组：$(if ($Status.groupId) { $Status.groupId } else { '-' })"
    $lblHost.Text = "主机：$(if ($Status.hostId) { $Status.hostId } else { '-' })"
    $lblJava.Text = "Java：$(if ($Status.java) { $Status.java } else { '-' })"
    $lblServer.Text = "MC 服务端目录：$(if ($script:ServerDir) { $script:ServerDir } else { '-' })"
}

function Refresh-Status {
    try {
        $json = Invoke-Agent @("desktop", "status", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port, "--json")
        $status = $json | ConvertFrom-Json
        Refresh-ServerConfig
        Set-StatusText $status
        Append-Log "状态已刷新。"
    } catch {
        Append-Log ("状态刷新失败：" + $_.Exception.Message)
    }
}

function Refresh-ServerConfig {
    $configPath = Join-Path $AppDataDir "config.yaml"
    if (-not (Test-Path $configPath)) { return }
    try {
        $config = Get-Content -Raw -Encoding UTF8 -Path $configPath | ConvertFrom-Json
        if ($config.server -and $config.server.dir) {
            $script:ServerDir = $config.server.dir
            try {
                $reportJson = Invoke-Agent @("desktop", "inspect-server", "--server-dir", $script:ServerDir, "--json")
                $report = $reportJson | ConvertFrom-Json
                $lblRCON.Text = "RCON：" + $report.rcon.chineseMessage
            } catch {
                $lblRCON.Text = "RCON：服务端目录检测失败"
            }
        }
    } catch {
        Append-Log ("读取本地配置失败：" + $_.Exception.Message)
    }
}

function Run-Action {
    param(
        [string]$Title,
        [string[]]$Args,
        [switch]$Refresh
    )
    try {
        Append-Log "$Title ..."
        $output = Invoke-Agent $Args
        Append-Log $output
        if ($Refresh) { Refresh-Status }
    } catch {
        Append-Log ("失败：" + $_.Exception.Message)
        [System.Windows.Forms.MessageBox]::Show($_.Exception.Message, "ACBH", "OK", "Error") | Out-Null
    }
}

function Choose-ServerDir {
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = "选择 Minecraft 服务端根目录"
    if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
        $script:ServerDir = $dialog.SelectedPath
        $lblServer.Text = "MC 服务端目录：$script:ServerDir"
        Run-Action "检测服务端目录" @("desktop", "inspect-server", "--server-dir", $script:ServerDir)
    }
}

function Import-ServerDir {
    if (-not $script:ServerDir) {
        Choose-ServerDir
    }
    if (-not $script:ServerDir) { return }
    Run-Action "导入服务端目录" @("desktop", "import-server", "--app-data-dir", $AppDataDir, "--server-dir", $script:ServerDir) -Refresh
}

function Show-AdvancedHint {
    param([string]$Name)
    [System.Windows.Forms.MessageBox]::Show(
        "$Name 在当前 v0.3 pre-release GUI 中暂不可用。请先使用一键启动、服务端导入、启动/停止服务端和 heartbeat；需要该高级能力时请使用命令行或等待后续完整向导。",
        "ACBH 暂不可用",
        "OK",
        "Information"
    ) | Out-Null
}

$form = New-Object System.Windows.Forms.Form
$form.Text = "ACBH 私人本地桌面版"
$form.StartPosition = "CenterScreen"
$form.Size = New-Object System.Drawing.Size(980, 720)
$form.MinimumSize = New-Object System.Drawing.Size(900, 640)
$form.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 9)

$title = New-Object System.Windows.Forms.Label
$title.Text = "ACBH 私人本地桌面版"
$title.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 18, [System.Drawing.FontStyle]::Bold)
$title.Location = New-Object System.Drawing.Point(20, 18)
$title.Size = New-Object System.Drawing.Size(520, 36)
$form.Controls.Add($title)

$lblPrivate = New-Object System.Windows.Forms.Label
$lblPrivate.Location = New-Object System.Drawing.Point(24, 58)
$lblPrivate.Size = New-Object System.Drawing.Size(720, 24)
$lblPrivate.Text = "私人模式：已启用，仅建议本机/可信局域网使用"
$form.Controls.Add($lblPrivate)

$statusPanel = New-Object System.Windows.Forms.Panel
$statusPanel.Location = New-Object System.Drawing.Point(20, 92)
$statusPanel.Size = New-Object System.Drawing.Size(920, 160)
$statusPanel.BorderStyle = "FixedSingle"
$form.Controls.Add($statusPanel)

$labels = @()
foreach ($i in 0..6) {
    $label = New-Object System.Windows.Forms.Label
    $label.Location = New-Object System.Drawing.Point(16, (14 + $i * 20))
    $label.Size = New-Object System.Drawing.Size(880, 18)
    $statusPanel.Controls.Add($label)
    $labels += $label
}
$lblCoordinator = $labels[0]
$lblAgent = $labels[1]
$lblGroup = $labels[2]
$lblHost = $labels[3]
$lblServer = $labels[4]
$lblJava = $labels[5]
$lblRCON = $labels[6]
$lblRCON.Text = "RCON：等待服务端目录检测"

$buttonPanel = New-Object System.Windows.Forms.Panel
$buttonPanel.Location = New-Object System.Drawing.Point(20, 270)
$buttonPanel.Size = New-Object System.Drawing.Size(920, 150)
$form.Controls.Add($buttonPanel)

function Add-Button {
    param([string]$Text, [int]$X, [int]$Y, [scriptblock]$Click)
    $button = New-Object System.Windows.Forms.Button
    $button.Text = $Text
    $button.Location = New-Object System.Drawing.Point($X, $Y)
    $button.Size = New-Object System.Drawing.Size(168, 32)
    $button.Add_Click($Click)
    $buttonPanel.Controls.Add($button)
}

function Add-UnavailableButton {
    param([string]$Text, [int]$X, [int]$Y, [string]$FeatureName)
    $click = { Show-AdvancedHint $FeatureName }.GetNewClosure()
    Add-Button "$Text（暂不可用）" $X $Y $click
}

Add-Button "一键初始化" 0 0 { Run-Action "一键初始化" @("desktop", "start", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port) -Refresh }
Add-Button "一键启动" 184 0 { Run-Action "一键启动" @("desktop", "start", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port) -Refresh }
Add-Button "一键关闭" 368 0 { Run-Action "一键关闭" @("desktop", "stop", "--app-data-dir", $AppDataDir, "--port", $Port) -Refresh }
Add-Button "刷新状态" 552 0 { Refresh-Status }
Add-Button "打开日志目录" 736 0 {
    $logDir = Join-Path $AppDataDir "logs"
    New-Item -ItemType Directory -Force -Path $logDir | Out-Null
    Start-Process $logDir
}

Add-Button "导入 MC 目录" 0 46 { Choose-ServerDir }
Add-Button "保存导入配置" 184 46 { Import-ServerDir }
Add-Button "启动 MC 服务端" 368 46 { Run-Action "启动 MC 服务端" @("server", "start") }
Add-Button "停止 MC 服务端" 552 46 { Run-Action "停止 MC 服务端" @("server", "stop") }
Add-Button "发送 heartbeat" 736 46 { Run-Action "发送 heartbeat" @("heartbeat") -Refresh }

Add-UnavailableButton "启动/停止 daemon" 0 92 "后台心跳服务"
Add-UnavailableButton "scan 服务端包" 184 92 "scan server-pack"
Add-UnavailableButton "safe-sync 世界快照" 368 92 "safe-sync world-snapshot"
Add-UnavailableButton "push / pull 制品" 552 92 "push / pull artifact"
Add-UnavailableButton "takeover 演练" 736 92 "takeover 演练"

$resetButton = New-Object System.Windows.Forms.Button
$resetButton.Text = "重置本地配置"
$resetButton.Location = New-Object System.Drawing.Point(20, 430)
$resetButton.Size = New-Object System.Drawing.Size(168, 32)
$resetButton.Add_Click({
    $answer = [System.Windows.Forms.MessageBox]::Show("重置会删除本地 groupId/accessKey/hostToken，确定继续？", "确认重置", "YesNo", "Warning")
    if ($answer -eq [System.Windows.Forms.DialogResult]::Yes) {
        Run-Action "重置本地配置" @("desktop", "reset", "--app-data-dir", $AppDataDir, "--yes") -Refresh
    }
})
$form.Controls.Add($resetButton)

$logBox = New-Object System.Windows.Forms.TextBox
$logBox.Location = New-Object System.Drawing.Point(20, 472)
$logBox.Size = New-Object System.Drawing.Size(920, 190)
$logBox.Multiline = $true
$logBox.ScrollBars = "Vertical"
$logBox.ReadOnly = $true
$logBox.Font = New-Object System.Drawing.Font("Consolas", 9)
$form.Controls.Add($logBox)

$form.Add_Shown({ Refresh-Status })
[void]$form.ShowDialog()
