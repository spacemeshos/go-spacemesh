#!/bin/bash

make -C .. dockerbuild-go

export IMAGE=$(docker images | awk 'FNR == 2 { print $3 }')

TIME=$(date -u -d '2 minutes' "+%Y-%m-%dT%H:%M:%S%:z")
for file in config.standalone.client.json config.standalone.node-service.json;do
  jq ".genesis.\"genesis-time\" |= \"$TIME\"" "$file" > temp.json && mv temp.json "$file"
done

rm -rf /tmp/spacemesh*
docker compose up
