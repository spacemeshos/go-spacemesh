#!bin/bash
res=$(find . -not -path ./vendor/ -not -path ./.git/ -not -path ./.idea/ -type d -name "pb")
#echo $res
while read -r p; do
  echo "Generating protobuf for $p"
  protoc -I/usr/local/include -I. -I$GOPATH/src -I$GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis --go_out=plugins=grpc:. *.proto
done <<< "$res"
