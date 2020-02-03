FOR /F "tokens=* USEBACKQ" %%F IN (`protoc --version`) DO (
    SET protoc_version=%%F
)
if "%protoc_version%" == "libprotoc 3.6.1" (
    ECHO Found protoc 3.6.1 installed
) else (
    ECHO Fetching protoc...
    powershell.exe -nologo -noprofile -command "& { [Net.ServicePointManager]::SecurityProtocol = 'tls12, tls11, tls'; (new-object System.Net.WebClient).DownloadFile('https://github.com/protocolbuffers/protobuf/releases/download/v3.6.1/protoc-3.6.1-win32.zip','%TEMP%\protoc-3.6.1-win32.zip'); }"
    ECHO Extracting protoc...
    powershell.exe -nologo -noprofile -command "& { Add-Type -A 'System.IO.Compression.FileSystem'; [IO.Compression.ZipFile]::ExtractToDirectory('%TEMP%\protoc-3.6.1-win32.zip', '%USERPROFILE%\protoc-3.6.1'); }"
    for /F "tokens=2* delims= " %%f IN ('reg query HKCU\Environment /v PATH ^| findstr /i path') do set OLD_PATH=%%g
    setx PATH "%USERPROFILE%\protoc-3.6.1\bin;%OLD_PATH%"
)

