@echo off
set target=E:\workspace\WebSQL2

tskill WebSql

go build -o WebSql.exe main.go
del /q %target%\WebSql.exe
mv WebSql.exe %target%

cd web-src
call npm run build
del /s /q %target%\static\*
xcopy /Y /e dist %target%\static

cd ..
start /b %target%\WebSql.exe

PAUSE