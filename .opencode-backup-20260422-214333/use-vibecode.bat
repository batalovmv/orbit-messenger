@echo off
REM SWARM MODE — opencode-swarm handles orchestration.
REM Config: .opencode/opencode-swarm.json
REM Agent selection: pick "Swarm architect" in OpenCode GUI dropdown.
pushd "%~dp0"

echo [SWARM] VIBECODE Claude + GPT keys, opencode-swarm v6.81 orchestration

popd
opencode
