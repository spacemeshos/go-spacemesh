@echo off
PowerShell.exe -NoProfile -ExecutionPolicy Bypass -Command "& './scripts/win/verify-protoc-gen-go.ps1'"
if %ERRORLEVEL% GTR 0 exit /B 1

FOR /F "tokens=* USEBACKQ" %%F IN (`go list -m -f {{.Dir}} github.com/grpc-ecosystem/grpc-gateway`) DO (
  SET grpc_gateway_path=%%F
)

ECHO Generating protobuf for api/pb
%USERPROFILE%\protoc-3.6.1\bin\protoc -I api\api\proto -I api\api\third_party --go_out=plugins=grpc:api api\api\spacemesh\v1\*.proto
%USERPROFILE%\protoc-3.6.1\bin\protoc -I api\api\proto -I api\api\third_party --grpc-gateway_out=logtostderr=true:api api\api\spacemesh\v1\*.proto
