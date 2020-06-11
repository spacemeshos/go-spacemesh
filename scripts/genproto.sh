#!/bin/bash -e
./scripts/verify-protoc-gen-go.sh

protoc=./devtools/bin/protoc
if [[ -n "$PROTOCPATH" ]]; then
	protoc=${PROTOCPATH};
fi

compile() {
        eval $protoc "$@"
}

errcho() {
    RED='\033[0;31m'
    NC='\033[0m' # no color
    echo -e "${RED}$1${NC}"
}

if [ ! -f $protoc ] ; then
    echo "Could not find protoc in $protoc. Trying to use protoc in PATH."
    protoc=protoc # try loading from PATH
    if !(hash protoc 2>/dev/null) ; then
        errcho "Could not find protoc. Try running 'make install' or setting PROTOCPATH to your protoc bin file."
        exit 1;
    fi
fi

echo "using protoc from $protoc"

echo "Generating protobuf for api"
compile -I api/api/proto -I api/api/third_party --go_out=plugins=grpc:api api/api/proto/spacemesh/v1/*.proto
compile -I api/api/proto -I api/api/third_party --grpc-gateway_out=logtostderr=true:api api/api/proto/spacemesh/v1/*.proto
