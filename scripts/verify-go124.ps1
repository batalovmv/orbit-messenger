#!/usr/bin/env pwsh
# verify-go124.ps1 — Verify services build with Go 1.24 (Saturn compatibility)
# Run: pwsh scripts/verify-go124.ps1

$ErrorActionPreference = "Stop"
$GoVersion = "1.24"

Write-Host "=== Saturn Build Verification ===" -ForegroundColor Cyan
Write-Host "Target: Go >= $GoVersion, buildable by golang:1.24-alpine" -ForegroundColor Gray

# Check Go version
$GoInstalled = (go version) -match "go(1\.\d+)"
if (-not $GoInstalled) {
    Write-Error "Go not found"
}
$ActualVersion = $matches[1]
Write-Host "[OK] Go $ActualVersion installed" -ForegroundColor Green

# Services to verify
$Services = @("gateway", "auth", "messaging", "media", "calls", "ai", "bots", "integrations")
$Failed = @()
$Passed = @()

foreach ($svc in $Services) {
    $modPath = "services/$svc/go.mod"
    if (-not (Test-Path $modPath)) {
        Write-Host "[SKIP] $svc — no go.mod" -ForegroundColor Yellow
        continue
    }

    Write-Host "`n=== $svc ===" -ForegroundColor Cyan
    Push-Location services/$svc

    try {
        # 1. Check go directive
        $GoDirective = (Get-Content go.mod | Select-String "^go " | ForEach-Object { $_.Line -replace "go ", "" }).Trim()
        Write-Host "  go $GoDirective" -NoNewline
        if ($GoDirective -notmatch "^1\.2[0-4]") {
            Write-Host " [FAIL] — requires Go 1.25+" -ForegroundColor Red
            $Failed += $svc
            Pop-Location
            continue
        }
        Write-Host " [OK]" -ForegroundColor Green

        # 2. go mod download (what Saturn runs)
        Write-Host "  go mod download..." -NoNewline
        go mod download 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Host " [FAIL]" -ForegroundColor Red
            $Failed += $svc
            Pop-Location
            continue
        }
        Write-Host " [OK]" -ForegroundColor Green

        # 3. go build (what Saturn runs)
        Write-Host "  go build..." -NoNewline
        go build -v ./... 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Host " [FAIL]" -ForegroundColor Red
            $Failed += $svc
            Pop-Location
            continue
        }
        Write-Host " [OK]" -ForegroundColor Green

        $Passed += $svc
    }
    catch {
        Write-Host " [ERROR] $_" -ForegroundColor Red
        $Failed += $svc
    }
    Pop-Location
}

Write-Host "`n=== Results ===" -ForegroundColor Cyan
Write-Host "  Passed: $($Passed.Count)" -ForegroundColor Green -NoNewline
Write-Host "  Failed: $($Failed.Count)" -ForegroundColor $(if ($Failed.Count) { "Red" } else { "Green" })

if ($Failed.Count -gt 0) {
    Write-Host "`nFailed services: $($Failed -join ', ')" -ForegroundColor Red
    exit 1
}

Write-Host "`nAll services ready for Saturn (golang:1.24-alpine)!" -ForegroundColor Green
exit 0