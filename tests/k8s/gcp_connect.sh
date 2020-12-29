#!/bin/sh

if [ -z $GCLOUD_KEY ]; then
    echo "Environment variable GCLOUD_KEY is not set"
    exit 1
fi

if [ -z $PROJECT_NAME ]; then
    echo "Environment variable PROJECT_NAME is not set"
    exit 1
fi

if [ -z $CLUSTER_NAME ]; then
    echo "Environment variable CLUSTER_NAME is not set"
    exit 1
fi

if [ -z $CLUSTER_ZONE ]; then
    echo "Environment variable CLUSTER_ZONE is not set"
    exit 1
fi

echo $GCLOUD_KEY | base64 --decode > spacemesh.json
gcloud auth activate-service-account --key-file spacemesh.json
gcloud config set project $PROJECT_NAME
gcloud container clusters get-credentials $CLUSTER_NAME --zone $CLUSTER_ZONE