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
$script:LastStatus = $null
$script:ToolTip = New-Object System.Windows.Forms.ToolTip

function Protect-Text {
    param([string]$Text)
    if ([string]::IsNullOrEmpty($Text)) { return $Text }
    $safe = $Text
    $safe = [regex]::Replace($safe, '(?i)(accessKey|hostToken|rcon\.password|ACBH_RCON_PASSWORD)(\s*[:=]\s*)[^\s,;}]+', '$1$2[已隐藏]')
    $safe = [regex]::Replace($safe, 'ak_[A-Za-z0-9_\-]+', 'ak_[已隐藏]')
    $safe = [regex]::Replace($safe, 'ht_[A-Za-z0-9_\-]+', 'ht_[已隐藏]')
    return $safe
}

function Invoke-Agent {
    param(
        [string[]]$Args,
        [hashtable]$ExtraEnv
    )
    if (-not (Test-Path $AgentPath)) {
        throw "找不到本地主机代理 Agent：$AgentPath"
    }
    $oldAppData = $env:ACBH_APP_DATA_DIR
    $oldExtra = @{}
    try {
        $env:ACBH_APP_DATA_DIR = $AppDataDir
        if ($ExtraEnv) {
            foreach ($key in $ExtraEnv.Keys) {
                $oldExtra[$key] = [Environment]::GetEnvironmentVariable($key, "Process")
                [Environment]::SetEnvironmentVariable($key, [string]$ExtraEnv[$key], "Process")
            }
        }
        $output = & $AgentPath @Args 2>&1 | Out-String
        $output = Protect-Text $output.Trim()
        if ($LASTEXITCODE -ne 0) {
            throw $output
        }
        return $output
    } finally {
        $env:ACBH_APP_DATA_DIR = $oldAppData
        foreach ($key in $oldExtra.Keys) {
            [Environment]::SetEnvironmentVariable($key, $oldExtra[$key], "Process")
        }
    }
}

function Invoke-AgentJson {
    param(
        [string[]]$Args,
        [hashtable]$ExtraEnv
    )
    $text = Invoke-Agent -Args $Args -ExtraEnv $ExtraEnv
    return ($text | ConvertFrom-Json)
}

function Append-Log {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    $safe = Protect-Text $Text
    $logBox.AppendText(("[" + (Get-Date -Format "HH:mm:ss") + "] " + $safe + [Environment]::NewLine))
    $logBox.SelectionStart = $logBox.TextLength
    $logBox.ScrollToCaret()
}

function Show-ChineseError {
    param([string]$Message)
    $safe = Protect-Text $Message
    [System.Windows.Forms.MessageBox]::Show($safe, "ACBH 操作失败", "OK", "Error") | Out-Null
}

function State-Text {
    param([object]$Value)
    if ($null -eq $Value) { return "未知 unknown" }
    $raw = [string]$Value
    switch ($raw) {
        "running" { return "运行中 running" }
        "stopped" { return "已停止 stopped" }
        "unknown" { return "未知 unknown" }
        "standby" { return "待命 standby" }
        "hosting" { return "正在托管 hosting" }
        "offline" { return "离线 offline" }
        "healthy" { return "健康 healthy" }
        "unhealthy" { return "异常 unhealthy" }
        "enabled" { return "已启用 enabled" }
        "disabled" { return "未启用 disabled" }
        "missing" { return "缺失 missing" }
        "configured" { return "已配置 configured" }
        "not configured" { return "未配置 not configured" }
        "current host" { return "当前主机 current host" }
        "not current host" { return "不是当前主机 not current host" }
        default { return $raw }
    }
}

function Set-StatusText {
    param([object]$Status)
    $script:LastStatus = $Status
    $coordinator = if ($Status.healthOk) { "运行中 running" } elseif ($Status.coordinatorPid) { "异常 unhealthy：已启动但未响应" } else { "未运行 stopped" }
    $agent = if ($Status.hostId) { "已登录：心跳 heartbeat 可用" } else { "未登录：请先一键初始化" }
    $daemon = if ($Status.daemonRunning) { "运行中 running (PID $($Status.daemonPid))" } else { "未运行 stopped" }
    $mcServer = if ($Status.mcServerRunning) { "运行中 running" } else { "未运行 stopped" }
    $currentHost = if ($null -eq $Status.isCurrentHost) { "未知 unknown" } elseif ($Status.isCurrentHost) { "当前主机 current host" } else { "不是当前主机 not current host" }

    $statusLabels[0].Text = "控制端 Coordinator 状态：$coordinator"
    $statusLabels[1].Text = "本地主机代理 Agent 状态：$agent"
    $statusLabels[2].Text = "后台服务 daemon 状态：$daemon"
    $statusLabels[3].Text = "MC 服务端状态：$mcServer"
    $statusLabels[4].Text = "服务器组 Group ID：$(if ($Status.groupId) { $Status.groupId } else { '-' })"
    $statusLabels[5].Text = "本地主机 Host ID：$(if ($Status.hostId) { $Status.hostId } else { '-' })"
    $statusLabels[6].Text = "当前主机 Current Host：$currentHost $(if ($Status.currentHostId) { '(' + $Status.currentHostId + ')' } else { '' })"
    $statusLabels[7].Text = "Java 状态：$(if ($Status.java) { $Status.java } else { '未知 unknown' })"
    $statusLabels[8].Text = "MC 目录：$(if ($Status.mcServerDir) { $Status.mcServerDir } elseif ($script:ServerDir) { $script:ServerDir } else { '-' })"
    $statusLabels[9].Text = "服务端类型：$(if ($Status.mcServerType) { $Status.mcServerType } else { '未知 unknown' })"
    $statusLabels[10].Text = "RCON 状态：$(if ($Status.rconStatus) { $Status.rconStatus } else { '未检测 not configured' })"
    $statusLabels[11].Text = "最近 manifest：$(if ($Status.latestManifestPath) { $Status.latestManifestPath } else { '-' })"
    $statusLabels[12].Text = "最近制品 Artifact ID：$(if ($Status.latestArtifactId) { $Status.latestArtifactKind + ' / ' + $Status.latestArtifactId } else { '-' })"
    $statusLabels[13].Text = "日志目录：$(if ($Status.logDir) { $Status.logDir } else { Join-Path $AppDataDir 'logs' })"
    $statusLabels[14].Text = "数据目录 / 便携模式 portable mode：$AppDataDir"
}

function Refresh-Status {
    try {
        $status = Invoke-AgentJson @("desktop", "status", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port, "--json")
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
        }
    } catch {
        Append-Log ("读取本地配置失败：" + $_.Exception.Message)
    }
}

function Run-ActionAsync {
    param(
        [string]$Title,
        [scriptblock]$Action,
        [switch]$Refresh
    )
    Append-Log "$Title ..."
    $form.UseWaitCursor = $true
    $worker = New-Object System.ComponentModel.BackgroundWorker
    $worker.DoWork += {
        param($sender, $eventArgs)
        try {
            $eventArgs.Result = & $Action
        } catch {
            $eventArgs.Result = $_
        }
    }
    $worker.RunWorkerCompleted += {
        param($sender, $eventArgs)
        $form.UseWaitCursor = $false
        if ($eventArgs.Result -is [System.Management.Automation.ErrorRecord]) {
            Append-Log ("失败：" + $eventArgs.Result.Exception.Message)
            Show-ChineseError $eventArgs.Result.Exception.Message
        } else {
            Append-Log ([string]$eventArgs.Result)
            if ($Refresh) { Refresh-Status }
        }
    }
    $worker.RunWorkerAsync()
}

function Choose-ServerDir {
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = "选择 Minecraft 服务端根目录"
    if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
        $script:ServerDir = $dialog.SelectedPath
        $statusLabels[8].Text = "MC 目录：$script:ServerDir"
        Run-ActionAsync "检测服务端目录" { Invoke-Agent @("desktop", "inspect-server", "--server-dir", $script:ServerDir) }
    }
}

function Import-ServerDir {
    if (-not $script:ServerDir) {
        Choose-ServerDir
    }
    if (-not $script:ServerDir) { return }
    Run-ActionAsync "保存导入配置" {
        Invoke-Agent @("desktop", "import-server", "--app-data-dir", $AppDataDir, "--server-dir", $script:ServerDir)
    } -Refresh
}

function Prompt-Secret {
    param([string]$Title, [string]$Label)
    $dialog = New-Object System.Windows.Forms.Form
    $dialog.Text = $Title
    $dialog.StartPosition = "CenterParent"
    $dialog.Size = New-Object System.Drawing.Size(460, 160)
    $dialog.FormBorderStyle = "FixedDialog"
    $dialog.MaximizeBox = $false
    $dialog.MinimizeBox = $false

    $lbl = New-Object System.Windows.Forms.Label
    $lbl.Text = $Label
    $lbl.Location = New-Object System.Drawing.Point(16, 16)
    $lbl.Size = New-Object System.Drawing.Size(410, 24)
    $dialog.Controls.Add($lbl)

    $box = New-Object System.Windows.Forms.TextBox
    $box.Location = New-Object System.Drawing.Point(16, 46)
    $box.Size = New-Object System.Drawing.Size(410, 24)
    $box.UseSystemPasswordChar = $true
    $dialog.Controls.Add($box)

    $ok = New-Object System.Windows.Forms.Button
    $ok.Text = "确定"
    $ok.Location = New-Object System.Drawing.Point(246, 82)
    $ok.DialogResult = [System.Windows.Forms.DialogResult]::OK
    $dialog.Controls.Add($ok)

    $cancel = New-Object System.Windows.Forms.Button
    $cancel.Text = "取消"
    $cancel.Location = New-Object System.Drawing.Point(334, 82)
    $cancel.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
    $dialog.Controls.Add($cancel)
    $dialog.AcceptButton = $ok
    $dialog.CancelButton = $cancel

    if ($dialog.ShowDialog($form) -eq [System.Windows.Forms.DialogResult]::OK) {
        return $box.Text
    }
    return $null
}

function Refresh-Logs {
    $logDir = Join-Path $AppDataDir "logs"
    if (-not (Test-Path $logDir)) {
        Append-Log "日志目录还不存在：$logDir"
        return
    }
    $latest = Get-ChildItem -Path $logDir -File -Recurse -ErrorAction SilentlyContinue |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
    if (-not $latest) {
        Append-Log "日志目录中还没有日志文件。"
        return
    }
    $lines = Get-Content -Path $latest.FullName -Tail 100 -ErrorAction SilentlyContinue
    Append-Log "读取最新日志：$($latest.FullName)"
    Append-Log (($lines | ForEach-Object { Protect-Text $_ }) -join [Environment]::NewLine)
}

function Open-LogDir {
    $logDir = Join-Path $AppDataDir "logs"
    New-Item -ItemType Directory -Force -Path $logDir | Out-Null
    Start-Process $logDir
}

function Add-Button {
    param([string]$Text, [int]$X, [int]$Y, [scriptblock]$Click, [string]$Tip = "")
    $button = New-Object System.Windows.Forms.Button
    $button.Text = $Text
    $button.Location = New-Object System.Drawing.Point($X, $Y)
    $button.Size = New-Object System.Drawing.Size(202, 34)
    $button.Add_Click($Click)
    if ($Tip) { $script:ToolTip.SetToolTip($button, $Tip) }
    $buttonPanel.Controls.Add($button)
    return $button
}

$form = New-Object System.Windows.Forms.Form
$form.Text = "ACBH 私人本地桌面版"
$form.StartPosition = "CenterScreen"
$form.Size = New-Object System.Drawing.Size(1130, 860)
$form.MinimumSize = New-Object System.Drawing.Size(1060, 760)
$form.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 9)

$title = New-Object System.Windows.Forms.Label
$title.Text = "ACBH 私人本地桌面版"
$title.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 18, [System.Drawing.FontStyle]::Bold)
$title.Location = New-Object System.Drawing.Point(20, 18)
$title.Size = New-Object System.Drawing.Size(560, 36)
$form.Controls.Add($title)

$lblPrivate = New-Object System.Windows.Forms.Label
$lblPrivate.Location = New-Object System.Drawing.Point(24, 58)
$lblPrivate.Size = New-Object System.Drawing.Size(920, 24)
$lblPrivate.Text = "私人模式：默认绑定 127.0.0.1，仅建议本机/可信局域网使用。"
$form.Controls.Add($lblPrivate)

$statusPanel = New-Object System.Windows.Forms.Panel
$statusPanel.Location = New-Object System.Drawing.Point(20, 92)
$statusPanel.Size = New-Object System.Drawing.Size(1070, 322)
$statusPanel.BorderStyle = "FixedSingle"
$form.Controls.Add($statusPanel)

$statusLabels = @()
foreach ($i in 0..14) {
    $label = New-Object System.Windows.Forms.Label
    $label.Location = New-Object System.Drawing.Point(16, (12 + $i * 20))
    $label.Size = New-Object System.Drawing.Size(1036, 18)
    $statusPanel.Controls.Add($label)
    $statusLabels += $label
}

$buttonPanel = New-Object System.Windows.Forms.Panel
$buttonPanel.Location = New-Object System.Drawing.Point(20, 430)
$buttonPanel.Size = New-Object System.Drawing.Size(1070, 154)
$form.Controls.Add($buttonPanel)

Add-Button "一键初始化" 0 0 { Run-ActionAsync "一键初始化" { Invoke-Agent @("desktop", "start", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port) } -Refresh } "启动控制端 Coordinator 并注册本地主机 Agent。"
Add-Button "一键启动" 214 0 { Run-ActionAsync "一键启动" { Invoke-Agent @("desktop", "start", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port) } -Refresh } "复用或启动私人本地控制端 Coordinator。"
Add-Button "一键关闭" 428 0 { Run-ActionAsync "一键关闭" { Invoke-Agent @("desktop", "stop", "--app-data-dir", $AppDataDir, "--port", $Port) } -Refresh } "关闭由桌面版启动的控制端 Coordinator。"
Add-Button "刷新状态" 642 0 { Refresh-Status } "刷新控制端、Agent、daemon、MC、RCON 和 manifest 状态。"
Add-Button "打开日志目录" 856 0 { Open-LogDir } "打开 ACBH logs 日志目录。"

Add-Button "导入 MC 服务端目录" 0 40 { Choose-ServerDir } "选择 Minecraft 服务端根目录。"
Add-Button "保存导入配置" 214 40 { Import-ServerDir } "保存 serverDir 和建议启动命令。"
Add-Button "启动 MC 服务端" 428 40 { Run-ActionAsync "启动 MC 服务端" { Invoke-Agent @("server", "start") } -Refresh } "使用导入时保存的 command 启动 MC 服务端。"
Add-Button "停止 MC 服务端" 642 40 { Run-ActionAsync "停止 MC 服务端" { Invoke-Agent @("server", "stop") } -Refresh } "仅停止由 ACBH 启动的 MC 服务端进程。"
Add-Button "发送心跳 heartbeat" 856 40 { Run-ActionAsync "发送心跳 heartbeat" { Invoke-Agent @("heartbeat") } -Refresh } "heartbeat：告诉控制端这台电脑仍在线。"

Add-Button "启动后台服务 daemon" 0 80 { Run-ActionAsync "启动后台服务 daemon" { Invoke-Agent @("desktop", "daemon", "start", "--app-data-dir", $AppDataDir) } -Refresh } "后台心跳服务：持续向控制端上报本机状态。"
Add-Button "停止后台服务 daemon" 214 80 { Run-ActionAsync "停止后台服务 daemon" { Invoke-Agent @("desktop", "daemon", "stop", "--app-data-dir", $AppDataDir) } -Refresh } "停止由 ACBH desktop 启动的 daemon，不误杀其他进程。"
Add-Button "扫描服务端包 scan" 428 80 { Run-ActionAsync "扫描服务端包 scan server-pack" { Invoke-Agent @("desktop", "scan-pack", "--app-data-dir", $AppDataDir) } -Refresh } "扫描服务端包：生成 mods/config/jar 等服务端文件清单。"
Add-Button "安全同步世界快照" 642 80 {
    $password = Prompt-Secret "RCON 密码" "请输入 RCON password；不会显示、记录或作为命令行参数传递。"
    if ($null -eq $password) { return }
    Run-ActionAsync "安全同步世界快照 safe-sync" {
        Invoke-Agent -Args @("desktop", "safe-sync-world", "--app-data-dir", $AppDataDir) -ExtraEnv @{ ACBH_RCON_PASSWORD = $password }
    } -Refresh
} "安全同步世界快照：通过 RCON 保存世界后生成 world manifest。"
Add-Button "上传同步制品 push" 856 80 { Run-ActionAsync "上传同步制品 push" { Invoke-Agent @("desktop", "push-latest", "--app-data-dir", $AppDataDir) } -Refresh } "上传同步制品：把最近生成的 manifest 对应文件上传到控制端。"

Add-Button "拉取同步制品 pull" 0 120 {
    $warning = "pull 可能覆盖本地服务端文件，请确认已备份。默认不应用删除。是否继续拉取最新 world-snapshot？"
    $answer = [System.Windows.Forms.MessageBox]::Show($warning, "确认 pull", "YesNo", "Warning")
    if ($answer -ne [System.Windows.Forms.DialogResult]::Yes) { return }
    Run-ActionAsync "拉取同步制品 pull" { Invoke-Agent @("desktop", "pull-latest", "--app-data-dir", $AppDataDir, "--artifact-kind", "world-snapshot", "--artifact-id", "latest") } -Refresh
} "拉取同步制品：从控制端下载指定制品，可能覆盖本地文件。"
Add-Button "接管演练 takeover" 214 120 { Run-ActionAsync "接管演练 takeover" { Invoke-Agent @("desktop", "takeover-status", "--app-data-dir", $AppDataDir) } -Refresh } "接管演练：检查当前主机是否可接管，不满足条件时不会执行危险操作。"
Add-Button "检查远程控制 RCON" 428 120 { Run-ActionAsync "检查远程控制 RCON" { Invoke-Agent @("desktop", "rcon-status", "--app-data-dir", $AppDataDir) } -Refresh } "RCON 是 Minecraft 服务端远程控制接口，safe-sync 需要它执行 save-all flush。"
Add-Button "刷新日志" 642 120 { Refresh-Logs } "读取 ACBH logs 目录内最新日志文件的最近 100 行。"
Add-Button "清空 GUI 显示" 856 120 { $logBox.Clear(); Append-Log "已清空 GUI 日志显示；真实日志文件未删除。" } "只清空当前窗口显示，不删除真实日志文件。"

$resetButton = New-Object System.Windows.Forms.Button
$resetButton.Text = "重置本地配置"
$resetButton.Location = New-Object System.Drawing.Point(20, 596)
$resetButton.Size = New-Object System.Drawing.Size(202, 34)
$script:ToolTip.SetToolTip($resetButton, "删除本地 groupId/accessKey/hostToken；不会删除真实 MC 服务端目录。")
$resetButton.Add_Click({
    $answer = [System.Windows.Forms.MessageBox]::Show("重置会删除本地 Group ID、accessKey 和 hostToken，确定继续？", "确认重置", "YesNo", "Warning")
    if ($answer -eq [System.Windows.Forms.DialogResult]::Yes) {
        Run-ActionAsync "重置本地配置" { Invoke-Agent @("desktop", "reset", "--app-data-dir", $AppDataDir, "--yes") } -Refresh
    }
})
$form.Controls.Add($resetButton)

$help = New-Object System.Windows.Forms.TextBox
$help.Location = New-Object System.Drawing.Point(236, 596)
$help.Size = New-Object System.Drawing.Size(854, 86)
$help.Multiline = $true
$help.ReadOnly = $true
$help.ScrollBars = "Vertical"
$help.Text = @"
术语说明：
Coordinator = 控制端，负责服务器组、主机状态、同步制品和接管流程。
Agent = 本地主机代理，运行在你的电脑上，负责心跳、启动 MC、同步和上传。
heartbeat = 心跳，用来告诉控制端这台电脑还在线。
daemon = 后台心跳服务，用来持续上报状态。
safe-sync = 安全同步，会先通过 RCON 保存世界，再生成世界快照。
push = 上传同步制品到控制端。pull = 从控制端拉取同步制品到本地。takeover = 接管演练。
"@
$form.Controls.Add($help)

$logBox = New-Object System.Windows.Forms.TextBox
$logBox.Location = New-Object System.Drawing.Point(20, 694)
$logBox.Size = New-Object System.Drawing.Size(1070, 118)
$logBox.Multiline = $true
$logBox.ScrollBars = "Vertical"
$logBox.ReadOnly = $true
$logBox.Font = New-Object System.Drawing.Font("Consolas", 9)
$form.Controls.Add($logBox)

$form.Add_FormClosing({
    try {
        $serverStatus = Invoke-AgentJson @("server", "status")
        if ($serverStatus.running) {
            $answer = [System.Windows.Forms.MessageBox]::Show("检测到由 ACBH 启动的 MC 服务端仍在运行。是否现在停止？", "关闭前确认", "YesNo", "Warning")
            if ($answer -eq [System.Windows.Forms.DialogResult]::Yes) {
                Invoke-Agent @("server", "stop") | Out-Null
            }
        }
    } catch {
        # 关闭窗口时不阻塞用户。
    }
})

$form.Add_Shown({ Refresh-Status; Refresh-Logs })
[void]$form.ShowDialog()
