@echo off
PowerShell.exe -NoProfile -ExecutionPolicy Bypass -Command "& './scripts/win/check-go-version.ps1'"
if %ERRORLEVEL% GTR 0 exit /B 1

