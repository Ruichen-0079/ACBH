param(
    [string]$Version = "v0.5.0-minimal-core-alpha1",
    [string]$OutputDir = "dist/v0.5.0-minimal-core-alpha1",
    [switch]$AllowDirty,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"

function Repo-Root {
    $root = git rev-parse --show-toplevel 2>$null
    if (-not $root) {
        throw "This script must run inside a git repository."
    }
    return $root.Trim()
}

function Assert-CleanWorktree {
    if ($AllowDirty) {
        Write-Host "AllowDirty set; skipping clean worktree check."
        return
    }
    $status = @(git status --short)
    if ($status.Count -gt 0) {
        throw "Refusing to build release from dirty worktree. Commit or use a clean git worktree. Pass -AllowDirty only for local dry-run diagnostics."
    }
}

function Assert-SensitivePathsExcluded {
    param([string[]]$RelativePaths)
    $forbidden = @(
        "config.json",
        "migration-report.json",
        "legacy/",
        "logs/",
        "restore-v0.5-test",
        "test.pem",
        ".env",
        "%APPDATA%/ACBH",
        "AppData/Roaming/ACBH",
        "MinecraftServers/example-server-data"
    )
    foreach ($path in $RelativePaths) {
        $normalized = $path.Replace("\", "/")
        foreach ($item in $forbidden) {
            if ($normalized -like "*$item*") {
                throw "Sensitive path would be packaged: $path"
            }
        }
    }
}

function Copy-ReleaseDocs {
    param([string]$RepoRoot, [string]$DocsDir)
    $docs = @(
        "docs/architecture/minimal-core.md",
        "docs/api/body-api.md",
        "docs/config/config-json.md",
        "docs/migration/v0.4-to-v0.5.md",
        "docs/known-limits.md"
    )
    Assert-SensitivePathsExcluded -RelativePaths $docs
    New-Item -ItemType Directory -Force -Path $DocsDir | Out-Null
    foreach ($doc in $docs) {
        $source = Join-Path $RepoRoot $doc
        if (-not (Test-Path -LiteralPath $source)) {
            throw "Required release doc missing: $doc"
        }
        Copy-Item -LiteralPath $source -Destination (Join-Path $DocsDir (Split-Path $doc -Leaf)) -Force
    }
}

function Copy-MinimalGuiScript {
    param([string]$RepoRoot, [string]$ScriptsDir)
    $script = "scripts/acbh-minimal-core-gui.ps1"
    Assert-SensitivePathsExcluded -RelativePaths @($script)
    New-Item -ItemType Directory -Force -Path $ScriptsDir | Out-Null
    Copy-Item -LiteralPath (Join-Path $RepoRoot $script) -Destination (Join-Path $ScriptsDir (Split-Path $script -Leaf)) -Force
}

function Write-ReleaseNotesTemplate {
    param([string]$Path, [string]$Version)
    $text = @"
# ACBH $Version

Minimal-core alpha release for Windows.

## Included

- Local body runtime API on 127.0.0.1:6120
- Minimal GUI
- Single-owner/private instance identity model
- Listener status and VPS relay configuration
- Backup upload, snapshot list, and restore download through a protocolVersion=2 compatible VPS Coordinator

## Validation Summary

- Tested against a v0.4.0-alpha6-hotfix2 compatible Coordinator
- Verified with a ~995MB Minecraft server backup
- 806 files
- 24 roots

## Not Included

- GitHub Release publishing
- Coordinator upgrade bundle
- User config, logs, backup data, restore data, or tokens
- Protocol v3
"@
    Set-Content -LiteralPath $Path -Value $text -Encoding UTF8
}

function Write-Sha256Sums {
    param([string]$Directory)
    $sumPath = Join-Path $Directory "SHA256SUMS"
    $root = (Resolve-Path -LiteralPath $Directory).Path.TrimEnd("\", "/")
    $files = Get-ChildItem -LiteralPath $Directory -File -Recurse | Where-Object { $_.FullName -ne $sumPath } | Sort-Object FullName
    $lines = foreach ($file in $files) {
        $hash = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        $relative = $file.FullName.Substring($root.Length).TrimStart("\", "/").Replace("\", "/")
        "$hash  $relative"
    }
    Set-Content -LiteralPath $sumPath -Value $lines -Encoding ASCII
}

function Show-ArtifactList {
    param([string]$Directory)
    Get-ChildItem -LiteralPath $Directory -File | Sort-Object Name | ForEach-Object {
        [pscustomobject]@{
            name = $_.Name
            size = $_.Length
        }
    } | ConvertTo-Json -Depth 3
}

$repoRoot = Repo-Root
Set-Location $repoRoot
Assert-CleanWorktree

$releaseFiles = @(
    "acbh-agent-windows-amd64.exe",
    "acbh-desktop-windows-amd64.exe",
    "scripts/acbh-minimal-core-gui.ps1",
    "release-notes.md",
    "SHA256SUMS"
)
Assert-SensitivePathsExcluded -RelativePaths $releaseFiles

$resolvedOutput = if ([System.IO.Path]::IsPathRooted($OutputDir)) {
    $OutputDir
} else {
    Join-Path $repoRoot $OutputDir
}

if ($DryRun) {
    Write-Host "Dry run only. No artifacts will be built."
    Write-Host "Version: $Version"
    Write-Host "OutputDir: $resolvedOutput"
    Write-Host "Would build:"
    Write-Host "  acbh-agent-windows-amd64.exe"
    Write-Host "  acbh-desktop-windows-amd64.exe"
    Write-Host "Would copy minimal-core docs and generate release-notes.md + SHA256SUMS."
    exit 0
}

New-Item -ItemType Directory -Force -Path $resolvedOutput | Out-Null

Push-Location (Join-Path $repoRoot "agent")
try {
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    go build -trimpath -ldflags="-s -w -X main.version=$Version" -o (Join-Path $resolvedOutput "acbh-agent-windows-amd64.exe") .
    go build -trimpath -ldflags="-s -w -X main.version=$Version" -o (Join-Path $resolvedOutput "acbh-desktop-windows-amd64.exe") .\cmd\acbh-desktop
} finally {
    Remove-Item Env:\GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue
    Pop-Location
}

Copy-ReleaseDocs -RepoRoot $repoRoot -DocsDir (Join-Path $resolvedOutput "docs")
Copy-MinimalGuiScript -RepoRoot $repoRoot -ScriptsDir (Join-Path $resolvedOutput "scripts")
Write-ReleaseNotesTemplate -Path (Join-Path $resolvedOutput "release-notes.md") -Version $Version
Write-Sha256Sums -Directory $resolvedOutput

Write-Host "Minimal-core release artifacts prepared:"
Show-ArtifactList -Directory $resolvedOutput
