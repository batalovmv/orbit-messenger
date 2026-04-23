@echo off
REM verify-and-push.bat — Verify then push
REM Run: .\verify-and-push.bat

echo.
echo =========================================
echo Running Saturn build verification...
echo =========================================

pwsh -ExecutionPolicy Bypass -File "%~dp0scripts\verify-go124.ps1"
if errorlevel 1 (
    echo.
    echo =========================================
    echo [ABORTED] Build verification failed
    echo =========================================
    exit /b 1
)

echo.
echo =========================================
echo [OK] All services verified!
echo =========================================
echo.

git push %*
if errorlevel 1 (
    echo [ERROR] Push failed
    exit /b 1
)

echo [OK] Pushed successfully!
exit /b 0