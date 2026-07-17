# Run all ACBH checks on Windows and stop on the first failure.
$ErrorActionPreference = "Stop"

function Invoke-Pnpm {
    param([Parameter(Mandatory = $true)][string[]]$PnpmArgs)

    if (Get-Command pnpm -ErrorAction SilentlyContinue) {
        & pnpm @PnpmArgs
    } elseif (Get-Command corepack -ErrorAction SilentlyContinue) {
        & corepack pnpm @PnpmArgs
    } else {
        throw "pnpm not found; install pnpm or enable Corepack"
    }
    if ($LASTEXITCODE -ne 0) {
        throw "pnpm command failed: $($PnpmArgs -join ' ')"
    }
}

Write-Host "`n-- Go vet --" -ForegroundColor Cyan
Push-Location agent
try {
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet failed" }

    Write-Host "`n-- Go tests --" -ForegroundColor Cyan
    go test ./... -count=1
    if ($LASTEXITCODE -ne 0) { throw "go test failed" }
} finally {
    Pop-Location
}

Write-Host "`n-- Coordinator build --" -ForegroundColor Cyan
Invoke-Pnpm -PnpmArgs @("--filter", "@acbh/coordinator", "build")

Write-Host "`n-- Coordinator tests --" -ForegroundColor Cyan
Invoke-Pnpm -PnpmArgs @("--filter", "@acbh/coordinator", "test")

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "  ACBH verify-all: ALL CHECKS PASSED" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
