#!/bin/bash
#
cd ..
make dockerbuild-go
cd activation_service_poc

export IMAGE=$(docker images | head -n 2 | tail -n 1 | awk '{print $3}')

TIME=$(date -u -d '2 minutes' "+%Y-%m-%dT%H:%M:%S%:z")
jq ".genesis.\"genesis-time\" |= \"$TIME\"" config.standalone.client.json
jq ".genesis.\"genesis-time\" |= \"$TIME\"" config.standalone.node-service.json

rm -rf /tmp/space*
docker compose up
