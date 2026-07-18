@echo off
setlocal
title Install Hope HUB

set "SOURCE_EXE=%~dp0bin\cortex.exe"
set "DATA_DIR=%LOCALAPPDATA%\HopeHUB"
set "HERMES_HOME=%LOCALAPPDATA%\hermes"

if not exist "%SOURCE_EXE%" (
  echo Hope HUB executable was not found at:
  echo %SOURCE_EXE%
  echo Build or download Hope HUB first, then run this installer again.
  pause
  exit /b 1
)

if not exist "%DATA_DIR%\config.json" (
  "%SOURCE_EXE%" init
  if errorlevel 1 goto :failed
)

"%SOURCE_EXE%" dashboard pin --off
if errorlevel 1 goto :failed

if exist "%HERMES_HOME%" (
  "%SOURCE_EXE%" connector sync hermes --home "%HERMES_HOME%"
  if errorlevel 1 goto :failed
)

"%SOURCE_EXE%" service install
if errorlevel 1 goto :failed
"%SOURCE_EXE%" service start
if errorlevel 1 goto :failed
"%SOURCE_EXE%" open
if errorlevel 1 goto :failed
exit /b 0

:failed
echo.
echo Hope HUB installation did not finish. No external service was removed.
pause
exit /b 1
