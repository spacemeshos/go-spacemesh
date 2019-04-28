#!/bin/bash
set -xe

NAMESPACE=$1

pods=`kubectl get pods -n $NAMESPACE | grep client | awk '{print $1}'`
deployment_id=`kubectl get pods -n $NAMESPACE | grep client | awk '{print $1}' | head -n1 | cut -d'-' -f2`
deployment='deployment_'$deployment_id
mkdir -p /tmp/$deployment
for p in $pods; do
  kubectl logs $p -n $NAMESPACE > /tmp/$deployment/$p.log
done
   
