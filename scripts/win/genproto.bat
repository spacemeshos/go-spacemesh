@echo off
PowerShell.exe -NoProfile -ExecutionPolicy Bypass -Command "& './scripts/win/verify-protoc-gen-go.ps1'"
if %ERRORLEVEL% GTR 0 exit /B 1

setlocal enableextensions
FOR /F "usebackq tokens=*" %%G IN (`dir /s/b *.proto ^| find "pb"`) do (
  call :func %%G
)

%USERPROFILE%\protoc-3.6.1\bin\protoc -I%CD%\api\pb -I %GOPATH%\src\github.com\grpc-ecosystem\grpc-gateway\third_party\googleapis --grpc-gateway_out=logtostderr=true:%CD%\api\pb %CD%\api\pb\api.proto
%USERPROFILE%\protoc-3.6.1\bin\protoc -I%CD%\api\pb -I %GOPATH%\src\github.com\grpc-ecosystem\grpc-gateway\third_party\googleapis --swagger_out=logtostderr=true:%CD%\api\pb %CD%\api\pb\api.proto

goto :eof
:func
(
  ECHO Generating protobuf for %1
  %USERPROFILE%\protoc-3.6.1\bin\protoc  -I%~dp1 -I %GOPATH%\src\github.com\grpc-ecosystem\grpc-gateway\third_party\googleapis --go_out=plugins=grpc:%~dp1 %1
  exit /b
)
:eof
endlocal