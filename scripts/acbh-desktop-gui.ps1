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

$script:ToolTip = New-Object System.Windows.Forms.ToolTip
$script:LastStatus = $null
$script:LastInspectResult = $null
$script:LastWorldBackupStatus = $null
$script:Running = $false
$script:CurrentState = "Unconfigured"
$script:GuiOperationState = "Idle"
$script:GuiOperationName = ""
$script:GuiOperationGeneration = 0
$script:GuiActiveOperationId = $null
$script:GuiActiveThreadJob = $null
$script:GuiLogBuffer = New-Object System.Collections.Generic.List[string]
$script:GuiLogFlushScheduled = $false
$script:GuiMutexOperations = @("server", "backup", "restore")
$script:GuiActiveMutex = $null
$script:GuiOperationButtons = New-Object System.Collections.Generic.List[System.Windows.Forms.Button]
$script:GuiCancelSource = $null
$script:GuiSelfTestFailures = 0
$script:DesktopGroupContext = $null
$script:LastCreatedInvite = $null
$script:InviteListGeneration = 0
$script:InviteManagerForm = $null
$script:InviteListView = $null
$script:PendingInviteCode = $null

function Initialize-GuiAsyncHost {
    if (-not (Get-Command Start-ThreadJob -ErrorAction SilentlyContinue)) {
        throw "PowerShell 7+ Start-ThreadJob is required for ACBH desktop GUI async operations."
    }
}

function Invoke-OnUiThread {
    param([scriptblock]$Action)
    if ($null -eq $form) {
        & $Action
        return
    }
    if ($form.InvokeRequired) {
        [void]$form.BeginInvoke($Action)
    } else {
        & $Action
    }
}

function Register-GuiOperationButton {
    param([System.Windows.Forms.Button]$Button)
    if ($Button -and -not $script:GuiOperationButtons.Contains($Button)) {
        [void]$script:GuiOperationButtons.Add($Button)
    }
}

function Set-GuiBusyState {
    param(
        [bool]$Busy,
        [string]$Text = "",
        [string]$State = "Idle"
    )
    Invoke-OnUiThread {
        $script:Running = $Busy
        $script:GuiOperationState = $State
        if ($form -ne $null) { $form.UseWaitCursor = $Busy }
        if ($lblBusy -ne $null) { $lblBusy.Text = $Text }
        if ($progressBusy -ne $null) {
            $progressBusy.Visible = $Busy
            $progressBusy.Style = "Marquee"
        }
        foreach ($btn in $script:GuiOperationButtons) {
            if ($null -ne $btn) { $btn.Enabled = -not $Busy }
        }
        if (-not $Busy) {
            if ($btnMain -ne $null) { $btnMain.Enabled = $true }
            if ($btnRefreshStatus -ne $null) { $btnRefreshStatus.Enabled = $true }
        }
    }
}

function Add-GuiLogThrottled {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    $safe = Redact-Secrets $Text
    [void]$script:GuiLogBuffer.Add("[" + (Get-Date -Format "HH:mm:ss") + "] " + $safe)
    if ($script:GuiLogFlushScheduled) { return }
    $script:GuiLogFlushScheduled = $true
    Invoke-OnUiThread {
        $script:GuiLogFlushScheduled = $false
        if ($script:LogBox -eq $null) { return }
        foreach ($line in $script:GuiLogBuffer) {
            $script:LogBox.AppendText($line + [Environment]::NewLine)
        }
        $script:GuiLogBuffer.Clear()
        $script:LogBox.SelectionStart = $script:LogBox.TextLength
        $script:LogBox.ScrollToCaret()
    }
}

function Test-AgentBusinessResult {
    param($JsonResult)
    if ($null -eq $JsonResult) { return $true }
    if ($JsonResult.PSObject.Properties.Name -contains "ok") {
        return [bool]$JsonResult.ok
    }
    return $true
}

function Format-AgentFailureMessage {
    param($JsonResult, [string]$ActionName)
    $parts = @("$ActionName 失败")
    if ($null -ne $JsonResult) {
        if ($JsonResult.PSObject.Properties.Name -contains "message" -and $JsonResult.message) {
            $parts += $JsonResult.message
        }
        if ($JsonResult.PSObject.Properties.Name -contains "errorCode" -and $JsonResult.errorCode) {
            $parts += ("errorCode=" + $JsonResult.errorCode)
        }
        if ($JsonResult.PSObject.Properties.Name -contains "partialFailure" -and $JsonResult.partialFailure) {
            $pf = $JsonResult.partialFailure
            $parts += ("部分完成：MC已停=" + $pf.minecraftStopped + " 快照=" + $pf.worldSnapshotPublished + " Relay=" + $pf.relayStopped + " 心跳=" + $pf.heartbeatStopped)
        }
    }
    return ($parts -join "；")
}

function Invoke-AgentCommandAsync {
    param(
        [string[]]$CommandArgs,
        [hashtable]$ExtraEnv,
        [int]$TimeoutSeconds = 0,
        [System.Threading.CancellationTokenSource]$CancelSource
    )
    Initialize-GuiAsyncHost
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $AgentPath
    $psi.Arguments = Join-ProcessArguments $CommandArgs
    $psi.WorkingDirectory = Split-Path -Parent $AgentPath
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    Set-ProcessUtf8OutputEncoding $psi
    if ($AppDataDir) { $psi.Environment["ACBH_APP_DATA_DIR"] = $AppDataDir }
    if ($ExtraEnv) {
        foreach ($key in $ExtraEnv.Keys) { $psi.Environment[$key] = [string]$ExtraEnv[$key] }
    }

    $process = [System.Diagnostics.Process]::Start($psi)
    $stdoutBuilder = New-Object System.Text.StringBuilder
    $stderrBuilder = New-Object System.Text.StringBuilder
    $stdoutDone = $false
    $stderrDone = $false
    $readStdout = {
        if (-not $stdoutDone) {
            $chunk = $process.StandardOutput.ReadLine()
            if ($null -eq $chunk) { $script:stdoutDone = $true } else { [void]$stdoutBuilder.AppendLine($chunk) }
        }
    }
    $readStderr = {
        if (-not $stderrDone) {
            $chunk = $process.StandardError.ReadLine()
            if ($null -eq $chunk) { $script:stderrDone = $true } else { [void]$stderrBuilder.AppendLine($chunk) }
        }
    }

    $deadline = if ($TimeoutSeconds -gt 0) { [datetime]::UtcNow.AddSeconds($TimeoutSeconds) } else { [datetime]::MaxValue }
    while (-not $process.HasExited -or -not $stdoutDone -or -not $stderrDone) {
        if ($CancelSource -and $CancelSource.IsCancellationRequested) {
            try { if (-not $process.HasExited) { $process.Kill() } } catch { }
            throw "操作已取消"
        }
        if ([datetime]::UtcNow -gt $deadline) {
            try { if (-not $process.HasExited) { $process.Kill() } } catch { }
            throw "操作超时"
        }
        if (-not $stdoutDone) {
            while ($process.StandardOutput.Peek() -ge 0) {
                $line = $process.StandardOutput.ReadLine()
                if ($null -eq $line) { $stdoutDone = $true; break }
                [void]$stdoutBuilder.AppendLine($line)
                Add-GuiLogThrottled $line
            }
            if ($process.HasExited) { $stdoutDone = $true }
        }
        if (-not $stderrDone) {
            while ($process.StandardError.Peek() -ge 0) {
                $line = $process.StandardError.ReadLine()
                if ($null -eq $line) { $stderrDone = $true; break }
                [void]$stderrBuilder.AppendLine($line)
                Add-GuiLogThrottled $line
            }
            if ($process.HasExited) { $stderrDone = $true }
        }
        Start-Sleep -Milliseconds 50
    }
    if (-not $process.HasExited) { $process.WaitForExit() }
    $stdout = Redact-Secrets ($stdoutBuilder.ToString().Trim())
    $stderr = Redact-Secrets ($stderrBuilder.ToString().Trim())
    $parsedJson = $null
    if ($stdout -match '^\s*[\{\[]') {
        try { $parsedJson = $stdout | ConvertFrom-Json } catch { }
    }
    return [pscustomobject]@{
        ExitCode = $process.ExitCode
        Stdout = $stdout
        Stderr = $stderr
        Output = Redact-Secrets ((@($stdout, $stderr) | Where-Object { $_ }) -join [Environment]::NewLine).Trim()
        ParsedJson = $parsedJson
    }
}

function Start-GuiOperation {
    param(
        [string]$Name,
        [string]$MutexClass = "",
        [scriptblock]$Work,
        [scriptblock]$OnComplete,
        [switch]$Cancellable,
        [switch]$AllowWhileIdleOnly
    )
    if ($script:GuiOperationState -eq "Running") {
        Add-GuiLog "$Name 已忽略：已有任务正在运行（$($script:GuiOperationName)）。"
        return
    }
    if ($MutexClass -and $script:GuiActiveMutex -and $script:GuiActiveMutex -ne $MutexClass) {
        Add-GuiLog "$Name 已忽略：与当前互斥任务冲突。"
        return
    }
    Initialize-GuiAsyncHost
    $operationId = [guid]::NewGuid().ToString()
    $script:GuiActiveOperationId = $operationId
    $script:GuiOperationName = $Name
    $script:GuiOperationGeneration++
    $generation = $script:GuiOperationGeneration
    if ($MutexClass) { $script:GuiActiveMutex = $MutexClass }
    $script:Running = $true
    $script:GuiOperationState = "Running"
    $cancelSource = $null
    if ($Cancellable) { $cancelSource = New-Object System.Threading.CancellationTokenSource }
    $script:GuiCancelSource = $cancelSource
    Set-GuiBusyState -Busy $true -Text ("处理中：" + $Name) -State "Running"

    $payload = @{
        Name = $Name
        MutexClass = $MutexClass
        OnComplete = $OnComplete
        Generation = $generation
        OperationId = $operationId
    }
    $threadJob = Start-ThreadJob -ScriptBlock $Work
    $script:GuiActiveThreadJob = $threadJob
    $pollTimer = New-Object System.Timers.Timer
    $pollTimer.Interval = 100
    $pollTimer.AutoReset = $true
    $pollTimer.add_Elapsed({
        if ($threadJob.State -eq "Running") { return }
        $pollTimer.Stop()
        $pollTimer.Dispose()
        $complete = {
            $job = $payload
            if ($script:GuiActiveOperationId -ne $job.OperationId) { return }
            $result = $null
            $errorText = $null
            try {
                $result = Receive-Job -Job $threadJob -ErrorAction Stop
            } catch {
                $errorText = $_.Exception.Message
            }
            Remove-Job -Job $threadJob -Force -ErrorAction SilentlyContinue
            $script:GuiActiveThreadJob = $null
            Complete-GuiOperation -Name $job.Name -MutexClass $job.MutexClass -Result $result -ErrorText $errorText -OnComplete $job.OnComplete -Generation $job.Generation
        }
        if ($null -ne $form) {
            [void]$form.BeginInvoke([System.Action]$complete)
        } else {
            & $complete
        }
    }.GetNewClosure())
    $pollTimer.Start()
}

function Complete-GuiOperation {
    param(
        [string]$Name,
        [string]$MutexClass,
        $Result,
        [string]$ErrorText,
        [scriptblock]$OnComplete,
        [int]$Generation
    )
    try {
        if ($Generation -ne $script:GuiOperationGeneration) { return }
        if ($ErrorText) {
            Set-GuiBusyState -Busy $false -Text "" -State "Failed"
            Show-GuiError "$Name 异常：$ErrorText"
            return
        }
        if ($OnComplete) {
            Invoke-OnUiThread { & $OnComplete $Result }
        }
        Set-GuiBusyState -Busy $false -Text "" -State "Succeeded"
    } finally {
        if ($MutexClass -and $script:GuiActiveMutex -eq $MutexClass) { $script:GuiActiveMutex = $null }
        $script:GuiActiveOperationId = $null
        $script:GuiCancelSource = $null
        $script:GuiOperationName = ""
        if ($Generation -eq $script:GuiOperationGeneration) { $script:GuiOperationState = "Idle" }
    }
}

function Cancel-GuiOperation {
    if ($script:GuiCancelSource) { $script:GuiCancelSource.Cancel() }
    if ($script:GuiActiveThreadJob) {
        Stop-Job -Job $script:GuiActiveThreadJob -ErrorAction SilentlyContinue
        Remove-Job -Job $script:GuiActiveThreadJob -Force -ErrorAction SilentlyContinue
        $script:GuiActiveThreadJob = $null
    }
    Set-GuiBusyState -Busy $false -Text "正在取消..." -State "Cancelling"
}

function Run-GuiSelfTests {
    [Console]::Out.WriteLine("GUI self-test starting")
    $script:GuiSelfTestFailures = 0
    function Assert-Test([string]$Name, [bool]$Condition) {
        if ($Condition) { [Console]::Out.WriteLine("PASS: $Name") } else { [Console]::Out.WriteLine("FAIL: $Name"); $script:GuiSelfTestFailures++ }
    }
    Initialize-GuiAsyncHost
    $hidden = New-Object System.Windows.Forms.Form
    $hidden.ShowInTaskbar = $false
    $hidden.Opacity = 0
    $hidden.Size = New-Object System.Drawing.Size(1, 1)
    $script:form = $hidden
    $script:LogBox = New-Object System.Windows.Forms.TextBox
    $script:lblBusy = New-Object System.Windows.Forms.Label
    $script:progressBusy = New-Object System.Windows.Forms.ProgressBar
    $hidden.Controls.Add($script:LogBox) | Out-Null
    $hidden.Controls.Add($script:lblBusy) | Out-Null
    $hidden.Controls.Add($script:progressBusy) | Out-Null
    $hidden.Show() | Out-Null

    $sw = [Diagnostics.Stopwatch]::StartNew()
    $noop = Start-GuiOperation -Name "selftest-delay" -Work { Start-Sleep -Milliseconds 50; return "ok" }
    Start-Sleep -Milliseconds 200
    Assert-Test "callback returns within 200ms" ($sw.ElapsedMilliseconds -lt 400)
    Assert-Test "Start-ThreadJob available" ($null -ne (Get-Command Start-ThreadJob -ErrorAction SilentlyContinue))

    $script:GuiOperationState = "Idle"
    $duplicateBlocked = $false
    Start-GuiOperation -Name "first" -MutexClass "backup" -Work { Start-Sleep -Milliseconds 300; return "ok" }
    Start-GuiOperation -Name "second" -MutexClass "backup" -Work { return "should-not-run" }
    if ($script:GuiOperationName -eq "first") { $duplicateBlocked = $true }
    $deadline = [datetime]::UtcNow.AddSeconds(3)
    while ($script:GuiOperationState -eq "Running" -and [datetime]::UtcNow -lt $deadline) {
        [System.Windows.Forms.Application]::DoEvents()
        Start-Sleep -Milliseconds 50
    }
    Assert-Test "duplicate mutex operation suppressed" $duplicateBlocked

    $biz = [pscustomobject]@{ ok = $false; message = "failed"; errorCode = "test_code" }
    Assert-Test "business failure detected" (-not (Test-AgentBusinessResult $biz))
    $msg = Format-AgentFailureMessage $biz "测试操作"
    Assert-Test "failure message includes errorCode" ($msg -match "test_code")

    $sampleCode = "ACBH-ABCDEF-123456"
    $masked = Mask-InviteIdentifier $sampleCode
    Assert-Test "invite code masked in list helper" ($masked -match "••••" -and $masked -notmatch "ABCDEF")
    $redactedLog = Redact-Secrets ("inviteCode=$sampleCode and $sampleCode")
    Assert-Test "invite code redacted from logs" ($redactedLog -notmatch $sampleCode)
    $ownerCfg = [pscustomobject]@{ group = [pscustomobject]@{ role = "owner"; groupId = "grp_test" } }
    $memberCfg = [pscustomobject]@{ group = [pscustomobject]@{ role = "member"; groupId = "grp_test" } }
    Assert-Test "owner can manage invites" (Test-CanManageInvites $ownerCfg)
    Assert-Test "member cannot manage invites" (-not (Test-CanManageInvites $memberCfg))
    $availableInvite = [pscustomobject]@{ inviteId = "inv_1"; oneTime = $true; expiresAt = ([datetime]::UtcNow.AddHours(1).ToString("o")) }
    $revokedInvite = [pscustomobject]@{ inviteId = "inv_2"; revokedAt = "2026-01-01T00:00:00Z" }
    Assert-Test "available invite detected" ((Get-InviteLifecycleStatus $availableInvite).Available)
    Assert-Test "revoked invite blocked" (-not (Get-InviteLifecycleStatus $revokedInvite).Available)
    $script:InviteListGeneration = 0
    $gen1 = ++$script:InviteListGeneration
    $gen2 = ++$script:InviteListGeneration
    Assert-Test "invite list generation advances" ($gen2 -gt $gen1)
    $failInvite = [pscustomobject]@{ ok = $false; message = "denied"; errorCode = "invite_permission_denied" }
    $failMsg = Format-InviteErrorMessage $failInvite "生成邀请码"
    Assert-Test "invite permission error is actionable" ($failMsg -match "owner")
    $script:PendingInviteCode = $sampleCode
    $script:PendingInviteCode = $null
    Assert-Test "pending invite cleared" ($null -eq $script:PendingInviteCode)

    $swInvite = [Diagnostics.Stopwatch]::StartNew()
    Start-GuiOperation -Name "selftest-invite" -MutexClass "invite" -Work { Start-Sleep -Milliseconds 40; return "ok" }
    Start-Sleep -Milliseconds 120
    Assert-Test "invite create callback returns quickly" ($swInvite.ElapsedMilliseconds -lt 300)
    $script:GuiOperationState = "Idle"
    $script:GuiActiveMutex = $null
    $inviteDup = $false
    Start-GuiOperation -Name "invite-first" -MutexClass "invite" -Work { Start-Sleep -Milliseconds 300; return "ok" }
    Start-GuiOperation -Name "invite-second" -MutexClass "invite" -Work { return "no" }
    if ($script:GuiOperationName -eq "invite-first") { $inviteDup = $true }
    $deadline2 = [datetime]::UtcNow.AddSeconds(3)
    while ($script:GuiOperationState -eq "Running" -and [datetime]::UtcNow -lt $deadline2) {
        [System.Windows.Forms.Application]::DoEvents()
        Start-Sleep -Milliseconds 50
    }
    Assert-Test "double invite generate suppressed" $inviteDup

    $hidden.Close()
    $script:GuiActiveThreadJob = $null
    [Console]::Out.WriteLine("GUI self-test failures: " + $script:GuiSelfTestFailures)
    return $script:GuiSelfTestFailures
}

function Redact-Secrets {
    param([string]$Text)
    if ([string]::IsNullOrEmpty($Text)) { return $Text }
    $safe = $Text
    $safe = [regex]::Replace($safe, '(?i)(accessKey|hostToken|joinToken|relayToken|proxyPassword|rcon\.password|ACBH_RCON_PASSWORD|inviteCode)(\s*[:=]\s*)[^\s,;}"\]]+', '$1$2[已隐藏]')
    $safe = [regex]::Replace($safe, 'ak_[A-Za-z0-9_\-]+', 'ak_[已隐藏]')
    $safe = [regex]::Replace($safe, 'ht_[A-Za-z0-9_\-]+', 'ht_[已隐藏]')
    $safe = [regex]::Replace($safe, 'ACBH-[A-Fa-f0-9]{6}-[A-Fa-f0-9]{6}', 'ACBH-[邀请码已隐藏]')
    return $safe
}

function Mask-InviteIdentifier {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return "-" }
    $value = $Text.Trim()
    if ($value -match '^ACBH-[A-Fa-f0-9]{6}-[A-Fa-f0-9]{6}$') {
        return ("ACBH-" + $value.Substring(5, 2) + "••••" + $value.Substring($value.Length - 4))
    }
    if ($value.Length -le 8) { return ($value.Substring(0, 1) + "••••") }
    return ($value.Substring(0, 4) + "••••" + $value.Substring($value.Length - 4))
}

function Get-InviteLifecycleStatus {
    param($Invite)
    if ($null -eq $Invite) { return @{ Label = "未知"; Available = $false; SortRank = 9 } }
    if ($Invite.revokedAt) { return @{ Label = "已撤销"; Available = $false; SortRank = 4 } }
    if ($Invite.usedAt -and $Invite.oneTime) { return @{ Label = "已使用"; Available = $false; SortRank = 3 } }
    if ($Invite.expiresAt) {
        try {
            $expires = [datetime]::Parse($Invite.expiresAt).ToUniversalTime()
            if ($expires -lt [datetime]::UtcNow) { return @{ Label = "已过期"; Available = $false; SortRank = 2 } }
        } catch { }
    }
    if ($Invite.usedAt -and -not $Invite.oneTime) {
        return @{ Label = "可用(已使用)"; Available = $true; SortRank = 0 }
    }
    return @{ Label = "可用"; Available = $true; SortRank = 0 }
}

function Format-InviteUsageLimit {
    param($Invite)
    if ($null -eq $Invite) { return "-" }
    if ($Invite.oneTime) { return "单次使用" }
    return "可重复使用"
}

function Format-InviteErrorMessage {
    param($JsonResult, [string]$ActionName)
    $parts = @("$ActionName 失败")
    if ($null -ne $JsonResult) {
        if ($JsonResult.PSObject.Properties.Name -contains "message" -and $JsonResult.message) {
            $parts += $JsonResult.message
        }
        if ($JsonResult.PSObject.Properties.Name -contains "errorCode" -and $JsonResult.errorCode) {
            $parts += ("errorCode=" + $JsonResult.errorCode)
        }
    }
    switch ($JsonResult.errorCode) {
        "not_configured" { $parts += "下一步：先完成 Group 配置。" }
        "invite_permission_denied" { $parts += "下一步：请使用创建 Group 的 owner 设备管理邀请码。" }
        "coordinator_unreachable" { $parts += "下一步：检查公网 Coordinator 地址与网络。" }
        "invite_create_failed" { $parts += "下一步：稍后重试或检查 Coordinator 日志。" }
        "invite_list_failed" { $parts += "下一步：确认 Coordinator 在线后刷新列表。" }
        "invite_revoke_failed" { $parts += "下一步：确认邀请码仍可撤销后重试。" }
    }
    return (Redact-Secrets (($parts -join "；")))
}

function Test-CanManageInvites {
    param($DesktopConfig)
    if ($null -eq $DesktopConfig) { return $false }
    if ($DesktopConfig.group -and $DesktopConfig.group.role -eq "owner") { return $true }
    return $false
}

function Update-MembersPanel {
    param($DesktopConfig)
    $script:DesktopGroupContext = $DesktopConfig
    if ($null -eq $lblMembersGroup) { return }
    Invoke-OnUiThread {
        $groupLabel = "未配置"
        $identity = "未注册"
        $permission = "未知"
        if ($null -ne $DesktopConfig) {
            if ($DesktopConfig.groupName) {
                $groupLabel = $DesktopConfig.groupName
            } elseif ($DesktopConfig.group -and $DesktopConfig.group.groupId) {
                $groupLabel = $DesktopConfig.group.groupId
            }
            if ($DesktopConfig.group) {
                $role = if ($DesktopConfig.group.role) { $DesktopConfig.group.role } else { "member" }
                $hostId = if ($DesktopConfig.group.hostId) { $DesktopConfig.group.hostId } else { "-" }
                $identity = "$role / $hostId"
            }
            $permission = if (Test-CanManageInvites $DesktopConfig) { "可生成与管理邀请码" } else { "仅可加入 Group，不能管理邀请码" }
        }
        $lblMembersGroup.Text = "当前 Group：" + $groupLabel
        $lblMembersIdentity.Text = "本机身份：" + $identity
        $lblMembersPermission.Text = "邀请权限：" + $permission
        if ($btnCreateInviteMain) { $btnCreateInviteMain.Enabled = (Test-CanManageInvites $DesktopConfig) -and ($script:GuiOperationState -ne "Running") }
        if ($btnOpenInviteManager) { $btnOpenInviteManager.Enabled = ($null -ne $DesktopConfig -and $DesktopConfig.group -and $DesktopConfig.group.groupId) -and ($script:GuiOperationState -ne "Running") }
    }
}

function Copy-InviteClipboard {
    param([string]$Text, [string]$Hint = "邀请码已复制，请勿发送给不受信任的人。")
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    [System.Windows.Forms.Clipboard]::SetText($Text)
    [System.Windows.Forms.MessageBox]::Show($Hint, "ACBH", "OK", "Information") | Out-Null
}

function Build-FullInviteShareText {
    param($InviteResult)
    $coord = ""
    if ($txtCoordinator -and $txtCoordinator.Text) { $coord = $txtCoordinator.Text.Trim() }
    if (-not $coord -and $script:DesktopGroupContext -and $script:DesktopGroupContext.coordinatorUrl) {
        $coord = $script:DesktopGroupContext.coordinatorUrl
    }
    $lines = @(
        ("ACBH 邀请码：" + $InviteResult.inviteCode),
        ("Coordinator：" + $(if ($coord) { $coord } else { "(请填写你的公网 Coordinator 地址)" })),
        '请在 ACBH 中选择「加入已有 Group」并粘贴邀请码。',
        "说明：此邀请码用于让另一台电脑加入 ACBH Group 并成为候选 Host，不是 Minecraft 玩家白名单。"
    )
    return ($lines -join [Environment]::NewLine)
}

function Show-InviteCreatedDialog {
    param($InviteResult)
    if ($null -eq $InviteResult -or -not $InviteResult.inviteCode) { return }
    $script:LastCreatedInvite = [pscustomobject]@{
        inviteCode = $InviteResult.inviteCode
        inviteId = $InviteResult.inviteId
        groupId = $InviteResult.groupId
        expiresAt = $InviteResult.expiresAt
        oneTime = $InviteResult.oneTime
    }
    $dlg = New-Object System.Windows.Forms.Form
    $dlg.Text = "邀请码已生成"
    $dlg.StartPosition = "CenterParent"
    $dlg.Size = New-Object System.Drawing.Size(520, 360)
    $dlg.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 9)
    $warn = New-Object System.Windows.Forms.Label
    $warn.Text = "该邀请码只显示一次，请立即复制保存。"
    $warn.ForeColor = [System.Drawing.Color]::DarkRed
    $warn.Location = New-Object System.Drawing.Point(16, 12)
    $warn.Size = New-Object System.Drawing.Size(470, 20)
    $dlg.Controls.Add($warn)
    $info = New-Object System.Windows.Forms.TextBox
    $info.Multiline = $true
    $info.ReadOnly = $true
    $info.Location = New-Object System.Drawing.Point(16, 40)
    $info.Size = New-Object System.Drawing.Size(470, 220)
    $usage = if ($InviteResult.oneTime) { "单次使用" } else { "可重复使用" }
    $info.Text = @(
        "邀请码：$($InviteResult.inviteCode)",
        "Group：$($InviteResult.groupId)",
        "过期时间：$($InviteResult.expiresAt)",
        "使用限制：$usage"
    ) -join [Environment]::NewLine
    $dlg.Controls.Add($info)
    $btnCopyCode = New-Object System.Windows.Forms.Button
    $btnCopyCode.Text = "复制邀请码"
    $btnCopyCode.Location = New-Object System.Drawing.Point(16, 275)
    $btnCopyCode.Size = New-Object System.Drawing.Size(120, 30)
    $btnCopyCode.Add_Click({ Copy-InviteClipboard $InviteResult.inviteCode })
    $dlg.Controls.Add($btnCopyCode)
    $btnCopyAll = New-Object System.Windows.Forms.Button
    $btnCopyAll.Text = "复制完整邀请信息"
    $btnCopyAll.Location = New-Object System.Drawing.Point(146, 275)
    $btnCopyAll.Size = New-Object System.Drawing.Size(140, 30)
    $btnCopyAll.Add_Click({ Copy-InviteClipboard (Build-FullInviteShareText $InviteResult) "完整邀请信息已复制，请勿发送给不受信任的人。" })
    $dlg.Controls.Add($btnCopyAll)
    $btnClose = New-Object System.Windows.Forms.Button
    $btnClose.Text = "关闭"
    $btnClose.Location = New-Object System.Drawing.Point(386, 275)
    $btnClose.Size = New-Object System.Drawing.Size(100, 30)
    $btnClose.Add_Click({ $dlg.Close() })
    $dlg.Controls.Add($btnClose)
    if ($form) { [void]$dlg.ShowDialog($form) } else { [void]$dlg.ShowDialog() }
}

function Show-CreateInviteDialog {
    if (-not (Test-CanManageInvites $script:DesktopGroupContext)) {
        Show-GuiError (Format-InviteErrorMessage ([pscustomobject]@{ message = "当前设备无权限生成邀请码。"; errorCode = "invite_permission_denied" }) "生成邀请码")
        return
    }
    $dlg = New-Object System.Windows.Forms.Form
    $dlg.Text = "生成邀请码"
    $dlg.StartPosition = "CenterParent"
    $dlg.Size = New-Object System.Drawing.Size(420, 260)
    $dlg.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 9)
    $note = New-Object System.Windows.Forms.Label
    $note.Text = "当前 Coordinator 支持：有效期、是否单次使用。备注与最大使用次数暂不支持。"
    $note.Location = New-Object System.Drawing.Point(16, 12)
    $note.Size = New-Object System.Drawing.Size(380, 36)
    $dlg.Controls.Add($note)
    $lblExpire = New-Object System.Windows.Forms.Label
    $lblExpire.Text = "有效期（分钟）"
    $lblExpire.Location = New-Object System.Drawing.Point(16, 56)
    $lblExpire.Size = New-Object System.Drawing.Size(120, 20)
    $dlg.Controls.Add($lblExpire)
    $numExpire = New-Object System.Windows.Forms.NumericUpDown
    $numExpire.Minimum = 1
    $numExpire.Maximum = 43200
    $numExpire.Value = 30
    $numExpire.Location = New-Object System.Drawing.Point(140, 54)
    $numExpire.Size = New-Object System.Drawing.Size(120, 24)
    $dlg.Controls.Add($numExpire)
    $chkOneTime = New-Object System.Windows.Forms.CheckBox
    $chkOneTime.Text = "单次使用（用一次后失效）"
    $chkOneTime.Checked = $true
    $chkOneTime.Location = New-Object System.Drawing.Point(16, 92)
    $chkOneTime.Size = New-Object System.Drawing.Size(260, 24)
    $dlg.Controls.Add($chkOneTime)
    $btnOk = New-Object System.Windows.Forms.Button
    $btnOk.Text = "生成"
    $btnOk.Location = New-Object System.Drawing.Point(200, 170)
    $btnOk.Size = New-Object System.Drawing.Size(90, 30)
    $btnCancel = New-Object System.Windows.Forms.Button
    $btnCancel.Text = "取消"
    $btnCancel.Location = New-Object System.Drawing.Point(300, 170)
    $btnCancel.Size = New-Object System.Drawing.Size(90, 30)
    $btnOk.Add_Click({
        $dlg.Tag = @{
            expiresSeconds = [int]($numExpire.Value * 60)
            oneTime = [bool]$chkOneTime.Checked
            confirmed = $true
        }
        $dlg.Close()
    })
    $btnCancel.Add_Click({ $dlg.Close() })
    $dlg.Controls.Add($btnOk)
    $dlg.Controls.Add($btnCancel)
    if ($form) { [void]$dlg.ShowDialog($form) } else { [void]$dlg.ShowDialog() }
    if (-not $dlg.Tag -or -not $dlg.Tag.confirmed) { return }
    $expiresSeconds = [int]$dlg.Tag.expiresSeconds
    $oneTimeFlag = if ($dlg.Tag.oneTime) { "true" } else { "false" }
    Start-GuiOperation -Name "生成邀请码" -MutexClass "invite" -Work {
        Invoke-AgentCommandAsync -CommandArgs @(
            "desktop", "setup", "create-invite",
            "--app-data-dir", $AppDataDir,
            "--expires-seconds", "$expiresSeconds",
            "--one-time", $oneTimeFlag
        )
    } -OnComplete {
        param($agentResult)
        $parsed = $agentResult.ParsedJson
        if (-not $parsed -and $agentResult.Stdout) {
            try { $parsed = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName "生成邀请码" } catch { }
        }
        if ($agentResult.ExitCode -ne 0 -or -not (Test-AgentBusinessResult $parsed)) {
            Show-GuiError (Format-InviteErrorMessage $parsed "生成邀请码")
            return
        }
        Add-GuiLog "邀请码已生成（明文仅显示一次，不会写入日志）。"
        Invoke-OnUiThread { Show-InviteCreatedDialog $parsed }
        if ($script:InviteManagerForm -and $script:InviteManagerForm.Visible) { Refresh-InviteListInManager }
    }
}

function Sort-InviteRows {
    param([array]$Invites)
    return @($Invites | ForEach-Object {
        $status = Get-InviteLifecycleStatus $_
        [pscustomobject]@{
            Invite = $_
            Status = $status
            CreatedAt = $_.createdAt
        }
    } | Sort-Object @{ Expression = { - [int]$_.Status.SortRank } }, @{ Expression = { $_.CreatedAt }; Descending = $true })
}

function Populate-InviteListView {
    param([System.Windows.Forms.ListView]$ListView, [array]$Invites)
    $ListView.Items.Clear()
    foreach ($row in (Sort-InviteRows $Invites)) {
        $invite = $row.Invite
        $status = $row.Status
        $item = New-Object System.Windows.Forms.ListViewItem (Mask-InviteIdentifier $invite.inviteId)
        $item.SubItems.Add($status.Label) | Out-Null
        $item.SubItems.Add($(if ($invite.createdAt) { $invite.createdAt } else { "-" })) | Out-Null
        $item.SubItems.Add($(if ($invite.expiresAt) { $invite.expiresAt } else { "-" })) | Out-Null
        $item.SubItems.Add($(Format-InviteUsageLimit $invite)) | Out-Null
        $item.SubItems.Add($(if ($invite.usedAt) { "1" } else { "0" })) | Out-Null
        $item.Tag = $invite
        if ($status.Available) { $item.ForeColor = [System.Drawing.Color]::DarkGreen }
        elseif ($status.Label -eq "已撤销") { $item.ForeColor = [System.Drawing.Color]::Gray }
        [void]$ListView.Items.Add($item)
    }
}

function Refresh-InviteListInManager {
    if (-not $script:InviteManagerForm -or -not $script:InviteListView) { return }
    if (-not (Test-CanManageInvites $script:DesktopGroupContext)) {
        Show-GuiError (Format-InviteErrorMessage ([pscustomobject]@{ message = "当前设备无权限查看邀请码。"; errorCode = "invite_permission_denied" }) "刷新邀请码列表")
        return
    }
    $generation = ++$script:InviteListGeneration
    Start-GuiOperation -Name "刷新邀请码列表" -MutexClass "invite" -Cancellable -Work {
        Invoke-AgentCommandAsync -CommandArgs @("desktop", "setup", "list-invites", "--app-data-dir", $AppDataDir) -CancelSource $script:GuiCancelSource
    } -OnComplete {
        param($agentResult)
        if ($generation -ne $script:InviteListGeneration) { return }
        $parsed = $agentResult.ParsedJson
        if (-not $parsed -and $agentResult.Stdout) {
            try { $parsed = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName "刷新邀请码列表" } catch { }
        }
        if ($agentResult.ExitCode -ne 0 -or -not (Test-AgentBusinessResult $parsed)) {
            Show-GuiError (Format-InviteErrorMessage $parsed "刷新邀请码列表")
            return
        }
        Invoke-OnUiThread {
            if ($script:InviteListView) {
                Populate-InviteListView $script:InviteListView $parsed.invites
            }
        }
        Add-GuiLog "邀请码列表已刷新（列表仅显示掩码 ID）。"
    }
}

function Revoke-SelectedInvite {
    if (-not $script:InviteListView -or $script:InviteListView.SelectedItems.Count -lt 1) {
        Show-GuiError "请先在列表中选择一个邀请码。"
        return
    }
    $invite = $script:InviteListView.SelectedItems[0].Tag
    if ($null -eq $invite) { return }
    $status = Get-InviteLifecycleStatus $invite
    if (-not $status.Available) {
        Show-GuiError "该邀请码已不可用（$($status.Label)），无需重复撤销。撤销操作不可逆。"
        return
    }
    $masked = Mask-InviteIdentifier $invite.inviteId
    $answer = [System.Windows.Forms.MessageBox]::Show(
        "确认撤销邀请码 $masked ？`n创建：$($invite.createdAt)`n状态：$($status.Label)`n撤销后不可恢复。",
        "撤销邀请码",
        "YesNo",
        "Warning"
    )
    if ($answer -ne [System.Windows.Forms.DialogResult]::Yes) { return }
    Start-GuiOperation -Name "撤销邀请码" -MutexClass "invite" -Work {
        Invoke-AgentCommandAsync -CommandArgs @("desktop", "setup", "revoke-invite", "--app-data-dir", $AppDataDir, "--invite-id", $invite.inviteId)
    } -OnComplete {
        param($agentResult)
        $parsed = $agentResult.ParsedJson
        if (-not $parsed -and $agentResult.Stdout) {
            try { $parsed = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName "撤销邀请码" } catch { }
        }
        if ($agentResult.ExitCode -ne 0 -or -not (Test-AgentBusinessResult $parsed)) {
            Show-GuiError (Format-InviteErrorMessage $parsed "撤销邀请码")
            return
        }
        Add-GuiLog "邀请码已撤销：$masked"
        Refresh-InviteListInManager
    }
}

function Open-InviteManagerWindow {
    if ($script:InviteManagerForm -and $script:InviteManagerForm.Visible) {
        $script:InviteManagerForm.Activate()
        Refresh-InviteListInManager
        return
    }
    $mgr = New-Object System.Windows.Forms.Form
    $mgr.Text = "邀请码管理"
    $mgr.StartPosition = "CenterParent"
    $mgr.Size = New-Object System.Drawing.Size(760, 460)
    $mgr.Font = New-Object System.Drawing.Font("Microsoft YaHei UI", 9)
    $lblGroup = New-Object System.Windows.Forms.Label
    $groupText = if ($script:DesktopGroupContext -and $script:DesktopGroupContext.groupName) { $script:DesktopGroupContext.groupName } elseif ($script:DesktopGroupContext -and $script:DesktopGroupContext.group -and $script:DesktopGroupContext.group.groupId) { $script:DesktopGroupContext.group.groupId } else { "未配置" }
    $lblGroup.Text = "当前 Group：$groupText"
    $lblGroup.Location = New-Object System.Drawing.Point(16, 12)
    $lblGroup.Size = New-Object System.Drawing.Size(500, 20)
    $mgr.Controls.Add($lblGroup)
    $btnRefresh = New-Object System.Windows.Forms.Button
    $btnRefresh.Text = "刷新"
    $btnRefresh.Location = New-Object System.Drawing.Point(16, 40)
    $btnRefresh.Size = New-Object System.Drawing.Size(90, 28)
    $btnRefresh.Add_Click({ Refresh-InviteListInManager })
    $mgr.Controls.Add($btnRefresh)
    $btnCreate = New-Object System.Windows.Forms.Button
    $btnCreate.Text = "生成邀请码"
    $btnCreate.Location = New-Object System.Drawing.Point(116, 40)
    $btnCreate.Size = New-Object System.Drawing.Size(110, 28)
    $btnCreate.Enabled = (Test-CanManageInvites $script:DesktopGroupContext)
    $btnCreate.Add_Click({ Show-CreateInviteDialog })
    $mgr.Controls.Add($btnCreate)
    $list = New-Object System.Windows.Forms.ListView
    $list.View = "Details"
    $list.FullRowSelect = $true
    $list.GridLines = $true
    $list.Location = New-Object System.Drawing.Point(16, 78)
    $list.Size = New-Object System.Drawing.Size(710, 280)
    [void]$list.Columns.Add("Invite ID", 140)
    [void]$list.Columns.Add("状态", 90)
    [void]$list.Columns.Add("创建时间", 150)
    [void]$list.Columns.Add("过期时间", 150)
    [void]$list.Columns.Add("使用限制", 90)
    [void]$list.Columns.Add("已用", 50)
    $mgr.Controls.Add($list)
    $script:InviteListView = $list
    $btnCopyLast = New-Object System.Windows.Forms.Button
    $btnCopyLast.Text = "复制最近邀请码"
    $btnCopyLast.Location = New-Object System.Drawing.Point(16, 370)
    $btnCopyLast.Size = New-Object System.Drawing.Size(130, 30)
    $btnCopyLast.Add_Click({
        if ($script:LastCreatedInvite -and $script:LastCreatedInvite.inviteCode) {
            Copy-InviteClipboard $script:LastCreatedInvite.inviteCode
        } else {
            [System.Windows.Forms.MessageBox]::Show("没有可复制的最近邀请码。邀请码明文只显示一次，请在生成后立即复制。", "ACBH", "OK", "Information") | Out-Null
        }
    })
    $mgr.Controls.Add($btnCopyLast)
    $btnRevoke = New-Object System.Windows.Forms.Button
    $btnRevoke.Text = "撤销邀请码"
    $btnRevoke.Location = New-Object System.Drawing.Point(156, 370)
    $btnRevoke.Size = New-Object System.Drawing.Size(110, 30)
    $btnRevoke.Enabled = (Test-CanManageInvites $script:DesktopGroupContext)
    $btnRevoke.Add_Click({ Revoke-SelectedInvite })
    $mgr.Controls.Add($btnRevoke)
    $btnClose = New-Object System.Windows.Forms.Button
    $btnClose.Text = "关闭"
    $btnClose.Location = New-Object System.Drawing.Point(626, 370)
    $btnClose.Size = New-Object System.Drawing.Size(100, 30)
    $btnClose.Add_Click({ $mgr.Close() })
    $mgr.Controls.Add($btnClose)
    $mgr.Add_FormClosed({ $script:InviteManagerForm = $null; $script:InviteListView = $null })
    $script:InviteManagerForm = $mgr
    if ($form) { [void]$mgr.Show($form) } else { [void]$mgr.Show() }
    Refresh-InviteListInManager
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

function Set-ProcessUtf8OutputEncoding {
    param([System.Diagnostics.ProcessStartInfo]$ProcessStartInfo)
    $utf8 = New-Object System.Text.UTF8Encoding $false
    try { $ProcessStartInfo.StandardOutputEncoding = $utf8 } catch { }
    try { $ProcessStartInfo.StandardErrorEncoding = $utf8 } catch { }
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
    Set-ProcessUtf8OutputEncoding $psi
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
        [switch]$Refresh,
        [string]$MutexClass = "",
        [switch]$Cancellable,
        [scriptblock]$OnComplete
    )
    Start-GuiOperation -Name $ActionName -MutexClass $MutexClass -Cancellable:$Cancellable -Work {
        Invoke-AgentCommandAsync -CommandArgs $CommandArgs -ExtraEnv $ExtraEnv -CancelSource $script:GuiCancelSource
    } -OnComplete {
        param($agentResult)
        if ($agentResult.Output -and -not $Json) { Add-GuiLogThrottled $agentResult.Output }
        $parsed = $agentResult.ParsedJson
        if ($Json -and -not $parsed -and $agentResult.Stdout) {
            try { $parsed = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName $ActionName } catch {
                Show-GuiError $_.Exception.Message
                return
            }
        }
        if ($agentResult.ExitCode -ne 0 -or -not (Test-AgentBusinessResult $parsed)) {
            Set-GuiBusyState -Busy $false -Text "" -State "Failed"
            Show-GuiError (Format-AgentFailureMessage $parsed $ActionName)
            return
        }
        Add-GuiLog "$ActionName 完成。"
        if ($OnComplete) { & $OnComplete $parsed $agentResult }
        if ($Refresh) { Refresh-Status }
    }
}

function Set-Busy {
    param([bool]$Busy, [string]$Text)
    Set-GuiBusyState -Busy $Busy -Text $Text -State $(if ($Busy) { "Running" } else { "Idle" })
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
    Invoke-AgentCommandSafe -ActionName "环境检查" -Args @("desktop", "environment", "check", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port) -Json -OnComplete {
        param($report)
        if ($null -eq $report) { return }
        $script:CurrentState = $report.state
        Invoke-OnUiThread {
            Set-Checklist $report
            $lblState.Text = "状态：" + $report.state
        }
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
    Invoke-AgentCommandSafe -ActionName "公网服务器检查" -Args @("desktop", "setup", "configure-network", "--app-data-dir", $AppDataDir, "--host-name", $hostText, "--coordinator-port", "6121", "--public-game-port", "25565") -Json -OnComplete {
        param($result)
        if ($null -eq $result) { return }
        Invoke-OnUiThread {
            $txtCoordinator.Text = $result.coordinatorUrl
            $txtPlayerAddress.Text = $result.playerAddress
            $checkLabels[6].Text = "✓ 公网服务器：" + $result.coordinatorUrl
            $checkLabels[6].ForeColor = [System.Drawing.Color]::DarkGreen
        }
        if ($result.warnings) { $result.warnings | ForEach-Object { Add-GuiLog $_ } }
    }
}

function Load-DesktopConfig {
    Invoke-AgentCommandSafe -ActionName "加载桌面配置" -Args @("desktop", "setup", "config", "--app-data-dir", $AppDataDir) -Json -OnComplete {
        param($cfg)
        if ($null -eq $cfg) { return }
        Invoke-OnUiThread {
            if ($cfg.coordinatorUrl) {
                $txtCoordinator.Text = $cfg.coordinatorUrl
                try {
                    $uri = [System.Uri]$cfg.coordinatorUrl
                    $txtHost.Text = $uri.Host
                } catch { }
            }
            if ($cfg.publicEntry) { $txtPlayerAddress.Text = $cfg.publicEntry }
            if ($cfg.lastServerDir) {
                $txtServerDir.Text = $cfg.lastServerDir
                $entry = $(if ($cfg.launchProfile.path) { $cfg.launchProfile.path } else { "已选择目录" })
                $txtServerSummary.Text = @(
                    "目录：" + $cfg.lastServerDir,
                    "启动方式：" + $entry,
                    "Java：" + $(if ($cfg.javaPath) { $cfg.javaPath } else { "未检测" }),
                    "状态：已恢复上次配置"
                ) -join [Environment]::NewLine
            }
            if ($cfg.group -and $cfg.group.groupId) {
                $lblGroupResult.Text = "服务器组：已加入 / 本机身份：已注册"
                $checkLabels[7].Text = $(if ($cfg.lastServerDir) { "✓ Minecraft 服务端：已选择" } else { $checkLabels[7].Text })
            }
            if ($cfg.ui -and $cfg.ui.advancedPanelExpanded) {
                $advancedPanel.Visible = $true
            }
        }
        Add-GuiLog "桌面配置已恢复。"
        Update-MembersPanel $cfg
    }
}

function Forget-DesktopConfig {
    Invoke-AgentCommandSafe -ActionName "忘记此电脑配置" -Args @("desktop", "setup", "forget-config", "--app-data-dir", $AppDataDir) -Json -OnComplete {
        param($result)
        if ($null -eq $result -or -not $result.ok) { return }
        Invoke-OnUiThread {
            $txtCoordinator.Text = ""
            $txtPlayerAddress.Text = ""
            $txtServerDir.Text = ""
            $txtServerSummary.Text = ""
            $lblGroupResult.Text = ""
        }
        Add-GuiLog "此电脑配置已忘记。"
    }
}

function Reset-Wizard {
    Invoke-AgentCommandSafe -ActionName "重置向导" -Args @("desktop", "setup", "reset-wizard", "--app-data-dir", $AppDataDir) -Json -OnComplete {
        param($result)
        if ($null -eq $result) { return }
        Invoke-OnUiThread {
            $txtServerDir.Text = ""
            $txtServerSummary.Text = ""
        }
        Add-GuiLog "四步向导已重置。"
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
    Invoke-AgentCommandSafe -ActionName "创建 Group" -Args $args -Json -OnComplete {
        param($result)
        if ($null -eq $result) { return }
        Invoke-OnUiThread { $lblGroupResult.Text = "Group 已创建，本机已注册（owner）。" }
        Add-GuiLog "Group 已创建。本机 Host ID: $($result.hostId)"
        if ($result.inviteCode) {
            Add-GuiLog "初始邀请码已生成（明文仅显示一次，不会写入日志）。"
            Invoke-OnUiThread { Show-InviteCreatedDialog $result }
        }
        Load-DesktopConfig
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
    $script:PendingInviteCode = $code
    $args = @("desktop", "setup", "join-group", "--app-data-dir", $AppDataDir, "--display-name", $txtDisplayName.Text, "--coordinator-url", $coord)
    $extraEnv = @{ ACBH_INVITE_CODE = $code }
    Start-GuiOperation -Name "加入 Group" -MutexClass "invite" -Work {
        Invoke-AgentCommandAsync -CommandArgs $args -ExtraEnv $extraEnv
    } -OnComplete {
        param($agentResult)
        $parsed = $agentResult.ParsedJson
        if (-not $parsed -and $agentResult.Stdout) {
            try { $parsed = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName "加入 Group" } catch { }
        }
        if ($agentResult.ExitCode -ne 0 -or -not (Test-AgentBusinessResult $parsed)) {
            Show-GuiError (Format-InviteErrorMessage $parsed "加入 Group")
            return
        }
        Invoke-OnUiThread {
            $lblGroupResult.Text = "已加入 Group，本机已注册。"
            $txtInvite.Text = ""
            $txtInvite.UseSystemPasswordChar = $true
        }
        $script:PendingInviteCode = $null
        Add-GuiLog "已加入 Group。本机 Host ID: $($parsed.hostId)"
        Load-DesktopConfig
        Refresh-Status
    }
}

function Choose-ServerDir {
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = "选择 Minecraft 服务端根目录"
    if ($dialog.ShowDialog($form) -ne [System.Windows.Forms.DialogResult]::OK) { return }
    $txtServerDir.Text = $dialog.SelectedPath
    Invoke-AgentCommandSafe -ActionName "检测 Minecraft 服务端" -Args @("desktop", "setup", "inspect-server", "--app-data-dir", $AppDataDir, "--server-dir", $dialog.SelectedPath) -Json -OnComplete {
        param($result)
        if ($null -eq $result) { return }
        Invoke-OnUiThread { Update-ServerSummary $result }
        if (-not $result.launchReady) {
            Show-GuiError "已检测到 Minecraft 服务端，但无法确定启动文件。请选择 run.bat、run.ps1、start.bat、start.ps1 或服务端核心 JAR。"
        }
    }
}

function Update-ServerSummary {
    param([object]$Result)
    $script:LastInspectResult = $Result
    $profile = $Result.launchProfile
    $entry = ""
    $launchKind = ""
    if ($profile.scriptPath) { $entry = $profile.scriptPath }
    elseif ($profile.jarPath) { $entry = $profile.jarPath }
    elseif ($Result.report.launchEntry) { $entry = $Result.report.launchEntry }
    else { $entry = "需要选择" }
    if ($profile.scriptType -eq "powershell") {
        $launchKind = "PowerShell 脚本 "
    } elseif ($profile.scriptType -eq "batch") {
        $launchKind = "批处理脚本 "
    } elseif ($profile.jarPath) {
        $launchKind = "JAR "
    }
    $requiredJava = $(if ($Result.requiredJavaVersion) { $Result.requiredJavaVersion } else { "未知" })
    $detectedJava = $(if ($Result.detectedJavaVersion) { $Result.detectedJavaVersion } else { "未检测到" })
    $eulaText = $(if ($Result.report.eulaAccepted) { "已接受" } else { "未接受" })
    $statusText = switch ([string]$Result.state) {
        "ReadyToStart" { "可以启动" }
        "ServerNeedsLaunchSelection" { "需要选择启动文件" }
        "JavaNeedsRepair" { "需要修复 Java" }
        default { [string]$Result.state }
    }
    $txtServerSummary.Text = @(
        "目录：" + $Result.report.serverDir,
        "服务端类型：" + $Result.report.serverType + "    启动方式：" + $launchKind + $entry,
        "需要 Java：" + $requiredJava + "    当前 Java：" + $detectedJava,
        "EULA：" + $eulaText + "    端口：" + $Result.report.serverPort,
        "状态：" + $statusText
    ) -join [Environment]::NewLine
    if ($Result.launchReady) {
        $checkLabels[7].Text = "✓ Minecraft 服务端：" + $Result.report.serverType
        $checkLabels[7].ForeColor = [System.Drawing.Color]::DarkGreen
    } else {
        $checkLabels[7].Text = "△ Minecraft 服务端：需要选择启动文件"
        $checkLabels[7].ForeColor = [System.Drawing.Color]::DarkOrange
    }
    Add-GuiLog ("服务端检测：" + $statusText + "，类型：" + $Result.report.serverType + "，启动方式：" + $launchKind + $entry)
}

function Select-LaunchFile {
    param([string]$Filter, [string]$Title)
    $serverDir = $txtServerDir.Text.Trim()
    if (-not $serverDir) {
        Show-GuiError "请先选择 Minecraft 服务端目录。"
        return
    }
    $dialog = New-Object System.Windows.Forms.OpenFileDialog
    $dialog.Title = $Title
    $dialog.InitialDirectory = $serverDir
    $dialog.Filter = $Filter
    if ($dialog.ShowDialog($form) -ne [System.Windows.Forms.DialogResult]::OK) { return }
    Invoke-AgentCommandSafe -ActionName "选择启动文件" -Args @("desktop", "server", "select-launch", "--app-data-dir", $AppDataDir, "--path", $dialog.FileName) -Json -OnComplete {
        param($result)
        if ($null -ne $result) { Invoke-OnUiThread { Update-ServerSummary $result } }
    }
}

function Use-RecommendedLaunch {
    if ($null -eq $script:LastInspectResult -or $null -eq $script:LastInspectResult.candidates -or $null -eq $script:LastInspectResult.candidates.recommended) {
        Show-GuiError "当前没有可用的自动推荐。"
        return
    }
    $profile = $script:LastInspectResult.candidates.recommended
    $path = $(if ($profile.scriptPath) { $profile.scriptPath } else { $profile.jarPath })
    if (-not $path) {
        Show-GuiError "当前没有可用的自动推荐。"
        return
    }
    Invoke-AgentCommandSafe -ActionName "使用自动推荐" -Args @("desktop", "server", "select-launch", "--app-data-dir", $AppDataDir, "--path", $path) -Json -OnComplete {
        param($result)
        if ($null -ne $result) { Invoke-OnUiThread { Update-ServerSummary $result } }
    }
}

function Open-ServerDir {
    $serverDir = $txtServerDir.Text.Trim()
    if (-not $serverDir) {
        Show-GuiError "请先选择 Minecraft 服务端目录。"
        return
    }
    Start-Process $serverDir
}

function Show-LaunchEvidence {
    if ($null -eq $script:LastInspectResult) {
        Show-GuiError "请先检测 Minecraft 服务端目录。"
        return
    }
    $lines = @()
    if ($script:LastInspectResult.launchProfile.evidence) { $lines += $script:LastInspectResult.launchProfile.evidence }
    if ($script:LastInspectResult.blockingReasons) { $lines += $script:LastInspectResult.blockingReasons }
    if ($script:LastInspectResult.warnings) { $lines += $script:LastInspectResult.warnings }
    if ($lines.Count -eq 0) { $lines = @("暂无额外识别证据。") }
    [System.Windows.Forms.MessageBox]::Show(($lines -join [Environment]::NewLine), "识别证据", "OK", "Information") | Out-Null
}

function Format-ByteSize {
    param([long]$Bytes)
    if ($Bytes -lt 0) { return "-" }
    if ($Bytes -ge 1GB) { return ("{0:N2} GB" -f ($Bytes / 1GB)) }
    if ($Bytes -ge 1MB) { return ("{0:N2} MB" -f ($Bytes / 1MB)) }
    if ($Bytes -ge 1KB) { return ("{0:N2} KB" -f ($Bytes / 1KB)) }
    return "$Bytes B"
}

function Prompt-Text {
    param([string]$Title, [string]$Prompt, [string]$Default = "")
    $dialog = New-Object System.Windows.Forms.Form
    $dialog.Text = $Title
    $dialog.StartPosition = "CenterParent"
    $dialog.Size = New-Object System.Drawing.Size(420, 150)
    $dialog.FormBorderStyle = "FixedDialog"
    $dialog.MaximizeBox = $false
    $dialog.MinimizeBox = $false
    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Prompt
    $label.Location = New-Object System.Drawing.Point(12, 12)
    $label.Size = New-Object System.Drawing.Size(380, 20)
    $dialog.Controls.Add($label)
    $text = New-Object System.Windows.Forms.TextBox
    $text.Text = $Default
    $text.Location = New-Object System.Drawing.Point(12, 40)
    $text.Size = New-Object System.Drawing.Size(380, 24)
    $dialog.Controls.Add($text)
    $ok = New-Object System.Windows.Forms.Button
    $ok.Text = "确定"
    $ok.DialogResult = [System.Windows.Forms.DialogResult]::OK
    $ok.Location = New-Object System.Drawing.Point(216, 76)
    $ok.Size = New-Object System.Drawing.Size(80, 28)
    $dialog.Controls.Add($ok)
    $cancel = New-Object System.Windows.Forms.Button
    $cancel.Text = "取消"
    $cancel.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
    $cancel.Location = New-Object System.Drawing.Point(312, 76)
    $cancel.Size = New-Object System.Drawing.Size(80, 28)
    $dialog.Controls.Add($cancel)
    $dialog.AcceptButton = $ok
    $dialog.CancelButton = $cancel
    if ($dialog.ShowDialog($form) -ne [System.Windows.Forms.DialogResult]::OK) { return $null }
    return $text.Text.Trim()
}

function Update-WorldBackupPanel {
    param([object]$Status)
    if ($null -eq $Status) {
        $txtWorldBackupSummary.Text = "尚未加载世界差量备份状态。"
        return
    }
    $latest = $Status.remoteLatest
    $lines = @()
    if ($latest) {
        $lines += "最新快照：$($latest.snapshotId)"
        $lines += "时间：$($latest.createdAt)"
        $lines += "来源 Host：$($latest.sourceHostId)"
        $lines += "Generation：$($latest.hostGeneration)"
        $lines += "一致性：$(if ($latest.consistent) { '一致' } else { '不一致' })"
        $lines += "逻辑大小：$(Format-ByteSize $latest.logicalSize)"
        $lines += "本次上传量：$(Format-ByteSize $latest.uploadedSize)"
        $lines += "变化/删除文件数：$($latest.changedFileCount) / $($latest.deletedFileCount)"
    } else {
        $lines += "最新快照：暂无"
        if ($Status.remoteLatestError) { $lines += "远程读取失败：$($Status.remoteLatestError)" }
    }
    $lines += "历史数量：$($Status.historyCount)"
    if ($Status.localLatestSnapshotId) { $lines += "本地索引最新：$($Status.localLatestSnapshotId)（$($Status.localFileCount) 文件）" }
    if ($Status.indexError) { $lines += "本地索引错误：$($Status.indexError)" }
    $txtWorldBackupSummary.Text = ($lines -join [Environment]::NewLine)
}

function Refresh-WorldBackupStatus {
    Start-GuiOperation -Name "世界差量备份状态" -MutexClass "refresh" -Cancellable -Work {
        Invoke-AgentCommandAsync -CommandArgs @("desktop", "world-backup", "status", "--app-data-dir", $AppDataDir) -CancelSource $script:GuiCancelSource
    } -OnComplete {
        param($agentResult)
        if ($agentResult.ExitCode -ne 0) { return }
        try {
            $status = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName "世界差量备份状态"
            $script:LastWorldBackupStatus = $status
            Invoke-OnUiThread { Update-WorldBackupPanel $status }
        } catch { }
    }
}

function Backup-WorldStopped {
    Start-GuiOperation -Name "立即备份世界" -MutexClass "backup" -Work {
        Invoke-AgentCommandAsync -CommandArgs @("desktop", "world-backup", "create", "--app-data-dir", $AppDataDir) -CancelSource $script:GuiCancelSource
    } -OnComplete {
        param($agentResult)
        $parsed = $agentResult.ParsedJson
        if (-not $parsed -and $agentResult.Stdout) {
            try { $parsed = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName "立即备份世界" } catch { }
        }
        if ($agentResult.ExitCode -ne 0 -or -not (Test-AgentBusinessResult $parsed)) {
            Show-GuiError (Format-AgentFailureMessage $parsed "立即备份世界")
            return
        }
        Add-GuiLog "世界快照已发布：$($parsed.snapshotId)（上传 $(Format-ByteSize $parsed.uploadedSize)）"
        Refresh-WorldBackupStatus
    }
}

function Backup-WorldOnline {
    $extraEnv = @{}
    if (-not $env:ACBH_RCON_PASSWORD) {
        $password = Prompt-Text -Title "在线安全备份" -Prompt "请输入 Minecraft RCON 密码："
        if ([string]::IsNullOrWhiteSpace($password)) {
            Show-GuiError "在线安全备份需要 RCON 密码。"
            return
        }
        $extraEnv["ACBH_RCON_PASSWORD"] = $password
    }
    Invoke-AgentCommandSafe -ActionName "在线安全备份" -Args @("desktop", "world-backup", "create", "--app-data-dir", $AppDataDir, "--online") -ExtraEnv $extraEnv -Json -MutexClass "backup" -OnComplete {
        param($result)
        if ($null -eq $result) { return }
        Add-GuiLog "在线世界快照已发布：$($result.snapshotId)"
        Refresh-WorldBackupStatus
    }
}

function Restore-LatestWorld {
    Start-GuiOperation -Name "拉取最新世界" -MutexClass "restore" -Work {
        Invoke-AgentCommandAsync -CommandArgs @("desktop", "world-backup", "restore", "latest", "--app-data-dir", $AppDataDir) -CancelSource $script:GuiCancelSource
    } -OnComplete {
        param($agentResult)
        $parsed = $agentResult.ParsedJson
        if (-not $parsed -and $agentResult.Stdout) {
            try { $parsed = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName "拉取最新世界" } catch { }
        }
        if ($agentResult.ExitCode -ne 0 -or -not (Test-AgentBusinessResult $parsed)) {
            Show-GuiError (Format-AgentFailureMessage $parsed "拉取最新世界")
            return
        }
        Add-GuiLog "世界已恢复：$($parsed.snapshotId)（下载 $($parsed.downloadedFiles) 个文件）"
        Refresh-WorldBackupStatus
    }
}

function Show-WorldBackupHistory {
    Invoke-AgentCommandSafe -ActionName "查看历史快照" -Args @("desktop", "world-backup", "list", "--app-data-dir", $AppDataDir) -Json -OnComplete {
        param($result)
        if ($null -eq $result) { return }
        $lines = @()
        foreach ($snap in ($result.snapshots | Sort-Object createdAt -Descending)) {
            $pin = if ($snap.pinned) { " [固定]" } else { "" }
            $cons = if ($snap.consistent) { "一致" } else { "不一致" }
            $lines += "$($snap.snapshotId)  $($snap.createdAt)  host=$($snap.sourceHostId)  gen=$($snap.hostGeneration)  $cons$pin"
        }
        if ($lines.Count -eq 0) { $lines = @("暂无历史世界快照。") }
        [System.Windows.Forms.MessageBox]::Show(($lines -join [Environment]::NewLine), "历史世界快照", "OK", "Information") | Out-Null
    }
}

function Restore-WorldBackupSnapshot {
    $snapshotId = Prompt-Text -Title "恢复快照" -Prompt "输入快照 ID（留空取消）：" -Default "latest"
    if ([string]::IsNullOrWhiteSpace($snapshotId)) { return }
    Start-GuiOperation -Name "恢复快照" -MutexClass "restore" -Work {
        Invoke-AgentCommandAsync -CommandArgs @("desktop", "world-backup", "restore", $snapshotId, "--app-data-dir", $AppDataDir) -CancelSource $script:GuiCancelSource
    } -OnComplete {
        param($agentResult)
        $parsed = $agentResult.ParsedJson
        if (-not $parsed -and $agentResult.Stdout) {
            try { $parsed = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName "恢复快照" } catch { }
        }
        if ($agentResult.ExitCode -ne 0 -or -not (Test-AgentBusinessResult $parsed)) {
            Show-GuiError (Format-AgentFailureMessage $parsed "恢复快照")
            return
        }
        Add-GuiLog "快照已恢复：$($parsed.snapshotId)"
        Refresh-WorldBackupStatus
    }
}

function Pin-WorldBackupSnapshot {
    $snapshotId = Prompt-Text -Title "固定快照" -Prompt "输入要固定的快照 ID："
    if ([string]::IsNullOrWhiteSpace($snapshotId)) { return }
    Invoke-AgentCommandSafe -ActionName "固定快照" -Args @("desktop", "world-backup", "pin", $snapshotId, "--app-data-dir", $AppDataDir) -Json -OnComplete {
        param($result)
        if ($null -ne $result -and $result.ok) {
            Add-GuiLog "快照已固定：$($result.snapshotId)"
            Refresh-WorldBackupStatus
        }
    }
}

function Delete-WorldBackupSnapshot {
    $snapshotId = Prompt-Text -Title "删除快照" -Prompt "输入要删除的快照 ID："
    if ([string]::IsNullOrWhiteSpace($snapshotId)) { return }
    $confirm = [System.Windows.Forms.MessageBox]::Show("确认删除快照 $snapshotId ？", "删除快照", "YesNo", "Warning")
    if ($confirm -ne [System.Windows.Forms.DialogResult]::Yes) { return }
    Invoke-AgentCommandSafe -ActionName "删除快照" -Args @("desktop", "world-backup", "delete", $snapshotId, "--app-data-dir", $AppDataDir) -Json -OnComplete {
        param($result)
        if ($null -ne $result -and $result.ok) {
            Add-GuiLog "快照已删除：$($result.snapshotId)"
            Refresh-WorldBackupStatus
        }
    }
}

function Resume-WorldBackup {
    Start-GuiOperation -Name "继续未完成备份" -MutexClass "backup" -Cancellable -Work {
        Invoke-AgentCommandAsync -CommandArgs @("desktop", "world-backup", "resume", "--app-data-dir", $AppDataDir) -CancelSource $script:GuiCancelSource
    } -OnComplete {
        param($agentResult)
        $parsed = $agentResult.ParsedJson
        if (-not $parsed -and $agentResult.Stdout) {
            try { $parsed = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName "继续未完成备份" } catch { }
        }
        if ($agentResult.ExitCode -ne 0 -or -not (Test-AgentBusinessResult $parsed)) {
            Show-GuiError (Format-AgentFailureMessage $parsed "继续未完成备份")
            return
        }
        Add-GuiLog "备份已继续：$($parsed.snapshotId)"
        Refresh-WorldBackupStatus
    }
}

function Complete-Setup {
    Invoke-AgentCommandSafe -ActionName "完成配置" -Args @("desktop", "setup", "complete", "--app-data-dir", $AppDataDir) -Json -OnComplete {
        param($result)
        if ($null -ne $result -and $result.ok) {
            $script:CurrentState = "Ready"
            Invoke-OnUiThread { $lblState.Text = "状态：Ready" }
            Refresh-Status
        } elseif ($null -ne $result) {
            Show-GuiError $result.message
        }
    }
}

function Apply-StatusPanel {
    param($status)
    if ($null -eq $status) { return }
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
        $stateText = "状态：" + $status.state
        if ($status.startTimeout) { $stateText += "（启动等待 " + $status.startTimeout + "）" }
        $lblState.Text = $stateText
    }
}

function Refresh-Status {
    Start-GuiOperation -Name "刷新状态" -MutexClass "refresh" -Cancellable -Work {
        Invoke-AgentCommandAsync -CommandArgs @("desktop", "server", "status", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port) -CancelSource $script:GuiCancelSource
    } -OnComplete {
        param($agentResult)
        try {
            if ($agentResult.ExitCode -ne 0) { throw $agentResult.Stderr }
            $status = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName "刷新状态"
            Invoke-OnUiThread { Apply-StatusPanel $status }
            Add-GuiLog "状态已刷新。"
            Refresh-WorldBackupStatus
        } catch {
            Add-GuiLog "状态刷新失败：" + $_.Exception.Message
        }
    }
}

function Invoke-MainAction {
    $isStop = ($btnMain.Text -eq "停止服务器")
    $actionName = if ($isStop) { "停止服务器" } else { "在此电脑启动" }
    $args = if ($isStop) {
        @("desktop", "server", "stop-auto", "--app-data-dir", $AppDataDir)
    } else {
        @("desktop", "server", "start-auto", "--app-data-dir", $AppDataDir)
    }
    Start-GuiOperation -Name $actionName -MutexClass "server" -Work {
        Invoke-AgentCommandAsync -CommandArgs $args -CancelSource $script:GuiCancelSource
    } -OnComplete {
        param($agentResult)
        $parsed = $agentResult.ParsedJson
        if (-not $parsed -and $agentResult.Stdout) {
            try { $parsed = ConvertFrom-JsonSafe -Text $agentResult.Stdout -ActionName $actionName } catch { }
        }
        if ($parsed) {
            if ($parsed.steps) { $parsed.steps | ForEach-Object { Add-GuiLog $_ } }
            if ($parsed.warnings) { $parsed.warnings | ForEach-Object { Add-GuiLog "提示：" + $_ } }
        }
        if ($agentResult.ExitCode -ne 0 -or -not (Test-AgentBusinessResult $parsed)) {
            Show-GuiError (Format-AgentFailureMessage $parsed $actionName)
            return
        }
        Add-GuiLog "$actionName 完成。"
        Refresh-Status
    }
}

function Import-OfflinePack {
    $dialog = New-Object System.Windows.Forms.OpenFileDialog
    $dialog.Filter = "ACBH 环境包 (*.zip)|*.zip|All files (*.*)|*.*"
    if ($dialog.ShowDialog($form) -ne [System.Windows.Forms.DialogResult]::OK) { return }
    Invoke-AgentCommandSafe -ActionName "导入离线环境包" -Args @("desktop", "environment", "import-pack", "--app-data-dir", $AppDataDir, "--file", $dialog.FileName) -Json -OnComplete {
        param($result)
        if ($null -ne $result -and $result.ok) {
            Add-GuiLog "离线环境包已导入：" + $result.package.packageId
            Run-EnvironmentCheck
        } elseif ($null -ne $result) {
            Show-GuiError $result.message
        }
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

if ($SelfTest) {
    try {
        $failed = Run-GuiSelfTests
        if ($failed -gt 0) { exit 1 }
        Write-Output "ACBH desktop GUI self-test ok"
        exit 0
    } catch {
        Write-Output ("GUI self-test error: " + $_.Exception.Message)
        Write-Output $_.ScriptStackTrace
        exit 1
    }
}

$form = New-Object System.Windows.Forms.Form
$form.Text = "ACBH"
$form.StartPosition = "CenterScreen"
$form.Size = New-Object System.Drawing.Size(1040, 1000)
$form.MinimumSize = New-Object System.Drawing.Size(980, 820)
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
$lblBusy.Size = New-Object System.Drawing.Size(300, 24)
$form.Controls.Add($lblBusy)

$progressBusy = New-Object System.Windows.Forms.ProgressBar
$progressBusy.Location = New-Object System.Drawing.Point(860, 28)
$progressBusy.Size = New-Object System.Drawing.Size(120, 18)
$progressBusy.Style = "Marquee"
$progressBusy.MarqueeAnimationSpeed = 25
$progressBusy.Visible = $false
$form.Controls.Add($progressBusy)

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
$setupPanel.Size = New-Object System.Drawing.Size(490, 470)
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
$txtInvite.Size = New-Object System.Drawing.Size(120, 24)
$txtInvite.UseSystemPasswordChar = $true
$setupPanel.Controls.Add($txtInvite)
$btnToggleInvite = New-Object System.Windows.Forms.Button
$btnToggleInvite.Text = "显示"
$btnToggleInvite.Location = New-Object System.Drawing.Point(428, 46)
$btnToggleInvite.Size = New-Object System.Drawing.Size(36, 26)
$btnToggleInvite.Add_Click({
    if ($txtInvite.UseSystemPasswordChar) {
        $txtInvite.UseSystemPasswordChar = $false
        $btnToggleInvite.Text = "隐藏"
    } else {
        $txtInvite.UseSystemPasswordChar = $true
        $btnToggleInvite.Text = "显示"
    }
})
$setupPanel.Controls.Add($btnToggleInvite)
$lblInviteHint = New-Object System.Windows.Forms.Label
$lblInviteHint.Text = "邀请码用于让另一台电脑加入 ACBH Group（非 MC 玩家白名单）"
$lblInviteHint.ForeColor = [System.Drawing.Color]::DimGray
$lblInviteHint.Location = New-Object System.Drawing.Point(304, 72)
$lblInviteHint.Size = New-Object System.Drawing.Size(170, 32)
$setupPanel.Controls.Add($lblInviteHint)
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

$txtServerSummary = New-Object System.Windows.Forms.TextBox
$txtServerSummary.Location = New-Object System.Drawing.Point(18, 280)
$txtServerSummary.Size = New-Object System.Drawing.Size(445, 64)
$txtServerSummary.Multiline = $true
$txtServerSummary.ScrollBars = "Vertical"
$txtServerSummary.ReadOnly = $true
$setupPanel.Controls.Add($txtServerSummary)
Add-Button $setupPanel "自动推荐" 18 350 { Use-RecommendedLaunch } ""
Add-Button $setupPanel "选择启动脚本" 198 350 { Select-LaunchFile "启动脚本 (*.bat;*.ps1)|*.bat;*.ps1|All files (*.*)|*.*" "选择启动脚本" } ""
Add-Button $setupPanel "选择核心 JAR" 18 384 { Select-LaunchFile "Minecraft 核心 (*.jar)|*.jar|All files (*.*)|*.*" "选择服务端核心 JAR" } ""
Add-Button $setupPanel "打开服务端目录" 198 384 { Open-ServerDir } ""
Add-Button $setupPanel "查看识别证据" 18 418 { Show-LaunchEvidence } ""

$mainPanel = New-Object System.Windows.Forms.GroupBox
$mainPanel.Text = "主界面"
$mainPanel.Location = New-Object System.Drawing.Point(20, 310)
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
Register-GuiOperationButton $btnMain
$btnRefreshStatus = Add-Button $mainPanel "刷新状态" 330 140 { Refresh-Status } "刷新服务器与世界备份状态。"
Register-GuiOperationButton $btnRefreshStatus
Add-Button $mainPanel "完成并进入 ACBH" 204 104 { Complete-Setup } "保存 setupComplete=true。"
Add-Button $mainPanel "复制玩家地址" 20 140 { if ($txtPlayerAddress.Text) { [System.Windows.Forms.Clipboard]::SetText($txtPlayerAddress.Text); Add-GuiLog "玩家地址已复制。" } } ""
Add-Button $mainPanel "打开日志" 204 140 { Open-LogDir } ""

$membersPanel = New-Object System.Windows.Forms.GroupBox
$membersPanel.Text = "成员与邀请"
$membersPanel.Location = New-Object System.Drawing.Point(20, 500)
$membersPanel.Size = New-Object System.Drawing.Size(470, 120)
$form.Controls.Add($membersPanel)

$lblMembersGroup = New-Object System.Windows.Forms.Label
$lblMembersGroup.Text = "当前 Group：未配置"
$lblMembersGroup.Location = New-Object System.Drawing.Point(16, 24)
$lblMembersGroup.Size = New-Object System.Drawing.Size(430, 18)
$membersPanel.Controls.Add($lblMembersGroup)
$lblMembersIdentity = New-Object System.Windows.Forms.Label
$lblMembersIdentity.Text = "本机身份：未注册"
$lblMembersIdentity.Location = New-Object System.Drawing.Point(16, 44)
$lblMembersIdentity.Size = New-Object System.Drawing.Size(430, 18)
$membersPanel.Controls.Add($lblMembersIdentity)
$lblMembersPermission = New-Object System.Windows.Forms.Label
$lblMembersPermission.Text = "邀请权限：未知"
$lblMembersPermission.Location = New-Object System.Drawing.Point(16, 64)
$lblMembersPermission.Size = New-Object System.Drawing.Size(430, 18)
$membersPanel.Controls.Add($lblMembersPermission)
$btnCreateInviteMain = Add-Button $membersPanel "生成邀请码" 16 86 { Show-CreateInviteDialog } "生成一次性或可重复使用的 Group 邀请码。"
Register-GuiOperationButton $btnCreateInviteMain
$btnOpenInviteManager = Add-Button $membersPanel "查看邀请码" 196 86 { Open-InviteManagerWindow } "打开邀请码管理窗口。"
Register-GuiOperationButton $btnOpenInviteManager
Add-Button $membersPanel "刷新" 376 86 { Load-DesktopConfig; if ($script:InviteManagerForm -and $script:InviteManagerForm.Visible) { Refresh-InviteListInManager } } "刷新 Group 身份；若管理窗口已打开则同步列表。"

$worldBackupPanel = New-Object System.Windows.Forms.GroupBox
$worldBackupPanel.Text = "世界差量备份"
$worldBackupPanel.Location = New-Object System.Drawing.Point(20, 630)
$worldBackupPanel.Size = New-Object System.Drawing.Size(470, 190)
$form.Controls.Add($worldBackupPanel)

$txtWorldBackupSummary = New-Object System.Windows.Forms.TextBox
$txtWorldBackupSummary.Location = New-Object System.Drawing.Point(16, 24)
$txtWorldBackupSummary.Size = New-Object System.Drawing.Size(438, 72)
$txtWorldBackupSummary.Multiline = $true
$txtWorldBackupSummary.ScrollBars = "Vertical"
$txtWorldBackupSummary.ReadOnly = $true
$worldBackupPanel.Controls.Add($txtWorldBackupSummary)

function Add-WorldBackupButton {
    param([string]$Text, [int]$X, [int]$Y, [scriptblock]$Click, [string]$Tip = "")
    $button = New-Object System.Windows.Forms.Button
    $button.Text = $Text
    $button.Location = New-Object System.Drawing.Point($X, $Y)
    $button.Size = New-Object System.Drawing.Size(104, 28)
    Add-SafeClick $button $Click
    if ($Tip) { $script:ToolTip.SetToolTip($button, $Tip) }
    $worldBackupPanel.Controls.Add($button)
    return $button
}

Add-WorldBackupButton "立即备份世界" 16 104 { Backup-WorldStopped } "创建并上传停止状态下的世界差量快照。"
Add-WorldBackupButton "在线安全备份" 126 104 { Backup-WorldOnline } "通过 RCON 安全保存后创建差量快照。"
Add-WorldBackupButton "拉取最新世界" 236 104 { Restore-LatestWorld } "从 Coordinator 恢复最新一致世界快照。"
Add-WorldBackupButton "查看历史快照" 346 104 { Show-WorldBackupHistory } "列出远程历史世界快照。"
Add-WorldBackupButton "恢复快照" 16 138 { Restore-WorldBackupSnapshot } "恢复指定快照 ID。"
Add-WorldBackupButton "固定快照" 126 138 { Pin-WorldBackupSnapshot } "固定快照以避免被保留策略删除。"
Add-WorldBackupButton "删除快照" 236 138 { Delete-WorldBackupSnapshot } "删除未固定且非最新的快照。"
Add-WorldBackupButton "继续未完成备份" 346 138 { Resume-WorldBackup } "重新规划并上传缺失对象。"

$advancedPanel = New-Object System.Windows.Forms.GroupBox
$advancedPanel.Text = "高级诊断"
$advancedPanel.Location = New-Object System.Drawing.Point(510, 680)
$advancedPanel.Size = New-Object System.Drawing.Size(490, 130)
$advancedPanel.Visible = $false
$form.Controls.Add($advancedPanel)

Add-Button $advancedPanel "导入离线环境包" 14 24 { Import-OfflinePack } ""
Add-Button $advancedPanel "运行环境修复" 192 24 { Invoke-AgentCommandSafe -ActionName "环境修复" -Args @("desktop", "environment", "repair", "--app-data-dir", $AppDataDir) -Json } ""
Add-Button $advancedPanel "查看高级状态" 14 58 { Invoke-AgentCommandSafe -ActionName "高级状态" -Args @("desktop", "status", "--app-data-dir", $AppDataDir, "--coordinator", $CoordinatorPath, "--port", $Port, "--json") -Refresh } ""
Add-Button $advancedPanel "清理 runtime cache" 192 58 { Invoke-AgentCommandSafe -ActionName "清理 runtime cache" -Args @("desktop", "environment", "clear-cache", "--app-data-dir", $AppDataDir) -Json } ""
Add-Button $advancedPanel "忘记此电脑配置" 314 24 { Forget-DesktopConfig } ""
Add-Button $advancedPanel "重置向导" 314 58 { Reset-Wizard } ""
$btnAdvanced = Add-Button $form "高级诊断" 830 28 { $advancedPanel.Visible = -not $advancedPanel.Visible } "默认隐藏高级 CLI 能力。"

$script:LogBox = New-Object System.Windows.Forms.TextBox
$script:LogBox.Location = New-Object System.Drawing.Point(20, 840)
$script:LogBox.Size = New-Object System.Drawing.Size(980, 120)
$script:LogBox.Multiline = $true
$script:LogBox.ScrollBars = "Vertical"
$script:LogBox.ReadOnly = $true
$script:LogBox.Font = New-Object System.Drawing.Font("Consolas", 9)
$form.Controls.Add($script:LogBox)

$form.Add_FormClosing({
    param($sender, $e)
    if ($script:GuiOperationState -eq "Running") {
        $answer = [System.Windows.Forms.MessageBox]::Show(
            "后台任务仍在运行（$($script:GuiOperationName)）。确定要关闭窗口吗？",
            "ACBH",
            "YesNo",
            "Warning"
        )
        if ($answer -ne [System.Windows.Forms.DialogResult]::Yes) {
            $e.Cancel = $true
            return
        }
        Cancel-GuiOperation
    }
    if ($script:GuiActiveThreadJob) {
        Stop-Job -Job $script:GuiActiveThreadJob -ErrorAction SilentlyContinue
        Remove-Job -Job $script:GuiActiveThreadJob -Force -ErrorAction SilentlyContinue
        $script:GuiActiveThreadJob = $null
    }
})

$form.Add_Shown({
    try {
        Initialize-GuiAsyncHost
        Run-EnvironmentCheck
        Load-DesktopConfig
        Refresh-Status
        Update-WorldBackupPanel $null
    } catch {
        Show-GuiError "初始化失败：$($_.Exception.Message)"
    }
})

[void]$form.ShowDialog()
