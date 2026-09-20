@echo off
set APP_NAME=app

echo [Info] start building app

set GOOS=windows
set GOARCH=amd64

go version
if %errorlevel% neq 0 (
    echo [Error] failed to find go. Please install Go or check PATH.
    exit /b %errorlevel%
)

if not exist ..\bin (mkdir ..\bin & echo [Info] create ../bin)

go build -o ..\bin\%APP_NAME%_windows_amd64.exe ..\cmd\server
if %errorlevel% neq 0 (
    echo [Error] failed to build
    exit /b %errorlevel%
)
echo [Success] output at ..\bin\%APP_NAME%_windows_amd64

pause