@echo off
REM Switch both LOCAL agents and GLOBAL plugin agents to SEMAX variant.
pushd "%~dp0"
if not exist "agents" mkdir "agents"
del /Q "agents\*.md" >nul 2>&1
copy /Y "agents-semax\*.md" "agents\" >nul
echo [SEMAX] local agents active

copy /Y "%USERPROFILE%\.config\opencode\oh-my-opencode.semax.json" "%USERPROFILE%\.config\opencode\oh-my-opencode.json" >nul
echo [SEMAX] plugin agents active

popd
opencode
