#! /bin/sh

if [ "$1" == "connect" ]; then
    echo "connect"    
    kibana_pod=`kubectl get pods -n logging | grep efk-kibana | awk '{print $1}'`
    kubectl port-forward $kibana_pod -n logging 5601:5601 &

    elastic_pod=`kubectl get pods -n logging | grep es-master-efk | awk '{print $1}'`
    kubectl port-forward $elastic_pod -n logging 9200:9200 &
else
    if [ "$1" == "reconnect" ]; then
        echo "Re-reconnect"
        ./connect_efk.sh disconnect
	./connect_efk.sh connect
    else
        echo "Disconnect"
        ps -ef | pgrep -f 5601 | xargs kill -9
        ps -ef | pgrep -f 9200 | xargs kill -9
    fi
fi

