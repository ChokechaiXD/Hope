@echo off
setlocal

set "HOPE_EXE=%LOCALAPPDATA%\HopeHUB\bin\cortex.exe"
if not exist "%HOPE_EXE%" (
  title Hope HUB
  echo Hope HUB is not installed yet.
  echo Run the installer once, then try again.
  pause
  exit /b 1
)

powershell.exe -NoProfile -NonInteractive -WindowStyle Hidden -Command ^
  "$exe = Join-Path $env:LOCALAPPDATA 'HopeHUB\bin\cortex.exe'; Start-Process -WindowStyle Hidden -FilePath $exe -ArgumentList 'open'"

if errorlevel 1 (
  title Hope HUB
  echo Hope HUB could not be opened.
  pause
  exit /b 1
)

exit /b 0
