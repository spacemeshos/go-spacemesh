#!/bin/bash -e
./scripts/check-go-version.sh

go mod download

# Current version of grpc_gateway does not support go modules, so we install it to the gopath
# TODO: Follow this issue: https://github.com/grpc-ecosystem/grpc-gateway/issues/755

GO111MODULE=off go get golang.org/x/lint/golint # TODO: also install on Windows

echo "setup complete 🎉"
