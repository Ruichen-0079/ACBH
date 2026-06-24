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
if (-not $AppDataDir) {
    $bundleRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
    if (Test-Path (Join-Path $bundleRoot "portable.flag")) {
        $AppDataDir = Join-Path $bundleRoot "data"
    } else {
        $AppDataDir = Join-Path $env:APPDATA "ACBH"
    }
}

if ($SelfTest) {
    Write-Output "ACBH v0.3.5 hotfix panel self-test ok"
    exit 0
}

function Quote-ProcessArgument {
    param([string]$Arg)
    if ($null -eq $Arg -or $Arg.Length -eq 0) { return '""' }
    if ($Arg -notmatch '[\s"]') { return $Arg }
    return '"' + ($Arg -replace '(\\*)"', '$1$1\"' -replace '(\\+)$', '$1$1') + '"'
}

function Join-ProcessArguments {
    param([string[]]$CommandArgs)
    return (($CommandArgs | ForEach-Object { Quote-ProcessArgument ([string]$_) }) -join " ")
}

function Invoke-AgentRaw {
    param([string[]]$CommandArgs)
    if (-not (Test-Path $AgentPath)) {
        throw "找不到 ACBH Agent：$AgentPath"
    }
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $AgentPath
    $psi.Arguments = Join-ProcessArguments $CommandArgs
    $psi.WorkingDirectory = Split-Path -Parent $AgentPath
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $process = [System.Diagnostics.Process]::Start($psi)
    $stdout = $process.StandardOutput.ReadToEnd()
    $stderr = $process.StandardError.ReadToEnd()
    $process.WaitForExit()
    return [pscustomobject]@{
        ExitCode = $process.ExitCode
        Stdout = $stdout.Trim()
        Stderr = $stderr.Trim()
        Output = (($stdout, $stderr) -join [Environment]::NewLine).Trim()
    }
}

function Invoke-AgentJson {
    param([string[]]$CommandArgs, [switch]$AllowFailure)
    $raw = Invoke-AgentRaw $CommandArgs
    $text = if ($raw.Stdout) { $raw.Stdout } else { $raw.Output }
    $parsed = $null
    if ($text -and ($text.Trim().StartsWith("{") -or $text.Trim().StartsWith("["))) {
        try { $parsed = $text | ConvertFrom-Json } catch { }
    }
    if ($raw.ExitCode -ne 0 -and -not $AllowFailure) {
        throw $(if ($raw.Stderr) { $raw.Stderr } else { $raw.Output })
    }
    return [pscustomobject]@{ Raw = $raw; Json = $parsed }
}

function Add-Log {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    $safe = $Text
    $safe = [regex]::Replace($safe, 'ak_[A-Za-z0-9_\-]+', 'ak_[已隐藏]')
    $safe = [regex]::Replace($safe, 'ht_[A-Za-z0-9_\-]+', 'ht_[已隐藏]')
    $txtLog.AppendText("[$(Get-Date -Format HH:mm:ss)] $safe$([Environment]::NewLine)")
    $txtLog.SelectionStart = $txtLog.TextLength
    $txtLog.ScrollToCaret()
}

function Show-Error {
    param([string]$Text)
    Add-Log $Text
    [System.Windows.Forms.MessageBox]::Show($Text, "ACBH v0.3.5 Hotfix", "OK", "Error") | Out-Null
}

function Read-AgentConfig {
    $path = Join-Path $AppDataDir "config.yaml"
    if (-not (Test-Path $path)) { return $null }
    try { return (Get-Content $path -Raw -Encoding UTF8 | ConvertFrom-Json) } catch { return $null }
}

function Set-StatusText {
    param([string]$Text, [System.Drawing.Color]$Color)
    $lblOverall.Text = $Text
    $lblOverall.ForeColor = $Color
}

function Refresh-HotfixStatus {
    try {
        $cfg = Read-AgentConfig
        if ($null -eq $cfg) {
            Set-StatusText "未配置：请先在原向导创建或加入 Group" ([System.Drawing.Color]::Firebrick)
            return
        }

        $txtCoordinator.Text = [string]$cfg.coordinatorUrl
        $txtGroupId.Text = [string]$cfg.groupId
        $txtMemberId.Text = [string]$cfg.memberId
        $txtHostId.Text = [string]$cfg.hostId
        $txtServerDir.Text = [string]$cfg.server.dir

        $canPush = Invoke-AgentJson @("desktop", "can-push", "--app-data-dir", $AppDataDir, "--json") -AllowFailure
        if ($canPush.Json) {
            $txtCurrentHost.Text = [string]$canPush.Json.currentHostId
            $lblLease.Text = if ($canPush.Json.canPush) { "Current Host：本机（generation $($canPush.Json.currentHostGeneration)）" } else { "Current Host：$($canPush.Json.reason)" }
            $lblLease.ForeColor = if ($canPush.Json.canPush) { [System.Drawing.Color]::DarkGreen } else { [System.Drawing.Color]::DarkOrange }
        } else {
            $txtCurrentHost.Text = ""
            $lblLease.Text = "Current Host：无法读取"
            $lblLease.ForeColor = [System.Drawing.Color]::Firebrick
        }

        $relay = Invoke-AgentJson @("desktop", "relay", "status", "--app-data-dir", $AppDataDir, "--json") -AllowFailure
        $server = Invoke-AgentJson @("desktop", "server", "status", "--app-data-dir", $AppDataDir, "--json") -AllowFailure
        $relayRunning = $relay.Json -and $relay.Json.running
        $serverRunning = $server.Json -and $server.Json.state -eq "Running"
        $lblServer.Text = "Minecraft：" + $(if ($serverRunning) { "运行中" } else { "已停止" })
        $lblRelay.Text = "公网中转：" + $(if ($relayRunning) { "运行中" } else { "未运行" })
        $lblServer.ForeColor = if ($serverRunning) { [System.Drawing.Color]::DarkGreen } else { [System.Drawing.Color]::DimGray }
        $lblRelay.ForeColor = if ($relayRunning) { [System.Drawing.Color]::DarkGreen } else { [System.Drawing.Color]::DarkOrange }

        if ($serverRunning -and $relayRunning -and $canPush.Json.canPush) {
            Set-StatusText "公网开服链路正常" ([System.Drawing.Color]::DarkGreen)
        } elseif ($serverRunning) {
            Set-StatusText "Minecraft 已运行，但公网链路尚未完成" ([System.Drawing.Color]::DarkOrange)
        } else {
            Set-StatusText "已配置，等待启动" ([System.Drawing.Color]::DimGray)
        }
    } catch {
        Show-Error "刷新状态失败：$($_.Exception.Message)"
    }
}

function Generate-Invite {
    try {
        $result = Invoke-AgentJson @("desktop", "setup", "create-invite", "--app-data-dir", $AppDataDir, "--expires-seconds", "1800", "--one-time", "--json")
        if (-not $result.Json -or -not $result.Json.ok) {
            throw $(if ($result.Json.message) { $result.Json.message } else { $result.Raw.Output })
        }
        $txtInviteCode.Text = [string]$result.Json.inviteCode
        $lblInviteExpiry.Text = "有效期至：$($result.Json.expiresAt)"
        [System.Windows.Forms.Clipboard]::SetText($txtInviteCode.Text)
        Add-Log "邀请码已生成并复制到剪贴板。"
    } catch {
        Show-Error "生成邀请码失败：$($_.Exception.Message)"
    }
}

function List-Invites {
    try {
        $result = Invoke-AgentJson @("desktop", "setup", "list-invites", "--app-data-dir", $AppDataDir, "--json")
        if (-not $result.Json -or -not $result.Json.ok) {
            throw $(if ($result.Json.message) { $result.Json.message } else { $result.Raw.Output })
        }
        $lines = @()
        foreach ($item in $result.Json.invites) {
            $state = if ($item.revokedAt) { "已撤销" } elseif ($item.usedAt) { "已使用" } else { "可用" }
            $lines += "$($item.inviteId) | $state | expires $($item.expiresAt)"
        }
        if ($lines.Count -eq 0) { $lines = @("暂无邀请码。") }
        [System.Windows.Forms.MessageBox]::Show(($lines -join [Environment]::NewLine), "邀请码", "OK", "Information") | Out-Null
    } catch {
        Show-Error "读取邀请码失败：$($_.Exception.Message)"
    }
}

function Pull-LatestArtifact {
    param([string]$Kind, [string]$DisplayName)
    try {
        $server = Invoke-AgentJson @("desktop", "server", "status", "--app-data-dir", $AppDataDir, "--json") -AllowFailure
        if ($server.Json -and $server.Json.state -eq "Running") {
            $choice = [System.Windows.Forms.MessageBox]::Show("Minecraft 正在运行。为避免覆盖正在使用的文件，请先停服。现在停止服务器吗？", "拉取 $DisplayName", "YesNo", "Warning")
            if ($choice -ne [System.Windows.Forms.DialogResult]::Yes) { return }
            $stop = Invoke-AgentJson @("desktop", "server", "stop-auto", "--app-data-dir", $AppDataDir, "--json") -AllowFailure
            Add-Log $(if ($stop.Json.message) { $stop.Json.message } else { $stop.Raw.Output })
        }
        Add-Log "正在从公网服务器拉取最新 $DisplayName……"
        $result = Invoke-AgentJson @("desktop", "pull-latest", "--app-data-dir", $AppDataDir, "--artifact-kind", $Kind, "--artifact-id", "latest", "--json")
        Add-Log $(if ($result.Raw.Stdout) { $result.Raw.Stdout } else { "$DisplayName 拉取完成。" })
        [System.Windows.Forms.MessageBox]::Show("最新 $DisplayName 已拉取到本地。默认未删除本地独有文件。", "ACBH", "OK", "Information") | Out-Null
        Refresh-HotfixStatus
    } catch {
        Show-Error "拉取 $DisplayName 失败：$($_.Exception.Message)"
    }
}

function Start-RelayDetached {
    try {
        $check = Invoke-AgentJson @("desktop", "can-push", "--app-data-dir", $AppDataDir, "--json") -AllowFailure
        if (-not $check.Json -or -not $check.Json.canPush) {
            throw $(if ($check.Json.reason) { $check.Json.reason } else { "本机尚未成为 current host。" })
        }
        $args = @("desktop", "relay", "start-host", "--app-data-dir", $AppDataDir, "--target-address", "127.0.0.1:25565", "--json")
        $psi = New-Object System.Diagnostics.ProcessStartInfo
        $psi.FileName = $AgentPath
        $psi.Arguments = Join-ProcessArguments $args
        $psi.WorkingDirectory = Split-Path -Parent $AgentPath
        $psi.UseShellExecute = $false
        $psi.CreateNoWindow = $true
        [void][System.Diagnostics.Process]::Start($psi)
        Start-Sleep -Seconds 2
        $relay = Invoke-AgentJson @("desktop", "relay", "status", "--app-data-dir", $AppDataDir, "--json") -AllowFailure
        if (-not $relay.Json -or -not $relay.Json.running) {
            throw "Relay 进程未保持运行，请查看 relay-host.log。"
        }
        Add-Log "公网中转已启动。"
        Refresh-HotfixStatus
    } catch {
        Show-Error "启动公网中转失败：$($_.Exception.Message)"
    }
}

function Repair-Lease {
    try {
        Add-Log "正在重建 standby 心跳并检查 current host……"
        [void](Invoke-AgentJson @("desktop", "daemon", "stop", "--app-data-dir", $AppDataDir, "--json") -AllowFailure)
        [void](Invoke-AgentJson @("desktop", "daemon", "start", "--app-data-dir", $AppDataDir, "--json") -AllowFailure)
        Start-Sleep -Seconds 3
        $election = Invoke-AgentRaw @("election", "check-timeout")
        if ($election.Output) { Add-Log $election.Output }
        Start-Sleep -Seconds 2
        Refresh-HotfixStatus
    } catch {
        Show-Error "修复主机资格失败：$($_.Exception.Message)"
    }
}

function Start-HotfixServer {
    try {
        Add-Log "开始 hotfix 一键开服事务……"
        $start = Invoke-AgentJson @("desktop", "server", "start-auto", "--app-data-dir", $AppDataDir, "--json") -AllowFailure
        if ($start.Json.steps) { $start.Json.steps | ForEach-Object { Add-Log $_ } }
        if ($start.Json.warnings) { $start.Json.warnings | ForEach-Object { Add-Log "提示：$_" } }
        if (-not $start.Json -or -not $start.Json.ok) {
            throw $(if ($start.Json.message) { $start.Json.message } else { $start.Raw.Output })
        }
        Start-Sleep -Seconds 2
        $check = Invoke-AgentJson @("desktop", "can-push", "--app-data-dir", $AppDataDir, "--json") -AllowFailure
        if (-not $check.Json.canPush) {
            Repair-Lease
            $check = Invoke-AgentJson @("desktop", "can-push", "--app-data-dir", $AppDataDir, "--json") -AllowFailure
        }
        if ($check.Json.canPush) {
            Start-RelayDetached
        } else {
            Add-Log "Minecraft 已启动，但 current host 尚未就绪：$($check.Json.reason)"
        }
        Refresh-HotfixStatus
    } catch {
        Show-Error "一键开服失败：$($_.Exception.Message)"
    }
}

$form = New-Object System.Windows.Forms.Form
$form.Text = "ACBH v0.3.5 Hotfix"
$form.StartPosition = "CenterScreen"
$form.Size = New-Object System.Drawing.Size(980, 760)
$form.MinimumSize = New-Object System.Drawing.Size(900, 700)
$form.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 9)

$title = New-Object System.Windows.Forms.Label
$title.Text = "ACBH v0.3.5 Hotfix Panel"
$title.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 18, [System.Drawing.FontStyle]::Bold)
$title.Location = New-Object System.Drawing.Point(20, 16)
$title.Size = New-Object System.Drawing.Size(500, 34)
$form.Controls.Add($title)

$lblOverall = New-Object System.Windows.Forms.Label
$lblOverall.Text = "正在读取状态……"
$lblOverall.Location = New-Object System.Drawing.Point(560, 24)
$lblOverall.Size = New-Object System.Drawing.Size(380, 24)
$form.Controls.Add($lblOverall)

$identity = New-Object System.Windows.Forms.GroupBox
$identity.Text = "服务器组与本机身份"
$identity.Location = New-Object System.Drawing.Point(20, 60)
$identity.Size = New-Object System.Drawing.Size(920, 220)
$form.Controls.Add($identity)

function Add-Field {
    param([string]$Label, [int]$Y)
    $lbl = New-Object System.Windows.Forms.Label
    $lbl.Text = $Label
    $lbl.Location = New-Object System.Drawing.Point(16, $Y + 3)
    $lbl.Size = New-Object System.Drawing.Size(110, 22)
    $identity.Controls.Add($lbl)
    $txt = New-Object System.Windows.Forms.TextBox
    $txt.Location = New-Object System.Drawing.Point(130, $Y)
    $txt.Size = New-Object System.Drawing.Size(760, 24)
    $txt.ReadOnly = $true
    $identity.Controls.Add($txt)
    return $txt
}

$txtCoordinator = Add-Field "Coordinator" 26
$txtGroupId = Add-Field "Group ID" 58
$txtMemberId = Add-Field "Member ID" 90
$txtHostId = Add-Field "Host ID" 122
$txtCurrentHost = Add-Field "Current Host" 154
$lblLease = New-Object System.Windows.Forms.Label
$lblLease.Location = New-Object System.Drawing.Point(130, 184)
$lblLease.Size = New-Object System.Drawing.Size(620, 22)
$identity.Controls.Add($lblLease)
$btnRefresh = New-Object System.Windows.Forms.Button
$btnRefresh.Text = "刷新状态"
$btnRefresh.Location = New-Object System.Drawing.Point(760, 180)
$btnRefresh.Size = New-Object System.Drawing.Size(130, 28)
$btnRefresh.Add_Click({ Refresh-HotfixStatus })
$identity.Controls.Add($btnRefresh)

$invitePanel = New-Object System.Windows.Forms.GroupBox
$invitePanel.Text = "组员邀请"
$invitePanel.Location = New-Object System.Drawing.Point(20, 290)
$invitePanel.Size = New-Object System.Drawing.Size(450, 160)
$form.Controls.Add($invitePanel)
$txtInviteCode = New-Object System.Windows.Forms.TextBox
$txtInviteCode.Location = New-Object System.Drawing.Point(18, 34)
$txtInviteCode.Size = New-Object System.Drawing.Size(410, 26)
$txtInviteCode.ReadOnly = $true
$invitePanel.Controls.Add($txtInviteCode)
$lblInviteExpiry = New-Object System.Windows.Forms.Label
$lblInviteExpiry.Location = New-Object System.Drawing.Point(18, 68)
$lblInviteExpiry.Size = New-Object System.Drawing.Size(410, 22)
$invitePanel.Controls.Add($lblInviteExpiry)
$btnInvite = New-Object System.Windows.Forms.Button
$btnInvite.Text = "随时生成邀请码"
$btnInvite.Location = New-Object System.Drawing.Point(18, 104)
$btnInvite.Size = New-Object System.Drawing.Size(190, 32)
$btnInvite.Add_Click({ Generate-Invite })
$invitePanel.Controls.Add($btnInvite)
$btnListInvite = New-Object System.Windows.Forms.Button
$btnListInvite.Text = "查看邀请码记录"
$btnListInvite.Location = New-Object System.Drawing.Point(238, 104)
$btnListInvite.Size = New-Object System.Drawing.Size(190, 32)
$btnListInvite.Add_Click({ List-Invites })
$invitePanel.Controls.Add($btnListInvite)

$syncPanel = New-Object System.Windows.Forms.GroupBox
$syncPanel.Text = "公网服务器制品同步"
$syncPanel.Location = New-Object System.Drawing.Point(490, 290)
$syncPanel.Size = New-Object System.Drawing.Size(450, 160)
$form.Controls.Add($syncPanel)
$txtServerDir = New-Object System.Windows.Forms.TextBox
$txtServerDir.Location = New-Object System.Drawing.Point(18, 34)
$txtServerDir.Size = New-Object System.Drawing.Size(410, 26)
$txtServerDir.ReadOnly = $true
$syncPanel.Controls.Add($txtServerDir)
$syncHint = New-Object System.Windows.Forms.Label
$syncHint.Text = "默认安全拉取：不会删除本地独有文件。"
$syncHint.Location = New-Object System.Drawing.Point(18, 68)
$syncHint.Size = New-Object System.Drawing.Size(410, 22)
$syncPanel.Controls.Add($syncHint)
$btnPullServer = New-Object System.Windows.Forms.Button
$btnPullServer.Text = "拉取最新服务器文件"
$btnPullServer.Location = New-Object System.Drawing.Point(18, 104)
$btnPullServer.Size = New-Object System.Drawing.Size(190, 32)
$btnPullServer.Add_Click({ Pull-LatestArtifact "server-pack" "服务器文件" })
$syncPanel.Controls.Add($btnPullServer)
$btnPullWorld = New-Object System.Windows.Forms.Button
$btnPullWorld.Text = "拉取最新世界存档"
$btnPullWorld.Location = New-Object System.Drawing.Point(238, 104)
$btnPullWorld.Size = New-Object System.Drawing.Size(190, 32)
$btnPullWorld.Add_Click({ Pull-LatestArtifact "world-snapshot" "世界存档" })
$syncPanel.Controls.Add($btnPullWorld)

$runPanel = New-Object System.Windows.Forms.GroupBox
$runPanel.Text = "运行与公网中转"
$runPanel.Location = New-Object System.Drawing.Point(20, 460)
$runPanel.Size = New-Object System.Drawing.Size(920, 100)
$form.Controls.Add($runPanel)
$lblServer = New-Object System.Windows.Forms.Label
$lblServer.Location = New-Object System.Drawing.Point(18, 28)
$lblServer.Size = New-Object System.Drawing.Size(220, 22)
$runPanel.Controls.Add($lblServer)
$lblRelay = New-Object System.Windows.Forms.Label
$lblRelay.Location = New-Object System.Drawing.Point(18, 56)
$lblRelay.Size = New-Object System.Drawing.Size(220, 22)
$runPanel.Controls.Add($lblRelay)
$btnStart = New-Object System.Windows.Forms.Button
$btnStart.Text = "Hotfix 一键开服"
$btnStart.Location = New-Object System.Drawing.Point(260, 34)
$btnStart.Size = New-Object System.Drawing.Size(190, 38)
$btnStart.Add_Click({ Start-HotfixServer })
$runPanel.Controls.Add($btnStart)
$btnRepair = New-Object System.Windows.Forms.Button
$btnRepair.Text = "修复 Current Host"
$btnRepair.Location = New-Object System.Drawing.Point(470, 34)
$btnRepair.Size = New-Object System.Drawing.Size(190, 38)
$btnRepair.Add_Click({ Repair-Lease })
$runPanel.Controls.Add($btnRepair)
$btnRelay = New-Object System.Windows.Forms.Button
$btnRelay.Text = "启动公网中转"
$btnRelay.Location = New-Object System.Drawing.Point(680, 34)
$btnRelay.Size = New-Object System.Drawing.Size(190, 38)
$btnRelay.Add_Click({ Start-RelayDetached })
$runPanel.Controls.Add($btnRelay)

$txtLog = New-Object System.Windows.Forms.TextBox
$txtLog.Location = New-Object System.Drawing.Point(20, 575)
$txtLog.Size = New-Object System.Drawing.Size(920, 125)
$txtLog.Multiline = $true
$txtLog.ScrollBars = "Vertical"
$txtLog.ReadOnly = $true
$form.Controls.Add($txtLog)

$form.Add_Shown({
    Add-Log "v0.3.5 hotfix 面板已启动。"
    Refresh-HotfixStatus
})

[void]$form.ShowDialog()
