[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$OutputDirectory,
    [Parameter(Mandatory = $true)][string]$Version
)

$ErrorActionPreference = "Stop"
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
$zipPath = Join-Path $OutputDirectory "ACBH-${Version}-windows-x64-portable.zip"
$setupPath = Join-Path $OutputDirectory "ACBH-${Version}-windows-x64-setup.exe"
$manifestPath = Join-Path $OutputDirectory "ACBH-${Version}-windows-x64-manifest.json"
$sumsPath = Join-Path $OutputDirectory "SHA256SUMS"
foreach ($required in @($zipPath, $setupPath, $manifestPath, $sumsPath)) {
    if (-not (Test-Path -LiteralPath $required)) { throw "Missing package artifact: $required" }
}

$declared = @{}
foreach ($line in Get-Content -LiteralPath $sumsPath) {
    if ($line -notmatch '^([a-f0-9]{64})  (.+)$') { throw "Invalid SHA256SUMS line: $line" }
    $declared[$Matches[2]] = $Matches[1]
}
foreach ($file in @($zipPath, $setupPath, $manifestPath)) {
    $name = Split-Path -Leaf $file
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $file).Hash.ToLowerInvariant()
    if ($declared[$name] -ne $actual) { throw "SHA-256 mismatch for $name" }
}

$temp = Join-Path ([System.IO.Path]::GetTempPath()) ("acbh-package-verify-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $temp | Out-Null
try {
    Expand-Archive -LiteralPath $zipPath -DestinationPath $temp
    $manifest = Get-Content -Raw -LiteralPath (Join-Path $temp "manifest.json") | ConvertFrom-Json
    if ($manifest.version -ne $Version -or $manifest.frpc.version -ne "0.70.0" -or $manifest.runtime_dependencies.Count -ne 0) {
        throw "Manifest identity or dependency declaration is invalid"
    }
    $manifestPaths = @{}
    foreach ($entry in $manifest.files) {
        if ($manifestPaths.ContainsKey($entry.path)) { throw "Duplicate manifest path: $($entry.path)" }
        $manifestPaths[$entry.path] = $true
        $file = Join-Path $temp ($entry.path.Replace('/', '\'))
        if (-not (Test-Path -LiteralPath $file)) { throw "Manifest file missing: $($entry.path)" }
        if ((Get-Item -LiteralPath $file).Length -ne $entry.size) { throw "Manifest size mismatch: $($entry.path)" }
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $file).Hash.ToLowerInvariant()
        if ($actual -ne $entry.sha256) { throw "Manifest SHA-256 mismatch: $($entry.path)" }
    }
    $actualPaths = @(Get-ChildItem -LiteralPath $temp -File -Recurse | ForEach-Object {
        $_.FullName.Substring($temp.Length + 1).Replace('\', '/')
    } | Where-Object { $_ -ne "manifest.json" })
    foreach ($path in $actualPaths) {
        if (-not $manifestPaths.ContainsKey($path)) { throw "Portable ZIP contains unmanifested file: $path" }
    }
    if ($manifestPaths.Count -ne $actualPaths.Count) { throw "Manifest and portable ZIP file counts differ" }
    $embeddedManifestHash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $temp "manifest.json")).Hash
    $externalManifestHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $manifestPath).Hash
    if ($embeddedManifestHash -ne $externalManifestHash) { throw "Embedded and external manifests differ" }
    foreach ($required in @(
        "acbh-agent.exe", "acbh-launcher.exe", "frpc.exe", "portable.flag", "THIRD_PARTY_NOTICES.txt",
        "licenses\frp-Apache-2.0.txt", "licenses\go-BSD-3-Clause.txt", "licenses\cobra-Apache-2.0.txt",
        "licenses\pflag-BSD-3-Clause.txt", "licenses\mousetrap-Apache-2.0.txt", "licenses\websocket-ISC.txt",
        "licenses\x-sys-BSD-3-Clause.txt", "docs\v0.4-windows-installation.md"
    )) {
        if (-not (Test-Path -LiteralPath (Join-Path $temp $required))) { throw "Portable ZIP missing $required" }
    }
    $names = Get-ChildItem -LiteralPath $temp -Recurse | ForEach-Object { $_.Name.ToLowerInvariant() }
    foreach ($forbidden in @("node.exe", "npm.cmd", "java.exe", "server.jar", "server.properties")) {
        if ($names -contains $forbidden) { throw "Portable ZIP contains forbidden runtime: $forbidden" }
    }
    $frpcVersion = (& (Join-Path $temp "frpc.exe") -v 2>&1 | Out-String).Trim()
    if ($frpcVersion -notmatch '0\.70\.0') { throw "Unexpected frpc version: $frpcVersion" }
    Write-Host "Windows package structure, manifest, SHA-256, and frpc version verified."
} finally {
    $resolved = [System.IO.Path]::GetFullPath($temp)
    $tempBase = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath()).TrimEnd('\') + '\'
    if ($resolved.StartsWith($tempBase, [System.StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $resolved)) {
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
