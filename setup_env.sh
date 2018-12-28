#!/bin/bash -e
./scripts/install-protobuf.sh

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
set +e
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger

go get -u github.com/golang/protobuf/protoc-gen-go
	GO111MODULE=off
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway

go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger
	GO111MODULE=on

set -e

	echo "setting GO111MODULE=off to install some legacy deps"
echo "installing protoc-gen-grpc-gateway"
echo "installing protoc-gen-swagger"

echo "setup complete 🎉"
