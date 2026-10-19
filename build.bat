@echo off
rem Builds dist\LocalDrop.exe (the app) and dist\LocalDrop-Setup.exe (the installer).
rem Needs Go 1.24+ from https://go.dev/dl/ . All libraries are in the "vendor" folder,
rem so no internet connection is needed.
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo [!] Go is not installed. Install it from https://go.dev/dl/ and run this again.
  pause
  exit /b 1
)

set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
set GOFLAGS=-mod=vendor
if not exist dist mkdir dist

echo [1/2] Building dist\LocalDrop.exe ...
go build -trimpath -ldflags "-s -w -H windowsgui" -o dist\LocalDrop.exe . || goto fail
copy /y dist\LocalDrop.exe installer\payload\LocalDrop.exe >nul || goto fail

echo [2/2] Building dist\LocalDrop-Setup.exe ...
go build -trimpath -ldflags "-s -w -H windowsgui" -o dist\LocalDrop-Setup.exe .\installer || goto fail

echo.
echo Done: dist\LocalDrop.exe  dist\LocalDrop-Setup.exe
pause
exit /b 0

:fail
echo [!] Build failed.
pause
exit /b 1
