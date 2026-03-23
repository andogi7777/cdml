@echo off
cd /d %~dp0\..

echo Building cdml...
go build -o testnet\cdml.exe .\cmd\cdml
if errorlevel 1 (echo BUILD FAILED && pause && exit /b 1)

echo Starting node1...
start "node1" cmd /c "testnet\cdml.exe -config testnet\node1\config.json >> testnet\node1\node.log 2>&1"
echo Starting node2...
start "node2" cmd /c "testnet\cdml.exe -config testnet\node2\config.json >> testnet\node2\node.log 2>&1"
timeout /t 3 /nobreak >nul
echo Starting node3...
start "node3" cmd /c "testnet\cdml.exe -config testnet\node3\config.json >> testnet\node3\node.log 2>&1"
echo Starting node4...
start "node4" cmd /c "testnet\cdml.exe -config testnet\node4\config.json >> testnet\node4\node.log 2>&1"
echo Starting node5...
start "node5" cmd /c "testnet\cdml.exe -config testnet\node5\config.json >> testnet\node5\node.log 2>&1"

echo All nodes started.
echo Dashboard: http://127.0.0.1:8331/healthz
timeout /t 5 /nobreak >nul
echo.
echo Next: go run scripts\localnet\init.go
echo.
pause
