#!/usr/bin/env bash
set -e
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
if [[ `uname -s` == "FreeBSD" ]]; then
	freebsd_opts="-I/usr/local/include/"
fi

grpc_gateway_path=$(go list -m -f '{{.Dir}}' github.com/grpc-ecosystem/grpc-gateway)
googleapis_path="$grpc_gateway_path/third_party/googleapis"

echo "Generating protobuf for api/pb"
echo protoc -I. -Idevtools/include -I$googleapis_path --go_out=plugins=grpc:. api/pb/api.proto
compile -I. -Idevtools/include -I$googleapis_path $freebsd_opts --go_out=plugins=grpc:. api/pb/api.proto
compile -I. -Idevtools/include -I$googleapis_path $freebsd_opts --grpc-gateway_out=logtostderr=true:. api/pb/api.proto
compile -I. -Idevtools/include -I$googleapis_path $freebsd_opts --swagger_out=logtostderr=true:. api/pb/api.proto
