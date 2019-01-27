#!/bin/bash -e
./scripts/verify-protoc-gen-go.sh

protobuf_directories=$(find . -not -path ./.git/ -not -path ./.idea/ -type d -name "pb")
grpc_gateway_path=$(go list -m -f '{{.Dir}}' github.com/grpc-ecosystem/grpc-gateway)
googleapis_path="$grpc_gateway_path/third_party/googleapis"

while read -r p; do
  echo "Generating protobuf for $p"
  protoc -I. -I$googleapis_path --go_out=plugins=grpc:. $p/*.proto
done <<< "$protobuf_directories"

protoc -I. -I$googleapis_path --grpc-gateway_out=logtostderr=true:. api/pb/api.proto
protoc -I. -I$googleapis_path --swagger_out=logtostderr=true:. api/pb/api.proto
