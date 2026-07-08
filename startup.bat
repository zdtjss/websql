@ECHO OFF
cd /d "%~dp0"
tskill WebSql >nul 2>&1
start "" /min cmd /c "WebSql.exe"
start http://localhost