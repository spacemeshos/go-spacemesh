#!/usr/bin/env bash
set -e

errcho() {
    RED='\033[0;31m'
    NC='\033[0m' # no color
    echo -e "${RED}$1${NC}"
}

# Minimal Go version supported
min_major=1
min_minor=15

# Ensure Go is installed
if ! (hash go 2>/dev/null) ; then
    errcho "Could not find a Go installation, please install Go and try again."
    exit 1;
fi

read major minor <<< $(go version | sed 's/go version go\([0-9]*\)\.\([0-9]*\).*/\1 \2/')
if [[ ${major} -ne $min_major || ${minor} -lt $min_minor ]]; then
    errcho "Go ${min_major}.${min_minor}+ is required (v$major.$minor is installed at `which go`)"
    exit 1;
fi
