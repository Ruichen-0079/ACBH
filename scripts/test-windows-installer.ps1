[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$SetupPath
)

$ErrorActionPreference = "Stop"
$SetupPath = [System.IO.Path]::GetFullPath($SetupPath)
if (-not (Test-Path -LiteralPath $SetupPath)) { throw "Setup EXE does not exist: $SetupPath" }
if (Get-Service -Name ACBHAgent -ErrorAction SilentlyContinue) { throw "ACBHAgent service already exists" }
if (Get-NetTCPConnection -State Listen -LocalPort 6130 -ErrorAction SilentlyContinue) { throw "Port 6130 is already in use" }

$javaBefore = @(Get-CimInstance Win32_Process -Filter "Name = 'java.exe'" | ForEach-Object {
    "$($_.ProcessId)|$($_.CreationDate.ToUniversalTime().ToString('O'))"
})
$logRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("acbh-installer-test-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $logRoot | Out-Null

function Invoke-Setup {
    param([string]$LogName)
    $process = Start-Process -FilePath $SetupPath -ArgumentList @(
        "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/SP-",
        "/LOG=$(Join-Path $logRoot $LogName)"
    ) -WindowStyle Hidden -Wait -PassThru
    if ($process.ExitCode -ne 0) { throw "Setup failed with exit code $($process.ExitCode)" }
}

function Wait-Agent {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $service = Get-Service -Name ACBHAgent -ErrorAction SilentlyContinue
        if ($service -and $service.Status -eq "Running") {
            try {
                $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:6130/local/v1/status" -TimeoutSec 2
                if ($response.StatusCode -eq 200) { return $response.Content }
            } catch {}
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Installed Agent did not become healthy"
}

Invoke-Setup "install.log"
$statusJSON = Wait-Agent
if ($statusJSON -match '(?i)access_token|pem|ssh') { throw "Status API exposed a forbidden credential field" }
$serviceInfo = Get-CimInstance Win32_Service -Filter "Name = 'ACBHAgent'"
if (-not $serviceInfo.PathName.Contains("hobby service") -or
    -not $serviceInfo.PathName.Contains("--frpc") -or
    -not $serviceInfo.PathName.Contains("--app-data-dir") -or
    $serviceInfo.StartMode -ne "Auto") {
    throw "Installed service command line is incomplete"
}
$listeners = @(Get-NetTCPConnection -State Listen -LocalPort 6130)
if ($listeners.Count -ne 1 -or $listeners[0].LocalAddress -ne "127.0.0.1") {
    throw "Installed local API is not bound exclusively to 127.0.0.1"
}
$dataRoot = Join-Path $env:ProgramData "ACBH"
$dataACL = Get-Acl -LiteralPath $dataRoot
if (-not $dataACL.AreAccessRulesProtected) { throw "ProgramData ACBH directory still inherits permissive ACLs" }
$usersSID = "S-1-5-32-545"
$userRules = @($dataACL.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier]) |
    Where-Object { $_.IdentityReference.Value -eq $usersSID })
if ($userRules.Count -ne 1 -or
    ($userRules[0].FileSystemRights -band [Security.AccessControl.FileSystemRights]::Write) -ne 0 -or
    $userRules[0].InheritanceFlags -ne [Security.AccessControl.InheritanceFlags]::None) {
    throw "ProgramData root grants ordinary users write or inherited access to protected files"
}
$configSentinel = Join-Path $dataRoot "preserve-user-config.test"
$logSentinel = Join-Path $dataRoot "logs\preserve-user-log.test"
Set-Content -LiteralPath $configSentinel -Value "configuration must survive uninstall" -Encoding UTF8
Set-Content -LiteralPath $logSentinel -Value "logs must survive uninstall" -Encoding UTF8
$protocolCommand = (Get-ItemProperty -LiteralPath "Registry::HKEY_CLASSES_ROOT\acbh\shell\open\command")."(default)"
if ($protocolCommand -notmatch 'acbh-launcher\.exe' -or $protocolCommand -notmatch '%1') {
    throw "Installed log-directory URI handler is missing"
}
if (@(Get-CimInstance Win32_Process -Filter "Name = 'acbh-agent.exe'").Count -ne 1) {
    throw "Installed package did not run exactly one Agent"
}
if (@(Get-CimInstance Win32_Process -Filter "Name = 'frpc.exe'").Count -ne 0) {
    throw "Agent started frpc without a saved desired configuration"
}

Invoke-Setup "upgrade.log"
[void](Wait-Agent)
if (@(Get-CimInstance Win32_Process -Filter "Name = 'acbh-agent.exe'").Count -ne 1) {
    throw "In-place upgrade produced duplicate Agent processes"
}

$uninstaller = Join-Path $env:ProgramFiles "ACBH\unins000.exe"
if (-not (Test-Path -LiteralPath $uninstaller)) { throw "Installed uninstaller was not found" }
$uninstall = Start-Process -FilePath $uninstaller -ArgumentList @(
    "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART"
) -WindowStyle Hidden -Wait -PassThru
if ($uninstall.ExitCode -ne 0) { throw "Uninstall failed with exit code $($uninstall.ExitCode)" }

$deadline = [DateTime]::UtcNow.AddSeconds(20)
while ((Get-Service -Name ACBHAgent -ErrorAction SilentlyContinue) -and [DateTime]::UtcNow -lt $deadline) {
    Start-Sleep -Milliseconds 250
}
if (Get-Service -Name ACBHAgent -ErrorAction SilentlyContinue) { throw "Uninstall did not remove the Agent service" }
if ((Get-Content -Raw -LiteralPath $configSentinel).Trim() -ne "configuration must survive uninstall" -or
    (Get-Content -Raw -LiteralPath $logSentinel).Trim() -ne "logs must survive uninstall") {
    throw "Uninstall changed or removed user configuration and logs"
}

$javaAfter = @(Get-CimInstance Win32_Process -Filter "Name = 'java.exe'" | ForEach-Object {
    "$($_.ProcessId)|$($_.CreationDate.ToUniversalTime().ToString('O'))"
})
if (($javaBefore -join [Environment]::NewLine) -ne ($javaAfter -join [Environment]::NewLine)) {
    throw "Install, upgrade, or uninstall changed Java processes"
}
Write-Host "Silent install, service startup, in-place upgrade, uninstall, and Java coexistence verified."
