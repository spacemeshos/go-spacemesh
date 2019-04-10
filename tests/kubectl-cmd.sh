#!/bin/sh

set -xe

export end_point='http://'$1':9090/'$3
echo $2
#kubectl run --quiet curl-isaac --image=tutum/curl -i --tty --restart=Never --rm -n sm -- curl --request POST --data '{ "data": "foo" }' $end_point
kubectl run --quiet curl-isaac --image=tutum/curl -i --tty --restart=Never --rm -n $4 -- curl --request POST --data $2 $end_point
