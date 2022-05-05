Systest
===

Setup
---

This testing setup can run on top of any k8s installation. For local we recommend minikube. See intrusctions below how to set it up.

N.B. We pipe curl output into bash for brevity's sake. Always check scripts before executing.

1. Install kubectl, minikube, and helm

https://kubernetes.io/docs/tasks/tools/ \
https://minikube.sigs.k8s.io/docs/start/ \
https://helm.sh/docs/intro/install/

On linux x86:

```bash
curl -LO https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64
sudo install minikube-linux-amd64 /usr/local/bin/minikube

curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
```

2. Start minikube, and grant permissions for default serviceaccount so that it will be allowed to create namespaces by client that runs in-cluster.

```bash
minikube start
kubectl create clusterrolebinding serviceaccounts-cluster-admin \
  --clusterrole=cluster-admin --group=system:serviceaccounts
```

3. Install chaos-mesh

https://chaos-mesh.org/docs/quick-start/

```bash
curl -sSL https://mirrors.chaos-mesh.org/v2.1.2/install.sh | bash
```

4. Install logging infra on minkube cluster

Follow instructions https://grafana.com/docs/loki/latest/installation/microservices-helm/.

```bash
helm repo add grafana https://grafana.github.io/helm-charts

helm repo update

helm upgrade --install loki grafana/loki-stack  --set grafana.enabled=true,prometheus.enabled=true,prometheus.alertmanager.persistentVolume.enabled=false,prometheus.server.persistentVolume.enabled=false,loki.persistence.enabled=true,loki.persistence.storageClassName=standard,loki.persistence.size=20Gi
```

Get password with, username is `admin`:

```bash
kubectl get secret loki-grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
```

Make dashboard available on `0.0.0.0:9000`:

```bash
kubectl port-forward service/loki-grafana 9000:80 > /dev/null 2>&1 & 
```

Run Tests
---
From the `systest` directory:

1. Build test image for `tests` module with `make docker` .

2. `make run test_name=TestSmeshing node_selector=minikube.k8s.io/name=minikube`

If your minikube node runs out of CPU, you can decrease the number of spacemesh nodes by setting the `size` parameter to something less than the default in the [Makefile](./Makefile)

`make run test_name=TestSmeshing node_selector=minikube.k8s.io/name=minikube size=4`


The command will create a pod inside your k8s cluster named `systest`. After test completes it will cleanup after
itself. If you want to interrupt the test run `make clean` - it will gracefully terminate the pod allowing it to cleanup the test setup.

If logs were interrupted it is always possible to continue watching them with `make watch`.

Testing approach
---

Following list covers only tests that require to run some form of the network, it doesn't cover model based tests, fuzzing and any other in-memory testing that is required.

- sanity tests
  regular system tests that validate that every node received rewards from hare participation, that transactions are applied in the same order, no unexpected partitions caused by different beacon, new nodes can be synced, etc.

  must be done using API without log parsing or any weird stuff.

  requirements: cluster automation, API 

- longevity and crash-recovery testing
  the purpose of the tests is to consistently validate that network performs all its functions in the realistic environment.

  for example instead of 20us latency nodes should have latency close to 50-200ms. periodically some nodes may be disconnected temporarily. longer partitions will exist as well, and nodes are expected to recover from them gracefully. some clients will experience os or hardware failures and node is expected to recover.

  additionally we may do more concrete crash-recovery testing, with [failure points](https://github.com/pingcap/failpoint). 

  in this case we will also want to peek inside the node performance, and for that we will have to validate metrics. also we will want to have much more nuanced validation, such as in jepsen. we can do it by collecting necessary history and verifying it with [porcupine](https://github.com/anishathalye/porcupine) or [elle](https://github.com/pingcap/tipocket/tree/master/pkg/elle)/[elle paper](https://raw.githubusercontent.com/jepsen-io/elle/master/paper/elle.pdf) for more details.  

  this kind of tests will run for weeks instead of on-commit basis.

  requirements: cluster automation, API, observability, chaos tooling

- byzantine recovery testing 
  this kind of testing will require significant effort, and should be mostly done in-memory using other techniques (model based tests, specifically crafted unit tests).

  some test cases can be implemented by swapping implementation of the correct protocol with byzantine driver, or using other techniques. one low-effort example that provides good coverage is a [twins](https://arxiv.org/abs/2004.10617) method, which requires creating multiple nodes with the same signing key.

  requirements: cluster automation, API, observability, chaos tooling, different client implementations  