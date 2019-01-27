if(-not (Get-Command "protoc-gen-go" -errorAction SilentlyContinue))
{
    $host.ui.WriteErrorLine('Could not find protoc-gen-go. If you''ve run `make install`, ensure that $GOPATH\bin is in your shell''s $PATH.')
    exit 1
}
