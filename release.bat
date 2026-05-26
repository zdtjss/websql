@echo off
set target=D:\workspace\WebSQL2

tskill WebSql

go build -o WebSql.exe main.go
if exist %target%\WebSql.exe del /q %target%\WebSql.exe
move /Y WebSql.exe %target%

cd web-src
call npm run build
if exist %target%\static del /s /q %target%\static\*
if not exist %target%\static mkdir %target%\static
xcopy /Y /E dist\* %target%\static

cd ..
start /b %target%\WebSql.exe

PAUSE