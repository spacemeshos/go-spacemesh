Systest
===

Installation
---

This testing setup can run on top of any k8s installation. The instructions below uses `minikube`.

1. Install minikube

https://minikube.sigs.k8s.io/docs/start/

On linux x86:

```bash
curl -LO https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64
sudo install minikube-linux-amd64 /usr/local/bin/minikube
```

2. Grant permissions for default `serviceaccount` so that it will be allowed to create namespaces by client that runs in-cluster.

```bash
kubectl create clusterrolebinding serviceaccounts-cluster-admin \
  --clusterrole=cluster-admin --group=system:serviceaccounts
```

3. Install chaos-mesh

chaos-mesh is used for some tests. See https://chaos-mesh.org/docs/quick-start/ for more up-to-date instructions.

```bash
curl -sSL https://mirrors.chaos-mesh.org/v2.1.4/install.sh | bash
```

4. Install `loki` to use grafana dashboard.

Please follow instructions on https://grafana.com/docs/loki/latest/installation/microservices-helm/ :

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update
helm upgrade --install loki grafana/loki-stack  --set grafana.enabled=true,prometheus.enabled=true,prometheus.alertmanager.persistentVolume.enabled=false,prometheus.server.persistentVolume.enabled=false,loki.persistence.enabled=true,loki.persistence.storageClassName=standard,loki.persistence.size=20Gi
```

To log in grafana dashboard, use username `admin`, and get password with the following command:

```bash
kubectl get secret loki-grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
```

Make dashboard available on `0.0.0.0:9000`:

```bash
kubectl port-forward service/loki-grafana 9000:80
```

Build and Run
---

1. Allow docker to pull images from local repository.
```bash
eval $(minikube docker-env)
````

2. Build smesher image. Under the root directory of go-spacemesh:
```bash
make dockerbuild-go
```
note the image tag. e.g. `go-spacemesh-dev:develop-dirty`

3. Build test image for `tests` module with `make docker`.
```bash
cd systest
make docker
```

4. Run tests.
```bash
make run test_name=TestSmeshing smesher_image=<image built with `make dockerbuild-go`> e.g. `smesher_image=go-spacemesh-dev:develop-dirty`
```

The command will create a pod inside your k8s cluster named `systest`. After test completes it will clean up after
itself. If you want to interrupt the test run `make clean` - it will gracefully terminate the pod allowing it to clean up the test setup.

If logs were interrupted it is always possible to re-attach to them with `make attach`.

If you see the following output for a long time (5+ minutes):
```
➜  systest git:(systest-readme) make run test_name=TestSmeshing smesher_image=go-spacemesh-dev:systest-readme-dirty
pod/systest-eef2da3d created
pod/systest-eef2da3d condition met
=== RUN   TestSmeshing
=== PAUSE TestSmeshing
=== CONT  TestSmeshing
    logger.go:130: 2022-04-17T12:13:00.308Z	INFO	using	{"namespace": "test-adno"}
```
please make sure you don't have `Pending` pods:
```bash
➜  ~ kubectl get pods -n test-adno
NAME         READY   STATUS    RESTARTS   AGE
boot-0       1/1     Running   0          52s
boot-1       1/1     Running   0          52s
poet         1/1     Running   0          48s
smesher-0    1/1     Running   0          40s
smesher-1    1/1     Running   0          40s
smesher-10   1/1     Running   0          39s
smesher-11   1/1     Running   0          39s
smesher-12   1/1     Running   0          38s
smesher-13   1/1     Running   0          38s
smesher-14   1/1     Running   0          38s
smesher-15   1/1     Running   0          38s
smesher-16   1/1     Running   0          38s
smesher-17   1/1     Running   0          37s
smesher-2    1/1     Running   0          40s
smesher-3    1/1     Running   0          40s
smesher-4    1/1     Running   0          39s
smesher-5    1/1     Running   0          39s
smesher-6    1/1     Running   0          39s
smesher-7    1/1     Running   0          39s
smesher-8    1/1     Running   0          39s
smesher-9    1/1     Running   0          39s
```
and if you do:
```
➜  ~ kubectl get pods -n test-adno
NAME     READY   STATUS    RESTARTS   AGE
boot-0   1/1     Running   0          9m20s
boot-1   1/1     Running   0          9m20s
poet     0/1     Pending   0          9m13s
```
please see more details with
```
➜  ~ kubectl describe pods poet -n test-oyzd
Name:         poet
Namespace:    test-adno
...
node.kubernetes.io/unreachable:NoExecute op=Exists for 300s
Events:
  Type     Reason            Age                 From               Message
  ----     ------            ----                ----               -------
  Warning  FailedScheduling  69s (x11 over 11m)  default-scheduler  0/1 nodes are available: 1 Insufficient cpu, 1 Insufficient memory.
```
most likely you have insufficient CPU or memory and need to make `size` smaller in your `make run` command.
This is related to minikube setup though and shouldn't be an issue for Kubernetes cluster running in the cloud.

Note
---
* If you are switching between remote and local k8s, you have to run `minikube start` before running the tests locally.
* If you did `make clean`, you will have to install `loki` again for grafana to be installed.

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
