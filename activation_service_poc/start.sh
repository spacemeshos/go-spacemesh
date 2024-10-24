#!/bin/bash
#
cd ..
make dockerbuild-go
cd activation_service_poc

IMAGE=$(docker images | head -n 2 | tail -n 1 | awk '{print $3}')
sed -i "s/image.*/image:\ $IMAGE/g" docker-compose.yml
TIME=$(date -u -d '1 minutes' "+%Y-%m-%dT%H:%M:%S%:z")
sed -i "s/\"genesis-time\".*/\"genesis-time\"\:\"$TIME\"/g" config.standalone.client.json
sed -i "s/\"genesis-time\".*/\"genesis-time\"\:\"$TIME\"/g" config.standalone.node-service.json

rm -rf /tmp/space*
docker compose up
