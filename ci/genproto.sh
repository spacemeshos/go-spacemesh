#!bin/bash
res=$(find . -not -path ./vendor/ -not -path ./.git/ -not -path ./.idea/ -type d -name "pb")
#echo $res
while read -r p; do
  echo "Generating protobuf for $p"
  protoc --go_out=. $p/*.proto
done <<< "$res"
