#!/bin/bash -e
# Ensure we use Go version 1.11+
read major minor patch <<< $(go version | sed 's/go version go\([0-9]*\)\.\([0-9]*\)\.\(.*\) .*/\1 \2/')
if [[ ${major} -ne 1 || ${minor} -lt 11 ]]; then
    echo "Go 1.11+ is required (v$major.$minor.$patch is installed at `which go`)"
    exit 1;
fi

./scripts/install-protobuf.sh

go mod download
protobuf_path=$(go list -m -f '{{.Dir}}' github.com/golang/protobuf)
echo "installing protoc-gen-go..."
go install $protobuf_path/protoc-gen-go

# Current version of grpc_gateway does not support go modules, so we install it to the gopath
# TODO: Follow this issue: https://github.com/grpc-ecosystem/grpc-gateway/issues/755

#grpc_gateway_path=$(go list -m -f '{{.Dir}}' github.com/grpc-ecosystem/grpc-gateway)
#echo "installing protoc-gen-grpc-gateway"
#go install $grpc_gateway_path/protoc-gen-grpc-gateway
#
#echo "installing protoc-gen-swagger"
#go install $grpc_gateway_path/protoc-gen-swagger

echo "setting GO111MODULE=off to install some legacy deps"
GO111MODULE=off
echo "installing protoc-gen-grpc-gateway"
go get github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
echo "installing protoc-gen-swagger"
go get github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger
GO111MODULE=on

echo "setup complete 🎉"
