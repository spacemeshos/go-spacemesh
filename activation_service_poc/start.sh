#!/bin/bash
#
cd ..
make dockerbuild-go
cd activation_service_poc

export IMAGE=$(docker images | head -n 2 | tail -n 1 | awk '{print $3}')

TIME=$(date -u -d '2 minutes' "+%Y-%m-%dT%H:%M:%S%:z")
for file in config.standalone.client.json config.standalone.node-service.json;do
  # Run your command on each file
  jq ".genesis.\"genesis-time\" |= \"$TIME\"" "$file" > temp.json && mv temp.json "$file"
done

rm -rf /tmp/space*
docker compose up
