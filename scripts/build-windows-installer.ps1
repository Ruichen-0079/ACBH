[CmdletBinding()]
param(
    [string]$Version = "0.4.0-rc2-dev",
    [string]$OutputDirectory = "",
    [string]$InnoCompiler = ""
)

$ErrorActionPreference = "Stop"
$FrpVersion = "0.70.0"
$FrpArchiveSha256 = "8407f83429643aa3fa9590d0c87a46b1ac14660efb96e46c955a4c2802f744b0"
$FrpArchiveName = "frp_${FrpVersion}_windows_amd64.zip"
$FrpDownloadURL = "https://github.com/fatedier/frp/releases/download/v${FrpVersion}/${FrpArchiveName}"
$RepoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $RepoRoot "dist\windows" }
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
$repoPrefix = $RepoRoot.TrimEnd('\') + '\'
if (-not $OutputDirectory.StartsWith($repoPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Output directory must remain inside the repository"
}
if ($Version -notmatch '^[0-9A-Za-z][0-9A-Za-z._-]*$') { throw "Version contains unsupported characters" }

if (Test-Path $OutputDirectory) { Remove-Item -LiteralPath $OutputDirectory -Recurse -Force }
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
$TempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("acbh-windows-package-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $TempRoot | Out-Null

function Invoke-GoBuild {
    param([string]$Package, [string]$Output, [string]$LdFlags)
    $oldGOOS, $oldGOARCH, $oldCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
    try {
        $env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
        Push-Location (Join-Path $RepoRoot "agent")
        try {
            & go build -trimpath -ldflags $LdFlags -o $Output $Package
            if ($LASTEXITCODE -ne 0) { throw "go build failed for $Package" }
        } finally { Pop-Location }
    } finally {
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $oldGOOS, $oldGOARCH, $oldCGO
    }
}

function Find-InnoCompiler {
    if ($InnoCompiler) {
        if (-not (Test-Path -LiteralPath $InnoCompiler)) { throw "Inno Setup compiler not found: $InnoCompiler" }
        return [System.IO.Path]::GetFullPath($InnoCompiler)
    }
    $command = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }
    foreach ($candidate in @("$env:ProgramFiles(x86)\Inno Setup 6\ISCC.exe", "$env:ProgramFiles\Inno Setup 6\ISCC.exe", "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe")) {
        if ($candidate -and (Test-Path -LiteralPath $candidate)) { return $candidate }
    }
    throw "Inno Setup 6 compiler (ISCC.exe) is required"
}

function Copy-ModuleLicense {
    param([string]$ModulePath, [string]$LicenseFile, [string]$OutputName)
    $moduleCache = (& go env GOMODCACHE).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $moduleCache) { throw "Could not locate the Go module cache" }
    $source = Join-Path $moduleCache ($ModulePath.Replace('/', '\'))
    $source = Join-Path $source $LicenseFile
    if (-not (Test-Path -LiteralPath $source)) { throw "Dependency license not found: $source" }
    Copy-Item -LiteralPath $source -Destination (Join-Path $stage "licenses\$OutputName")
}

try {
    $archivePath = Join-Path $TempRoot $FrpArchiveName
    Invoke-WebRequest -UseBasicParsing -Uri $FrpDownloadURL -OutFile $archivePath
    $actualArchiveHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
    if ($actualArchiveHash -ne $FrpArchiveSha256) {
        throw "frp archive SHA-256 mismatch: $actualArchiveHash"
    }
    $frpExtract = Join-Path $TempRoot "frp"
    Expand-Archive -LiteralPath $archivePath -DestinationPath $frpExtract
    $frpRoot = Get-ChildItem -LiteralPath $frpExtract -Directory | Select-Object -First 1
    if (-not $frpRoot) { throw "frp archive layout is invalid" }

    $portableName = "ACBH-${Version}-windows-x64-portable"
    $stage = Join-Path $TempRoot $portableName
    New-Item -ItemType Directory -Force -Path (Join-Path $stage "licenses"), (Join-Path $stage "docs") | Out-Null

    Invoke-GoBuild -Package "." -Output (Join-Path $stage "acbh-agent.exe") -LdFlags "-s -w -X github.com/Ruichen-0079/ACBH/agent/internal/cli.Version=$Version"
    Invoke-GoBuild -Package "./cmd/acbh-launcher" -Output (Join-Path $stage "acbh-launcher.exe") -LdFlags "-s -w -H windowsgui"
    Copy-Item -LiteralPath (Join-Path $frpRoot.FullName "frpc.exe") -Destination (Join-Path $stage "frpc.exe")
    Copy-Item -LiteralPath (Join-Path $frpRoot.FullName "LICENSE") -Destination (Join-Path $stage "licenses\frp-Apache-2.0.txt")
    $goRoot = (& go env GOROOT).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $goRoot) { throw "Could not locate GOROOT" }
    Copy-Item -LiteralPath (Join-Path $goRoot "LICENSE") -Destination (Join-Path $stage "licenses\go-BSD-3-Clause.txt")
    Copy-ModuleLicense "github.com/spf13/cobra@v1.8.1" "LICENSE.txt" "cobra-Apache-2.0.txt"
    Copy-ModuleLicense "github.com/spf13/pflag@v1.0.5" "LICENSE" "pflag-BSD-3-Clause.txt"
    Copy-ModuleLicense "github.com/inconshreveable/mousetrap@v1.1.0" "LICENSE" "mousetrap-Apache-2.0.txt"
    Copy-ModuleLicense "nhooyr.io/websocket@v1.8.17" "LICENSE.txt" "websocket-ISC.txt"
    Copy-ModuleLicense "golang.org/x/sys@v0.36.0" "LICENSE" "x-sys-BSD-3-Clause.txt"
    Copy-Item -LiteralPath (Join-Path $RepoRoot "THIRD_PARTY_NOTICES.txt") -Destination $stage
    Copy-Item -LiteralPath (Join-Path $RepoRoot "docs\zh-CN\v0.4-windows-installation.md") -Destination (Join-Path $stage "docs")
    [System.IO.File]::WriteAllText((Join-Path $stage "portable.flag"), "portable`r`n", [System.Text.UTF8Encoding]::new($false))

    $sourceCommit = (& git -C $RepoRoot rev-parse HEAD).Trim()
    $files = Get-ChildItem -LiteralPath $stage -File -Recurse | Sort-Object FullName | ForEach-Object {
        [ordered]@{
            path = $_.FullName.Substring($stage.Length + 1).Replace('\', '/')
            size = $_.Length
            sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant()
        }
    }
    $manifest = [ordered]@{
        schema_version = 1
        product = "ACBH Hobby Edition"
        version = $Version
        platform = "windows"
        architecture = "x86_64"
        source_commit = $sourceCommit
        frpc = [ordered]@{
            version = $FrpVersion
            upstream_archive = $FrpArchiveName
            upstream_archive_sha256 = $FrpArchiveSha256
            license = "Apache-2.0"
        }
        runtime_dependencies = @()
        bundled_components = @(
            [ordered]@{ name = "frpc"; version = $FrpVersion; license = "Apache-2.0" },
            [ordered]@{ name = "Go standard library"; version = (& go version).Trim(); license = "BSD-3-Clause" },
            [ordered]@{ name = "github.com/spf13/cobra"; version = "v1.8.1"; license = "Apache-2.0" },
            [ordered]@{ name = "github.com/spf13/pflag"; version = "v1.0.5"; license = "BSD-3-Clause" },
            [ordered]@{ name = "github.com/inconshreveable/mousetrap"; version = "v1.1.0"; license = "Apache-2.0" },
            [ordered]@{ name = "nhooyr.io/websocket"; version = "v1.8.17"; license = "ISC" },
            [ordered]@{ name = "golang.org/x/sys"; version = "v0.36.0"; license = "BSD-3-Clause" }
        )
        files = @($files)
    }
    $manifestJSON = $manifest | ConvertTo-Json -Depth 8
    [System.IO.File]::WriteAllText((Join-Path $stage "manifest.json"), $manifestJSON + "`n", [System.Text.UTF8Encoding]::new($false))
    $manifestOutput = Join-Path $OutputDirectory "ACBH-${Version}-windows-x64-manifest.json"
    Copy-Item -LiteralPath (Join-Path $stage "manifest.json") -Destination $manifestOutput

    $portableZip = Join-Path $OutputDirectory "${portableName}.zip"
    Compress-Archive -Path (Join-Path $stage "*") -DestinationPath $portableZip -CompressionLevel Optimal

    $compiler = Find-InnoCompiler
    $issPath = Join-Path $RepoRoot "packaging\windows\acbh.iss"
    & $compiler "/DMyAppVersion=$Version" "/DSourceDir=$stage" "/DPackageOutputDir=$OutputDirectory" $issPath
    if ($LASTEXITCODE -ne 0) { throw "Inno Setup compilation failed" }

    $setupExe = Join-Path $OutputDirectory "ACBH-${Version}-windows-x64-setup.exe"
    if (-not (Test-Path -LiteralPath $setupExe)) { throw "Setup EXE was not produced" }
    $checksumTargets = @($portableZip, $setupExe, $manifestOutput)
    $checksumLines = $checksumTargets | ForEach-Object {
        "{0}  {1}" -f (Get-FileHash -Algorithm SHA256 -LiteralPath $_).Hash.ToLowerInvariant(), (Split-Path -Leaf $_)
    }
    [System.IO.File]::WriteAllLines((Join-Path $OutputDirectory "SHA256SUMS"), $checksumLines, [System.Text.UTF8Encoding]::new($false))
    Write-Host "Windows package created in $OutputDirectory"
} finally {
    $resolvedTemp = [System.IO.Path]::GetFullPath($TempRoot)
    $tempBase = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath()).TrimEnd('\') + '\'
    if ($resolvedTemp.StartsWith($tempBase, [System.StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $resolvedTemp)) {
        Remove-Item -LiteralPath $resolvedTemp -Recurse -Force
    }
}
