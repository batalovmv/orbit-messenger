#!/usr/bin/env pwsh
# verify-go124.ps1 — Verify services build with Go 1.24 (Saturn compatibility)

$ErrorActionPreference = "Continue"
$GoVersion = "1.24"

Write-Host "=== Saturn Build Verification ===" -ForegroundColor Cyan

# Services pinned to Go 1.24 (Saturn compat).
$Services = @("gateway", "auth", "messaging", "media", "calls", "ai", "bots", "integrations")
# Exceptions allowed on Go 1.25 (documented in services/<svc>/go.mod header).
$GoAllowed = @{
    "gateway" = "^1\.2[4-5]"  # embedded nats-server/v2 v2.12.7 requires 1.25
}
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

    $pattern = if ($GoAllowed.ContainsKey($svc)) { $GoAllowed[$svc] } else { "^1\.2[0-4]" }
    if ($GoDirective -notmatch $pattern) {
        Write-Host " [FAIL] - pinned to $pattern" -ForegroundColor Red
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