#!/bin/bash -e
# Ensure we use Go installed
if ! (hash go 2>/dev/null) ; then
    echo "Could not find a Go installation, please install Go and try again."
    exit 1;
fi

# Ensure we use Go version 1.11+
read major minor patch <<< $(go version | sed 's/go version go\([0-9]*\)\.\([0-9]*\)\.\(.*\) .*/\1 \2 \3/')
if [[ ${major} -ne 1 || ${minor} -lt 11 ]]; then
    echo "Go 1.11+ is required (v$major.$minor.$patch is installed at `which go`)"
    exit 1;
fi
