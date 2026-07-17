$ErrorActionPreference = 'Stop'
Write-Output "running gui selftest..."
& "$PSScriptRoot/acbh-desktop-gui.ps1" -SelfTest
Write-Output "selftest exit=$LASTEXITCODE"
exit $LASTEXITCODE