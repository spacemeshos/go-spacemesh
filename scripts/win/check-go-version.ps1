if(-not (Get-Command "go" -errorAction SilentlyContinue))
{
    $host.ui.WriteErrorLine("Could not find a Go installation, please install Go and try again.")
    exit 1
}

$null = ((go version) -match "go version go(\d+)\.(\d+)\.([^ ]+)")
$major = $Matches[1]
$minor = $Matches[2]
$patch = $Matches[3]
$loc = (gcm go).Path

if($major -ne 1 -or $minor -lt 13)
{
    $host.ui.WriteErrorLine("Go 1.13+ is required (v$major.$minor.$patch is installed at $loc)")
    exit 1
}
