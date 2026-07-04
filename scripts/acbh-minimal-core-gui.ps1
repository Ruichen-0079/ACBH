param(
    [string]$AgentPath,
    [string]$AppDataDir,
    [string]$BodyListen = "127.0.0.1:6120",
    [switch]$SelfTest
)

$ErrorActionPreference = "Stop"

if ($SelfTest) {
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
    $safe = [regex]::Replace($safe, '(?i)(hostToken|accessKey)(\s*[:=]\s*)[^\s,;}"]+', '$1$2[hidden]')
    $safe = [regex]::Replace($safe, 'ht_[A-Za-z0-9_\-]+', 'ht_[hidden]')
    return $safe
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
    $psi.Arguments = ($args | ForEach-Object { '"' + ($_ -replace '"', '\"') + '"' }) -join " "
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
        $txtGroupId.Text = $cfg.identity.groupId
        $txtMemberId.Text = $cfg.identity.memberId
        $txtHostId.Text = $cfg.identity.hostId
        $txtHostToken.Text = $cfg.identity.hostToken
        $txtDisplayName.Text = $cfg.identity.displayName
        $txtDeviceName.Text = $cfg.identity.deviceName
        $txtServerDir.Text = $cfg.server.dir
        Add-Log "config.json loaded."
    } catch {
        Add-Log ("Config not loaded yet: " + $_.Exception.Message)
    }
}

function Save-Config {
    try {
        $cfg = @{
            schemaVersion = 1
            mode = [string]$cmbMode.SelectedItem
            coordinatorUrl = $txtCoordinator.Text.Trim()
            identity = @{
                groupId = $txtGroupId.Text.Trim()
                memberId = $txtMemberId.Text.Trim()
                hostId = $txtHostId.Text.Trim()
                hostToken = $txtHostToken.Text.Trim()
                displayName = $txtDisplayName.Text.Trim()
                deviceName = $txtDeviceName.Text.Trim()
                platform = "windows"
            }
            server = @{ dir = $txtServerDir.Text.Trim() }
            listener = @{ enabled = $true; localHost = "127.0.0.1"; localPort = 25565 }
            relay = @{ enabled = $true; publicHost = ""; coordinatorPort = 6121; minecraftPort = 25565 }
            backup = @{
                profileId = "minecraft-migratable"
                include = @("dir:world","dir:mods","dir:config","file:server.properties","file:eula.txt","file:ops.json","file:whitelist.json","file:banned-ips.json","file:banned-players.json")
                exclude = @("dir:libraries","dir:jre","dir:logs","dir:crash-reports")
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

function Run-Init {
    try {
        $op = Invoke-BodyJson -Method "POST" -Path "/v1/init"
        $txtOperation.Text = ($op | ConvertTo-Json -Depth 12)
        if ($op.state -eq "success") {
            $lblState.Text = "State: ready"
        } else {
            $lblState.Text = "State: init failed"
        }
        Add-Log ("Init operation: " + $op.operationId + " state=" + $op.state)
    } catch {
        Show-Error ("Init failed: " + $_.Exception.Message)
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
$form.Size = New-Object System.Drawing.Size(920, 720)
$form.MinimumSize = New-Object System.Drawing.Size(860, 640)
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

Add-Label "Coordinator URL" 24 172 | Out-Null
$txtCoordinator = Add-TextBox 170 170 650
$txtCoordinator.Text = "http://121.40.101.224:6121"
Add-Label "Group ID" 24 206 | Out-Null
$txtGroupId = Add-TextBox 170 204 250
Add-Label "Member ID" 450 206 | Out-Null
$txtMemberId = Add-TextBox 560 204 260
Add-Label "Host ID" 24 240 | Out-Null
$txtHostId = Add-TextBox 170 238 250
Add-Label "Host Token" 450 240 | Out-Null
$txtHostToken = Add-TextBox 560 238 260
$txtHostToken.UseSystemPasswordChar = $true
Add-Label "Display name" 24 274 | Out-Null
$txtDisplayName = Add-TextBox 170 272 250
$txtDisplayName.Text = "私人本地主机"
Add-Label "Device name" 450 274 | Out-Null
$txtDeviceName = Add-TextBox 560 272 260
$txtDeviceName.Text = $env:COMPUTERNAME
Add-Label "Server dir" 24 308 | Out-Null
$txtServerDir = Add-TextBox 170 306 500
Add-Button "Choose dir" 680 303 { Choose-ServerDir } | Out-Null

Add-Label "Actual probe URL" 24 342 | Out-Null
$txtActualRequestUrl = Add-TextBox 170 340 650 $true
Add-Label "Protocol" 24 376 | Out-Null
$txtProtocol = Add-TextBox 170 374 120 $true
Add-Label "Capabilities" 310 376 | Out-Null
$txtCapabilities = Add-TextBox 420 374 400 $true
Add-Label "Error details" 24 410 | Out-Null
$txtErrorDetails = Add-TextBox 170 408 650 $true

Add-Button "Refresh health" 170 450 { Refresh-Health } | Out-Null
Add-Button "Load config" 330 450 { Load-Config } | Out-Null
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

$form.Add_Shown({
    try {
        Start-Body
        Refresh-Health
        Load-Config
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
