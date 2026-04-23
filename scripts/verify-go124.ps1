#!/usr/bin/env pwsh
# verify-go124.ps1 — Verify services build with Go 1.24 (Saturn compatibility)

$ErrorActionPreference = "Continue"
$GoVersion = "1.24"

Write-Host "=== Saturn Build Verification ===" -ForegroundColor Cyan

$Services = @("gateway", "auth", "messaging", "media", "calls", "ai", "bots", "integrations")
$Failed = @()

foreach ($svc in $Services) {
    $modPath = "services/$svc/go.mod"
    if (-not (Test-Path $modPath)) {
        Write-Host "[SKIP] $svc" -ForegroundColor Yellow
        continue
    }

    Write-Host "`n=== $svc ===" -ForegroundColor Cyan
    Push-Location services/$svc

    $GoDirective = (Get-Content go.mod | Select-String "^go ").ToString().Replace("go ", "").Trim()
    Write-Host "  go $GoDirective" -NoNewline -ForegroundColor Gray

    if ($GoDirective -notmatch "^1\.2[0-4]") {
        Write-Host " [FAIL]" -ForegroundColor Red
        $Failed += $svc
        Pop-Location
        continue
    }
    Write-Host " [OK]" -ForegroundColor Green

    Write-Host "  go mod download..." -NoNewline -ForegroundColor Gray
    go mod download 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Host " [FAIL]" -ForegroundColor Red
        $Failed += $svc
        Pop-Location
        continue
    }
    Write-Host " [OK]" -ForegroundColor Green

    Write-Host "  go build..." -NoNewline -ForegroundColor Gray
    go build -v ./... 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Host " [FAIL]" -ForegroundColor Red
        $Failed += $svc
        Pop-Location
        continue
    }
    Write-Host " [OK]" -ForegroundColor Green

    Pop-Location
}

$failedCount = $Failed.Count
Write-Host "`n=== Results ===" -ForegroundColor Cyan
Write-Host "  Failed: $failedCount" -ForegroundColor $(if ($failedCount -gt 0) { "Red" } else { "Green" })

if ($failedCount -gt 0) {
    Write-Host "Failed: $($Failed -join ', ')" -ForegroundColor Red
    exit 1
}

Write-Host "All services ready!" -ForegroundColor Green
exit 0