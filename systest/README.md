# Systest

## Installation

This testing setup can run on top of any k8s installation. The instructions below uses `minikube`.

1. Install minikube: <https://minikube.sigs.k8s.io/docs/start/>

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

    chaos-mesh is used for some tests. See <https://chaos-mesh.org/docs/quick-start/> for more up-to-date instructions.

    ```bash
    curl -sSL https://mirrors.chaos-mesh.org/v2.5.1/install.sh | bash
    ```

4. Install `loki` to use grafana dashboard.

    Please follow instructions on <https://grafana.com/docs/loki/latest/installation/microservices-helm/> :

    ```bash
    helm repo add grafana https://grafana.github.io/helm-charts
    helm repo update
    helm upgrade --install loki grafana/loki-stack  --set grafana.enabled=true,prometheus.enabled=true,prometheus.alertmanager.persistentVolume.enabled=false,prometheus.server.persistentVolume.enabled=false,loki.persistence.enabled=true,loki.persistence.storageClassName=standard,loki.persistence.size=20Gi
    ```

### Using Grafana and Loki

To log in grafana dashboard, use username `admin`, and get password with the following command:

```bash
kubectl get secret loki-grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
```

Make dashboard available on `0.0.0.0:9000`:

```bash
kubectl port-forward service/loki-grafana 9000:80
```

## Build and Run

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

```bash
➜  systest git:(systest-readme) make run test_name=TestSmeshing smesher_image=go-spacemesh-dev:systest-readme-dirty
pod/systest-eef2da3d created
pod/systest-eef2da3d condition met
=== RUN   TestSmeshing
=== PAUSE TestSmeshing
=== CONT  TestSmeshing
    logger.go:130: 2022-04-17T12:13:00.308Z INFO    using {"namespace": "test-adno"}
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

```bash
➜  ~ kubectl get pods -n test-adno
NAME     READY   STATUS    RESTARTS   AGE
boot-0   1/1     Running   0          9m20s
boot-1   1/1     Running   0          9m20s
poet     0/1     Pending   0          9m13s
```

then please see more details with

```bash
➜  ~ kubectl describe pods poet -n test-adno
Name:         poet
Namespace:    test-adno
...
node.kubernetes.io/unreachable:NoExecute op=Exists for 300s
Events:
  Type     Reason            Age                 From               Message
  ----     ------            ----                ----               -------
  Warning  FailedScheduling  69s (x11 over 11m)  default-scheduler  0/1 nodes are available: 1 Insufficient cpu, 1 Insufficient memory.
```

Most likely you have insufficient CPU or memory and need to make `size` smaller in your `make run` command.
This is related to minikube setup though and shouldn't be an issue for Kubernetes cluster running in the cloud.

### Note

* If you are switching between remote and local k8s, you have to run `minikube start` before running the tests locally.
* If you did `make clean`, you will have to install `loki` again for grafana to be installed.

## Parametrizable tests

Tests are parametrized using configmap that must be created in the same namespace
where test pod is running. Name of the configmap by default will match name of the pod where
tests are running.

Developer can replace configmap that is used in tests by creating a custom one, and updating `configname` variable.

```bash
make run configname=<custom configmap> test_name=TestStepCreate
```

Alternatively it is possible to replace smesher/poet config and add more values to the config map.

Command below will update configmap to use longer epochs:

```bash
make run smesher_config=parameters/longfast/smesher.json poet_config=parameters/longfast/poet.conf test_name=TestStepCreate
```

Command below will add additional variable to configmap from env file:

```bash
echo "layers-to-check=20" >> properties.env
make run properties=properties.env test_name=TestStepCreate
```

## Longevity testing

### Manual mode

Manual mode allows to setup a cluster as a separate step and apply tests on that cluster continuously.

The first step creates a cluster with 10 nodes.

```bash
export namespace=qqq
make run test_name=TestStepCreate size=10 bootstrap=1m keep=true
```

Once that step completes user is able to apply different tasks that either modify the cluster, asserts some expectations or enables chaos conditions.

For example creating a batch of transactions:

```bash
make run test_name=TestStepTransactions
```

Or replacing a some part of nodes:

```bash
make run test_name=TestStepReplaceNodes
```

Some steps may be executed concurrently with other steps. In manual mode this must be handled by the operator, for example creating transactions and setting up a short disconnect concurrent is safe:

```bash
make run test_name=TestStepTransactions &
make run test_name=TestStepShortDisconnect & 
```

All such individual steps can be found in `systest/steps_test.go`.

### Scheduler

Individual steps may be also scheduled by any software. For simplicity the first version of scheduler is implemented in golang (see TestScheduleBasic).

It launches a test that will execute sub-tests to create transactions, add nodes, verify consistency of the mesh and that nodes are eventually synced.

```bash
export namespace=qqq
make run test_name=TestScheduleBasic size=10 bootstrap=1m keep=true
```

### Storage for longevity tests

See available storage classes using:

```bash
kubectl get sc
```

For longevity tests standard storage class is not sufficiently fast, and has low amount of allocated resources (iops and throughput). Allocated resource also depend on the disk size.

On gke for the network with a moderate load `premium-rwo` storage class with 10Gi disk size can be used. It will provision ssd disks.

```bash
export storage=premium-rwo=10Gi
```

### Schedule chaos tasks for longevity network

Chaos-mesh tasks are scheduled using native chaos-mesh api for flexibility.
After cluster was created and all pods of the cluster have spawned it is possible to apply
a predefined set of tasks using kubectl

```bash
kubectl apply -n <namespace> -f ./parameters/chaos
```

Or use chaos mesh dashboard for custom chaos tasks.
