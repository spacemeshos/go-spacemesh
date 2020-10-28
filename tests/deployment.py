from datetime import datetime
import os
import time
import yaml
from kubernetes import client
from kubernetes.client.rest import ApiException
from tests import config as conf


""" k8s deployment api file for stateless deployments (opposite to statefulset.py) """


def wait_to_deployment_to_be_deleted(deployment_name, name_space, time_out=None):
    total_sleep_time = 0
    while True:
        try:
            resp = client.AppsV1Api().read_namespaced_deployment(name=deployment_name, namespace=name_space)
        except ApiException as e:
            if e.status == 404:
                print("Total time waiting for delete deployment {0}: {1} sec".format(deployment_name, total_sleep_time))
                break
        time.sleep(1)
        total_sleep_time += 1

        if time_out and total_sleep_time > time_out:
            raise Exception("Timeout waiting to delete deployment")


def wait_to_deployment_to_be_ready(deployment_name, name_space, time_out=None):
    start = datetime.now()
    while True:
        resp = client.AppsV1Api().read_namespaced_deployment_status(name=deployment_name, namespace=name_space)
        total_sleep_time = (datetime.now()-start).total_seconds()
        if resp.status.ready_replicas and resp.status.replicas == resp.status.ready_replicas:
            ready_replicas = resp.status.ready_replicas
            print("Total time waiting for deployment {0} [size: {1}]: {2} sec".format(deployment_name,
                                                                                      ready_replicas,
                                                                                      total_sleep_time))
            break
        print("{0}/{1} pods ready {2} sec               ".format(resp.status.available_replicas, resp.status.replicas, total_sleep_time), end="\r")
        time.sleep(1)

        if time_out and total_sleep_time > time_out:
            raise Exception("Timeout waiting to deployment to be ready")


def wait_for_service_to_be_ready(deployment_name, name_space, time_out=None):
    start = datetime.now()
    while True:
        resp = client.CoreV1Api().read_namespaced_service_status_with_http_info(name=deployment_name, namespace=name_space)
        total_sleep_time = (datetime.now()-start).total_seconds()
        if resp[1] == 200:
            print(f"Total time waiting for service {deployment_name}: {total_sleep_time} sec")
            break
        print(f"{total_sleep_time} sec  ", end="\r")
        time.sleep(1)

        if time_out and total_sleep_time > time_out:
            raise Exception("Timeout waiting to deployment to be ready")


def create_deployment(file_name, name_space, deployment_id=None, replica_size=1, container_specs=None, time_out=None):
    with open(os.path.join(os.path.dirname(__file__), file_name)) as f:
        dep = yaml.safe_load(f)

        # Set unique deployment id
        if deployment_id:
            dep['metadata']['generateName'] += '{0}-'.format(deployment_id)

        # Set replica size
        dep['spec']['replicas'] = replica_size
        if container_specs:
            dep = container_specs.update_deployment(dep)

        k8s_beta = client.ExtensionsV1beta1Api()
        resp1 = k8s_beta.create_namespaced_deployment(body=dep, namespace=name_space)
        wait_to_deployment_to_be_ready(resp1.metadata._name, name_space, time_out=time_out)
        return resp1


def delete_deployment(deployment_name, name_space):
    try:
        k8s_beta = client.ExtensionsV1beta1Api()
        resp = k8s_beta.delete_namespaced_deployment(name=deployment_name,
                                                     namespace=name_space,
                                                     body=client.V1DeleteOptions(propagation_policy='Foreground',
                                                                                 grace_period_seconds=5))
    except ApiException as e:
        if e.status == 404:
            return resp

    wait_to_deployment_to_be_deleted(deployment_name, name_space)
    return resp


def filebeat_teardown(namespace):
    # remove clusterrolebind
    k8s_client = client.RbacAuthorizationV1beta1Api()
    # TODO: find a solution for sharing the name both here and in the kube object
    crb_name = f"filebeat-cluster-role-binding-{namespace}"
    k8s_client.delete_cluster_role_binding(crb_name)


def add_elastic_cluster(namespace):
    print("Deploying ElasticSearch")
    add_deployment_dir(namespace, conf.ELASTIC_CONF_DIR)


def add_filebeat_cluster(namespace):
    print("Deploying FileBeat")
    add_deployment_dir(namespace, conf.FILEBEAT_CONF_DIR)


def add_kibana_cluster(namespace):
    print("Deploying Kibana")
    add_deployment_dir(namespace, conf.KIBANA_CONF_DIR)


def add_logstash_cluster(namespace):
    print("Deploying LogStash")
    add_deployment_dir(namespace, conf.LOGSTASH_CONF_DIR)


def add_deployment_dir(namespace, dir_path, timeout=None):
    with open(os.path.join(dir_path, 'dep_order.txt')) as f:
        dep_order = f.readline()
        dep_lst = [x.strip() for x in dep_order.split(',')]
        print(dep_lst)

    for filename in dep_lst:
        print(f"applying file: {filename}")
        with open(os.path.join(dir_path, filename)) as f:
            dep = yaml.safe_load(f)

            if 'metadata' in dep:
                dep['metadata']['namespace'] = namespace

            if dep['kind'] == 'StatefulSet':
                k8s_client = client.AppsV1Api()
                k8s_client.create_namespaced_stateful_set(body=dep, namespace=namespace)
            if dep['kind'] == 'DaemonSet':
                k8s_client = client.AppsV1Api()
                k8s_client.create_namespaced_daemon_set(body=dep, namespace=namespace)
            if dep['kind'] == 'Deployment':
                k8s_client = client.AppsV1Api()
                k8s_client.create_namespaced_deployment(body=dep, namespace=namespace)
            elif dep['kind'] == 'Service':
                k8s_client = client.CoreV1Api()
                k8s_client.create_namespaced_service(body=dep, namespace=namespace)
            elif dep['kind'] == 'PodDisruptionBudget':
                k8s_client = client.PolicyV1beta1Api()
                k8s_client.create_namespaced_pod_disruption_budget(body=dep, namespace=namespace)
            elif dep["kind"] == 'Role':
                k8s_client = client.RbacAuthorizationV1beta1Api()
                k8s_client.create_namespaced_role(body=dep, namespace=namespace)
            elif dep["kind"] == 'ClusterRole':
                try:
                    k8s_client = client.RbacAuthorizationV1beta1Api()
                    k8s_client.create_cluster_role(body=dep)
                except ApiException as e:
                    if e.status == 409:
                        print(f"cluster role already exists")
                        continue
                    raise e
            elif dep["kind"] == 'RoleBinding':
                k8s_client = client.RbacAuthorizationV1beta1Api()
                dep["subjects"][0]["namespace"] = namespace
                k8s_client.create_namespaced_role_binding(body=dep, namespace=namespace)
            elif dep["kind"] == 'ClusterRoleBinding':
                k8s_client = client.RbacAuthorizationV1beta1Api()
                # TODO: move this to a function
                dep["metadata"]["name"] = dep["metadata"]["name"].replace("NAMESPACE", namespace)
                dep["subjects"][0]["namespace"] = namespace
                k8s_client.create_cluster_role_binding(body=dep)
            elif dep["kind"] == 'ConfigMap':
                k8s_client = client.CoreV1Api()
                k8s_client.create_namespaced_config_map(body=dep, namespace=namespace)
            elif dep["kind"] == 'ServiceAccount':
                k8s_client = client.CoreV1Api()
                k8s_client.create_namespaced_service_account(body=dep, namespace=namespace)
