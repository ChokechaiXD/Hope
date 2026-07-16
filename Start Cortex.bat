@echo off
setlocal

set "CORTEX_EXE=%LOCALAPPDATA%\Cortex\bin\cortex.exe"
if not exist "%CORTEX_EXE%" (
  title Cortex
  echo Cortex is not installed yet.
  echo Run bin\cortex.exe service install once, then try again.
  pause
  exit /b 1
)

powershell.exe -NoProfile -NonInteractive -WindowStyle Hidden -Command ^
  "$exe = Join-Path $env:LOCALAPPDATA 'Cortex\bin\cortex.exe'; Start-Process -WindowStyle Hidden -FilePath $exe -ArgumentList 'open'"

if errorlevel 1 (
  title Cortex
  echo Cortex could not be opened.
  pause
  exit /b 1
)

exit /b 0
