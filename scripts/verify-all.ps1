# ACBH verify-all script (Windows)
# Run all project checks and tests.
# Requires: Go, pnpm, Node.js
$ErrorActionPreference = "Stop"

Write-Host "`n── Go vet ──" -ForegroundColor Cyan
Push-Location agent
go vet ./...
if ($LASTEXITCODE -ne 0) { throw "go vet failed" }

Write-Host "`n── Go tests ──" -ForegroundColor Cyan
go test ./... -count=1
if ($LASTEXITCODE -ne 0) { throw "go test failed" }
Pop-Location

Write-Host "`n── Coordinator build ──" -ForegroundColor Cyan
pnpm build:coordinator
if ($LASTEXITCODE -ne 0) { throw "pnpm build:coordinator failed" }

Write-Host "`n── Coordinator tests ──" -ForegroundColor Cyan
pnpm --filter @acbh/coordinator test
if ($LASTEXITCODE -ne 0) { throw "coordinator test failed" }

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "  ACBH verify-all: ALL CHECKS PASSED" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
