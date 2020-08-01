#!/usr/bin/env bash
set -e
# Ensure we use Go installed
errcho() {
    RED='\033[0;31m'
    NC='\033[0m' # no color
    echo -e "${RED}$1${NC}"
}

# Minimial Go version supported
min_major=1
min_minor=14

if ! (hash go 2>/dev/null) ; then
    errcho "Could not find a Go installation, please install Go and try again."
    exit 1;
fi

parse_go_version() {
	# read version out of 'go version go1.X.Y arch' output into 'v'
	read _ _ v _ <<< $(go version)

	# slash out 'go'
	v=${v#go}

	# split into array by '.'
	vv=(${v//./ })

	# assign values to global vars
	major=${vv[0]};
	minor=${vv[1]};
	patch=${vv[2]};
}
parse_go_version

# Ensure Go version satisfies requirements
if [[ ${major} -ne $min_major || ${minor} -lt $min_minor ]]; then
    errcho "Go $min_major.$min_minor+ is required ($v is installed at `which go`)"
    exit 1;
fi
