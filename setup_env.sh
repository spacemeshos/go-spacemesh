#!/bin/bash
./ci/install-protobuf.sh

go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger

rm -rf $GOPATH/src/github.com/golang/protobuf
git clone -b v1.2.0 https://github.com/golang/protobuf $GOPATH/src/github.com/golang/protobuf

go install github.com/golang/protobuf/protoc-gen-go
protoc --version

./ci/genproto.sh

go get -u github.com/kardianos/govendor
govendor sync
