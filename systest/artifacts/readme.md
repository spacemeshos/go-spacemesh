helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

helm install grafana grafana/grafana --values ./grafana.yaml
helm install loki grafana/loki --values ./loki.yaml
helm install promtail grafana/promtail --values ./promtail.yaml

kubectl create clusterrolebinding serviceaccounts-cluster-admin --clusterrole=cluster-admin --group=system:serviceaccounts

kubectl create ns chaos-testing

helm install chaos-mesh chaos-mesh/chaos-mesh -n=chaos-testing --set chaosDaemon.runtime=containerd --set chaosDaemon.socketPath=/run/containerd/containerd.sock --version 2.1.1

make docker
make run test_name=TestSmeshing

Point ci-grafana.spacemesh.io to Grafana Ingress IP

https://ci-grafana.spacemesh.io/login
Username: admin
Password: "Get from k8s secrets"

Then get the external IP of loki and add to grafana by create a data source