#!/bin/sh

set -xe

export end_point='http://'$1':9093/'$3

# Example:
# kubectl exec -it curl -n sm -- curl --request POST --data '{ "data": "foo" }' $end_point

kubectl exec -it curl -n $4 -- curl --request POST --data $2 $end_point