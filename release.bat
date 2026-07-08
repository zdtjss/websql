@echo off
set target=D:\workspace\WebSQL2

tskill WebSql

go build -o WebSql.exe main.go
if exist %target%\WebSql.exe del /q %target%\WebSql.exe
move /Y WebSql.exe %target%

REM 部署 Agents.md（运行时 AI 指南，可无需重编译修改）
if exist Agents.md copy /Y Agents.md %target%\

cd web-src
call npm run build
if exist %target%\static del /s /q %target%\static\*
if not exist %target%\static mkdir %target%\static
xcopy /Y /E dist\* %target%\static

cd ..
start "" /min /d %target% WebSql.exe

PAUSE