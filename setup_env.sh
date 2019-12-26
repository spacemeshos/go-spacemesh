#!/bin/bash -e
./scripts/check-go-version.sh
./scripts/install-protobuf.sh

go mod download
protobuf_path=$(go list -m -f '{{.Dir}}' github.com/golang/protobuf)
echo "installing protoc-gen-go..."
go install $protobuf_path/protoc-gen-go

# Current version of grpc_gateway does not support go models, so we install it to the gopath
# TODO: Follow this issue: https://github.com/grpc-ecosystem/grpc-gateway/issues/755

grpc_gateway_path=$(go list -m -f '{{.Dir}}' github.com/grpc-ecosystem/grpc-gateway)
echo "installing protoc-gen-grpc-gateway"
go install $grpc_gateway_path/protoc-gen-grpc-gateway
#
echo "installing protoc-gen-swagger"
go install $grpc_gateway_path/protoc-gen-swagger

#echo "setting GO111MODULE=off to install some legacy deps"
#GO111MODULE=off
#echo "installing protoc-gen-grpc-gateway"
#go get github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
#echo "installing protoc-gen-swagger"
#go get github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger
#GO111MODULE=on

echo "setup complete 🎉"
