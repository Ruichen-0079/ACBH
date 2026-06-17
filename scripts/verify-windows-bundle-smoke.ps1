# Smoke-test the Windows private desktop entrypoint without npm/pnpm/corepack install steps.
$ErrorActionPreference = "Stop"

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$TempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("acbh-bundle-smoke-" + [Guid]::NewGuid().ToString("N"))
$BundleRoot = Join-Path $TempRoot "bundle"
$AppData = Join-Path $TempRoot "appdata"

function Get-FreeTcpPort {
    $Listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Parse("127.0.0.1"), 0)
    $Listener.Start()
    try {
        return $Listener.LocalEndpoint.Port
    } finally {
        $Listener.Stop()
    }
}

function Invoke-Pnpm {
    param([Parameter(Mandatory = $true)][string[]]$PnpmArgs)

    if (Get-Command pnpm -ErrorAction SilentlyContinue) {
        & pnpm @PnpmArgs
    } elseif (Get-Command corepack -ErrorAction SilentlyContinue) {
        & corepack pnpm @PnpmArgs
    } else {
        throw "pnpm not found; install pnpm or use a full release bundle"
    }
    if ($LASTEXITCODE -ne 0) {
        throw "pnpm command failed: $($PnpmArgs -join ' ')"
    }
}

try {
    New-Item -ItemType Directory -Force -Path (Join-Path $BundleRoot "coordinator\dist") | Out-Null

    Push-Location (Join-Path $RepoRoot "apps\coordinator")
    try {
        Invoke-Pnpm -PnpmArgs @("build")
    } finally {
        Pop-Location
    }

    Push-Location (Join-Path $RepoRoot "agent")
    try {
        go build -o (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") .
        go build -o (Join-Path $BundleRoot "acbh-desktop-windows-amd64.exe") .\cmd\acbh-desktop
    } finally {
        Pop-Location
    }

    Copy-Item -Recurse -Force (Join-Path $RepoRoot "apps\coordinator\dist\*") (Join-Path $BundleRoot "coordinator\dist")
    Copy-Item -Force (Join-Path $RepoRoot "apps\coordinator\package.json") (Join-Path $BundleRoot "coordinator\package.json")
    New-Item -ItemType Directory -Force -Path (Join-Path $BundleRoot "scripts") | Out-Null
    Copy-Item -Force (Join-Path $RepoRoot "scripts\acbh-desktop-gui.ps1") (Join-Path $BundleRoot "scripts\acbh-desktop-gui.ps1")
    $ParseErrors = $null
    $null = [System.Management.Automation.PSParser]::Tokenize(
        (Get-Content -Raw -Encoding UTF8 -Path (Join-Path $BundleRoot "scripts\acbh-desktop-gui.ps1")),
        [ref]$ParseErrors
    )
    if ($ParseErrors -and $ParseErrors.Count -gt 0) {
        throw "GUI script parse failed: $($ParseErrors[0].Message)"
    }
    Push-Location (Join-Path $BundleRoot "coordinator")
    try {
        Invoke-Pnpm -PnpmArgs @("install", "--prod", "--offline")
    } finally {
        Pop-Location
    }

    $PackageJson = Get-Content -Raw -Path (Join-Path $BundleRoot "coordinator\package.json") | ConvertFrom-Json
    if (-not $PackageJson.dependencies.ws) {
        throw "ws is not present in coordinator production dependencies"
    }

    $Port = Get-FreeTcpPort
    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop start `
        --app-data-dir $AppData `
        --coordinator (Join-Path $BundleRoot "coordinator\dist\index.js") `
        --port "$Port"
    if ($LASTEXITCODE -ne 0) {
        throw "desktop start failed"
    }

    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop status --app-data-dir $AppData --port "$Port" --json | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "desktop status failed"
    }

    & (Join-Path $BundleRoot "acbh-agent-windows-amd64.exe") desktop stop --app-data-dir $AppData --port "$Port"
    if ($LASTEXITCODE -ne 0) {
        throw "desktop stop failed"
    }

    Write-Host "Windows private desktop bundle smoke passed." -ForegroundColor Green
} finally {
    if (Test-Path $TempRoot) {
        Remove-Item -Recurse -Force $TempRoot
    }
}
