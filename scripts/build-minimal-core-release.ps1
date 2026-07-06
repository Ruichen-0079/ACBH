param(
    [string]$Version = "v0.5.1-public-relay-hotfix",
    [string]$OutputDir = "dist/v0.5.1-public-relay-hotfix",
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
        Write-Warning "AllowDirty is for local diagnostics only. Do not use it for release validation."
        return
    }
    $status = @(git status --short)
    if ($status.Count -gt 0) {
        throw "Refusing to build release from dirty worktree. Commit changes or use a clean git worktree."
    }
}

function Assert-UnderRoot {
    param([string]$Path, [string]$Root)
    $fullPath = [System.IO.Path]::GetFullPath($Path)
    $fullRoot = [System.IO.Path]::GetFullPath($Root)
    if (-not $fullPath.StartsWith($fullRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Path is outside the expected root: $Path"
    }
}

function Get-RelativePathCompat {
    param([string]$Root, [string]$Path)
    $fullRoot = [System.IO.Path]::GetFullPath($Root)
    $fullPath = [System.IO.Path]::GetFullPath($Path)
    if (-not $fullRoot.EndsWith([System.IO.Path]::DirectorySeparatorChar)) {
        $fullRoot = $fullRoot + [System.IO.Path]::DirectorySeparatorChar
    }
    $rootUri = New-Object System.Uri($fullRoot)
    $pathUri = New-Object System.Uri($fullPath)
    $relativeUri = $rootUri.MakeRelativeUri($pathUri)
    return [System.Uri]::UnescapeDataString($relativeUri.ToString()).Replace("/", [System.IO.Path]::DirectorySeparatorChar)
}

function Remove-DirectoryIfExists {
    param([string]$Path, [string]$AllowedRoot)
    if (-not (Test-Path -LiteralPath $Path)) { return }
    Assert-UnderRoot -Path $Path -Root $AllowedRoot
    Remove-Item -LiteralPath $Path -Recurse -Force
}

function Assert-SensitivePathsExcluded {
    param([string[]]$RelativePaths)
    $forbiddenExactFiles = @(
        "config.json",
        "migration-report.json",
        "test.pem",
        ".env"
    )
    $forbiddenSegments = @(
        "legacy",
        "logs",
        "restore-v0.5-test",
        "%APPDATA%/ACBH",
        "AppData/Roaming/ACBH",
        "MinecraftServers/example-server-data"
    )
    foreach ($path in $RelativePaths) {
        $normalized = $path.Replace("\", "/")
        $segments = @($normalized.Split("/") | Where-Object { $_ -ne "" })
        foreach ($file in $forbiddenExactFiles) {
            if ($segments -contains $file) {
                throw "Sensitive path would be packaged: $path"
            }
        }
        foreach ($item in $forbiddenSegments) {
            $itemSegments = @($item.Split("/") | Where-Object { $_ -ne "" })
            if ($itemSegments.Count -eq 1) {
                if ($segments -contains $itemSegments[0]) {
                    throw "Sensitive path would be packaged: $path"
                }
                continue
            }
            if ($normalized -eq $item -or $normalized -like "$item/*" -or $normalized -like "*/$item/*") {
                throw "Sensitive path would be packaged: $path"
            }
        }
    }
}

function Assert-BundleHasNoSensitiveFiles {
    param([string]$BundleRoot)
    $root = [System.IO.Path]::GetFullPath($BundleRoot)
    $paths = Get-ChildItem -LiteralPath $BundleRoot -Recurse -File | ForEach-Object {
        Get-RelativePathCompat -Root $root -Path $_.FullName
    }
    Assert-SensitivePathsExcluded -RelativePaths @($paths)
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

function Copy-PowerShellUtf8Bom {
    param([string]$Source, [string]$Destination)
    $text = [System.IO.File]::ReadAllText($Source, [System.Text.Encoding]::UTF8)
    $utf8Bom = New-Object System.Text.UTF8Encoding $true
    [System.IO.File]::WriteAllText($Destination, $text, $utf8Bom)
}

function Write-ReleaseNotesTemplate {
    param([string]$Path, [string]$Version, [string]$Commit)
    $text = @"
# ACBH $Version

Minimal-core Windows GUI and matching VPS Coordinator bundle.

## Included

- Windows minimal-core GUI bundle
- Local body runtime API on 127.0.0.1:6120
- Single-owner/private instance identity model
- Listener status and VPS relay configuration
- Matching Linux amd64 Coordinator bundle from commit $Commit

## Not Included

- GitHub Release publishing
- User config, logs, backup data, restore data, or tokens
"@
    Set-Content -LiteralPath $Path -Value $text -Encoding UTF8
}

function Write-BuildInfo {
    param(
        [string]$Path,
        [string]$Version,
        [string]$Commit,
        [string]$BuildTime,
        [string]$Target,
        [string]$Component
    )
    $info = [ordered]@{
        version = $Version
        commit = $Commit
        buildTime = $BuildTime
        target = $Target
        component = $Component
    }
    $info | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $Path -Encoding UTF8
}

function Write-CoordinatorReadme {
    param([string]$Path, [string]$Version, [string]$Commit)
    $lines = @(
        "# ACBH Coordinator Deployment",
        "",
        "Version: $Version",
        "Build commit: $Commit",
        "",
        "## Upload to VPS",
        "",
        "Upload this tar.gz file to the VPS, for example under /opt/acbh.",
        "",
        "## Extract",
        "",
        '```bash',
        "tar -xzf acbh-coordinator-linux-amd64-bundle-$Version.tar.gz",
        "cd acbh-coordinator-linux-amd64-bundle-$Version",
        '```',
        "",
        "## Start Coordinator",
        "",
        '```bash',
        "chmod +x start-coordinator.sh coordinator",
        "./start-coordinator.sh",
        '```',
        "",
        "The default API port is 6121. This is the Coordinator API port, not the Minecraft join port 25565.",
        "",
        "## Check Health And Version",
        "",
        '```bash',
        "curl -i http://127.0.0.1:6121/health",
        "curl -i http://127.0.0.1:6121/version",
        '```',
        "",
        "## Firewall And Security Group",
        "",
        "Allow TCP 6121 so the Windows GUI can reach the Coordinator. If players connect directly to Minecraft or a relay port, also allow 25565 or the configured public Minecraft port.",
        "",
        "## Windows GUI Address",
        "",
        "In the Windows GUI, fill the VPS address as http://YOUR_VPS_PUBLIC_IP:6121. Do not use the Minecraft join port 25565 here.",
        "",
        "## Logs",
        "",
        "When running ./start-coordinator.sh directly, logs are printed to the current terminal. If you use systemd or another process manager, check that service log.",
        "",
        "## Confirm Matching Version",
        "",
        "The Windows zip and this Coordinator bundle must come from the same release commit. Compare build-info.json commit or call /version and check buildCommit."
    )
    Set-Content -LiteralPath $Path -Value $lines -Encoding UTF8
}

function Invoke-PnpmCoordinatorBuild {
    param([string]$RepoRoot)
    if (Get-Command corepack -ErrorAction SilentlyContinue) {
        Push-Location $RepoRoot
        try {
            & corepack pnpm install --frozen-lockfile
            if ($LASTEXITCODE -ne 0) {
                throw "pnpm install failed with exit code $LASTEXITCODE"
            }
            & corepack pnpm --filter "@acbh/coordinator" build
        } finally {
            Pop-Location
        }
    } elseif (Get-Command pnpm -ErrorAction SilentlyContinue) {
        & pnpm --dir $RepoRoot install --frozen-lockfile
        if ($LASTEXITCODE -ne 0) {
            throw "pnpm install failed with exit code $LASTEXITCODE"
        }
        & pnpm --dir $RepoRoot --filter "@acbh/coordinator" build
    } else {
        throw "pnpm/corepack not found; cannot build coordinator dist."
    }
    if ($LASTEXITCODE -ne 0) {
        throw "coordinator build failed with exit code $LASTEXITCODE"
    }
}

function Invoke-NpmInstallProduction {
    param([string]$Directory)
    if (-not (Get-Command npm -ErrorAction SilentlyContinue)) {
        throw "npm not found; cannot install coordinator production dependencies."
    }
    Push-Location $Directory
    try {
        & npm install --omit=dev --no-audit --no-fund --package-lock=false
        if ($LASTEXITCODE -ne 0) {
            throw "npm install failed with exit code $LASTEXITCODE"
        }
    } finally {
        Pop-Location
    }
}

function Compress-ZipDirectory {
    param([string]$SourceDir, [string]$TargetZip)
    if (Test-Path -LiteralPath $TargetZip) {
        Remove-Item -LiteralPath $TargetZip -Force
    }
    Compress-Archive -Path (Join-Path $SourceDir "*") -DestinationPath $TargetZip -Force
}

function Write-Sha256File {
    param([string]$Path)
    $hash = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
    $line = "$hash  $(Split-Path -Leaf $Path)"
    Set-Content -LiteralPath ($Path + ".sha256") -Value $line -Encoding ASCII
    return $hash
}

function Write-Sha256Sums {
    param([string]$Directory)
    $sumPath = Join-Path $Directory "SHA256SUMS"
    $files = Get-ChildItem -LiteralPath $Directory -File | Where-Object { $_.Name -ne "SHA256SUMS" } | Sort-Object Name
    $lines = foreach ($file in $files) {
        $hash = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  $($file.Name)"
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

$commit = (git rev-parse HEAD).Trim()
$buildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

$resolvedOutput = if ([System.IO.Path]::IsPathRooted($OutputDir)) {
    $OutputDir
} else {
    Join-Path $repoRoot $OutputDir
}
$resolvedOutput = [System.IO.Path]::GetFullPath($resolvedOutput)
$stagingRoot = Join-Path $resolvedOutput "_staging"

$windowsBundleName = "acbh-$Version-windows-amd64"
$windowsZipName = "$windowsBundleName.zip"
$coordinatorBundleName = "acbh-coordinator-linux-amd64-bundle-$Version"
$coordinatorTarName = "$coordinatorBundleName.tar.gz"

if ($DryRun) {
    Write-Host "Dry run only. No artifacts will be built."
    Write-Host "Version: $Version"
    Write-Host "Commit: $commit"
    Write-Host "OutputDir: $resolvedOutput"
    Write-Host "Would build:"
    Write-Host "  $windowsZipName"
    Write-Host "  $coordinatorTarName"
    Write-Host "  per-artifact .sha256 files and SHA256SUMS"
    exit 0
}

New-Item -ItemType Directory -Force -Path $resolvedOutput | Out-Null
Remove-DirectoryIfExists -Path $stagingRoot -AllowedRoot $resolvedOutput
New-Item -ItemType Directory -Force -Path $stagingRoot | Out-Null

Invoke-PnpmCoordinatorBuild -RepoRoot $repoRoot

$agentExe = Join-Path $resolvedOutput "acbh-agent-windows-amd64.exe"
$desktopExe = Join-Path $resolvedOutput "acbh-desktop-windows-amd64.exe"

Push-Location (Join-Path $repoRoot "agent")
try {
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    go build -trimpath -ldflags="-s -w -X main.version=$Version" -o $agentExe .
    if ($LASTEXITCODE -ne 0) {
        throw "go build acbh-agent-windows-amd64.exe failed with exit code $LASTEXITCODE"
    }
    go build -trimpath -ldflags="-s -w -X main.version=$Version" -o $desktopExe .\cmd\acbh-desktop
    if ($LASTEXITCODE -ne 0) {
        throw "go build acbh-desktop-windows-amd64.exe failed with exit code $LASTEXITCODE"
    }
} finally {
    Remove-Item Env:\GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue
    Pop-Location
}

$releaseNotes = Join-Path $resolvedOutput "release-notes.md"
Write-ReleaseNotesTemplate -Path $releaseNotes -Version $Version -Commit $commit

$windowsRoot = Join-Path $stagingRoot $windowsBundleName
New-Item -ItemType Directory -Force -Path (Join-Path $windowsRoot "scripts") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $windowsRoot "docs") | Out-Null
Copy-Item -LiteralPath $agentExe -Destination (Join-Path $windowsRoot "acbh-agent-windows-amd64.exe") -Force
Copy-Item -LiteralPath $desktopExe -Destination (Join-Path $windowsRoot "acbh-desktop-windows-amd64.exe") -Force
Copy-PowerShellUtf8Bom -Source (Join-Path $repoRoot "scripts/acbh-minimal-core-gui.ps1") -Destination (Join-Path $windowsRoot "scripts/acbh-minimal-core-gui.ps1")
Copy-ReleaseDocs -RepoRoot $repoRoot -DocsDir (Join-Path $windowsRoot "docs")
Copy-Item -LiteralPath $releaseNotes -Destination (Join-Path $windowsRoot "release-notes.md") -Force
Write-BuildInfo -Path (Join-Path $windowsRoot "build-info.json") -Version $Version -Commit $commit -BuildTime $buildTime -Target "windows-amd64" -Component "minimal-core-gui"

$guiInBundle = Join-Path $windowsRoot "scripts/acbh-minimal-core-gui.ps1"
if (-not (Test-Path -LiteralPath $guiInBundle)) {
    throw "Windows bundle missing GUI script."
}
Assert-BundleHasNoSensitiveFiles -BundleRoot $windowsRoot

$windowsZip = Join-Path $resolvedOutput $windowsZipName
Compress-ZipDirectory -SourceDir $windowsRoot -TargetZip $windowsZip

$coordinatorRoot = Join-Path $stagingRoot $coordinatorBundleName
New-Item -ItemType Directory -Force -Path (Join-Path $coordinatorRoot "dist") | Out-Null
Copy-Item -LiteralPath (Join-Path $repoRoot "apps/coordinator/package.json") -Destination (Join-Path $coordinatorRoot "package.json") -Force
Copy-Item -LiteralPath (Join-Path $repoRoot "apps/coordinator/dist/index.js") -Destination (Join-Path $coordinatorRoot "dist/index.js") -Force
Copy-Item -Recurse -Force (Join-Path $repoRoot "apps/coordinator/dist/*") (Join-Path $coordinatorRoot "dist")
Invoke-NpmInstallProduction -Directory $coordinatorRoot

$envExample = @"
PORT=6121
HOST=0.0.0.0
ACBH_VERSION=$Version
ACBH_BUILD_COMMIT=$commit
ACBH_PROTOCOL_VERSION=2
"@
Set-Content -LiteralPath (Join-Path $coordinatorRoot ".env.example") -Value $envExample -Encoding ASCII
Write-BuildInfo -Path (Join-Path $coordinatorRoot "build-info.json") -Version $Version -Commit $commit -BuildTime $buildTime -Target "linux-amd64" -Component "coordinator"
Set-Content -LiteralPath (Join-Path $coordinatorRoot "VERSION") -Value $Version -Encoding ASCII
Write-CoordinatorReadme -Path (Join-Path $coordinatorRoot "README-coordinator.md") -Version $Version -Commit $commit

$coordinatorWrapper = @'
#!/usr/bin/env bash
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export ACBH_VERSION="${ACBH_VERSION:-__VERSION__}"
export ACBH_BUILD_COMMIT="${ACBH_BUILD_COMMIT:-__COMMIT__}"
export ACBH_PROTOCOL_VERSION="${ACBH_PROTOCOL_VERSION:-2}"
exec node "$DIR/dist/index.js" "$@"
'@
$coordinatorWrapper = $coordinatorWrapper.Replace("__VERSION__", $Version).Replace("__COMMIT__", $commit)
Set-Content -LiteralPath (Join-Path $coordinatorRoot "coordinator") -Value $coordinatorWrapper -Encoding ASCII

$startScript = @'
#!/usr/bin/env bash
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$DIR/coordinator" "$@"
'@
Set-Content -LiteralPath (Join-Path $coordinatorRoot "start-coordinator.sh") -Value $startScript -Encoding ASCII

if (Get-Command chmod -ErrorAction SilentlyContinue) {
    & chmod +x (Join-Path $coordinatorRoot "coordinator") (Join-Path $coordinatorRoot "start-coordinator.sh")
}

foreach ($required in @("coordinator", "start-coordinator.sh", "README-coordinator.md", "build-info.json", "VERSION", "dist/index.js")) {
    if (-not (Test-Path -LiteralPath (Join-Path $coordinatorRoot $required))) {
        throw "Coordinator bundle missing $required"
    }
}
Assert-BundleHasNoSensitiveFiles -BundleRoot $coordinatorRoot

$coordinatorTar = Join-Path $resolvedOutput $coordinatorTarName
if (Test-Path -LiteralPath $coordinatorTar) {
    Remove-Item -LiteralPath $coordinatorTar -Force
}
if (-not (Get-Command tar -ErrorAction SilentlyContinue)) {
    throw "tar not found; cannot create coordinator .tar.gz bundle."
}
& tar -C $stagingRoot -czf $coordinatorTar $coordinatorBundleName
if ($LASTEXITCODE -ne 0) {
    throw "tar failed with exit code $LASTEXITCODE"
}

$artifacts = @($agentExe, $desktopExe, $windowsZip, $coordinatorTar)
foreach ($artifact in $artifacts) {
    if (-not (Test-Path -LiteralPath $artifact)) {
        throw "Expected artifact missing: $artifact"
    }
    [void](Write-Sha256File -Path $artifact)
}
Write-Sha256Sums -Directory $resolvedOutput
Remove-DirectoryIfExists -Path $stagingRoot -AllowedRoot $resolvedOutput

Write-Host "Minimal-core release artifacts prepared:"
Show-ArtifactList -Directory $resolvedOutput
