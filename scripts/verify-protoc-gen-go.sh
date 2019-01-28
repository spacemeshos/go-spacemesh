#!/bin/bash -e
# Ensure protoc-gen-go is installed
errcho() {
    RED='\033[0;31m'
    NC='\033[0m' # no color
    echo -e "${RED}$1${NC}"
}

if ! (hash protoc-gen-go 2>/dev/null) ; then
    errcho "Could not find protoc-gen-go. If you've run \`make install\`, ensure that \$GOPATH/bin is in your shell's \$PATH."
    exit 1;
fi
