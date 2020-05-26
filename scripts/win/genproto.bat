@echo off
PowerShell.exe -NoProfile -ExecutionPolicy Bypass -Command "& './scripts/win/verify-protoc-gen-go.ps1'"
if %ERRORLEVEL% GTR 0 exit /B 1

FOR /F "tokens=* USEBACKQ" %%F IN (`go list -m -f {{.Dir}} github.com/grpc-ecosystem/grpc-gateway`) DO (
  SET grpc_gateway_path=%%F
)

ECHO Generating protobuf for api/pb
%USERPROFILE%\protoc-3.6.1\bin\protoc -I%CD%\api\pb -I %grpc_gateway_path%\third_party\googleapis --go_out=plugins=grpc:%CD%\api\pb %CD%\api\pb\api.proto
%USERPROFILE%\protoc-3.6.1\bin\protoc -I%CD%\api\pb -I %grpc_gateway_path%\third_party\googleapis --grpc-gateway_out=logtostderr=true:%CD%\api\pb %CD%\api\pb\api.proto
%USERPROFILE%\protoc-3.6.1\bin\protoc -I%CD%\api\pb -I %grpc_gateway_path%\third_party\googleapis --swagger_out=logtostderr=true:%CD%\api\pb %CD%\api\pb\api.proto

