#!/usr/bin/env -S bash -e
./scripts/check-go-version.sh
./scripts/install-protobuf.sh

go mod download
protobuf_path=$(go list -m -f '{{.Dir}}' github.com/golang/protobuf)
echo "installing protoc-gen-go..."
go install $protobuf_path/protoc-gen-go

# Current version of grpc_gateway does not support go modules, so we install it to the gopath
# TODO: Follow this issue: https://github.com/grpc-ecosystem/grpc-gateway/issues/755

grpc_gateway_path=$(go list -m -f '{{.Dir}}' github.com/grpc-ecosystem/grpc-gateway)
echo "installing protoc-gen-grpc-gateway"
go install $grpc_gateway_path/protoc-gen-grpc-gateway
#
echo "installing protoc-gen-swagger"
go install $grpc_gateway_path/protoc-gen-swagger

GO111MODULE=off go get golang.org/x/lint/golint # TODO: also install on Windows

echo "setup complete 🎉"
