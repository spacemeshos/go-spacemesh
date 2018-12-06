#!/bin/bash -e
./ci/install-protobuf.sh

echo "getting grpc-gateway..."
if [ -d "$GOPATH/src/github.com/golang/protobuf" ]; then
    pushd . > /dev/null && cd $GOPATH/src/github.com/golang/protobuf && git checkout -q master
    popd > /dev/null
fi
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger

echo "fixing protobuf version..."
pushd . > /dev/null && cd $GOPATH/src/github.com/golang/protobuf && git checkout -q v1.2.0
popd > /dev/null

echo "installing protoc-gen-go..."
go install github.com/golang/protobuf/protoc-gen-go
protoc --version

./ci/genproto.sh

echo "running govendor sync..."
go get -u github.com/kardianos/govendor
govendor sync
echo "setup complete 🎉"
