@echo off
set APP_NAME=app

echo [Info] start building app

set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0

go version
if %errorlevel% neq 0 (
    echo [Error] failed to find go. Please install Go or check PATH.
    exit /b %errorlevel%
)

if not exist ..\bin (mkdir ..\bin & echo [Info] create ../bin)

@REM  go build -ldflags="-s -w" -o ../bin/%APP_NAME%_linux_amd64 ../cmd/server
go build -o ../bin/%APP_NAME%_linux_amd64 ../cmd/server
if %errorlevel% neq 0 (
    echo [Error] failed to build
    exit /b %errorlevel%
)
echo [Success] output at ../bin/%APP_NAME%_linux_amd64

pause