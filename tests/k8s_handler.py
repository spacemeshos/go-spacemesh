from datetime import datetime
from kubernetes import client
from kubernetes.client.rest import ApiException
import os
import time
import yaml

from tests import config as conf
import tests.utils as ut


def remove_clusterrole_binding(shipper_name, crb_name):
    # remove clusterrolebind
    k8s_client = client.RbacAuthorizationV1Api()
    try:
        k8s_client.delete_cluster_role_binding(crb_name)
        print(f"\nsuccessfully deleted: {crb_name}")
    except Exception as e:
        print(f"\n{shipper_name} cluster role binding deletion has failed, please manually delete {crb_name}:")
        print(f"kubectl delete clusterrolebinding {crb_name}")


def filebeat_teardown(namespace):
    # remove clusterrolebind
    # TODO: find a solution for sharing the name both here and in the kube object
    crb_name = f"filebeat-cluster-role-binding-{namespace}"
    remove_clusterrole_binding("filebeat", crb_name)


def fluent_bit_teardown(namespace):
    # remove clusterrolebind
    # TODO: find a solution for sharing the name both here and in the kube object
    crb_name = f"fluent-bit-clusterrole-binding-{namespace}"
    remove_clusterrole_binding("fluent-bit", crb_name)


def add_elastic_cluster(namespace):
    print("\nDeploying ElasticSearch\n")
    add_deployment_dir(namespace, conf.ELASTIC_CONF_DIR)


def add_filebeat_cluster(namespace):
    print("\nDeploying FileBeat\n")
    add_deployment_dir(namespace, conf.FILEBEAT_CONF_DIR)


def add_fluent_bit_cluster(namespace):
    print("\nDeploying Fluent-bit\n")
    add_deployment_dir(namespace, conf.FLUENT_BIT_CONF_DIR)


def add_kibana_cluster(namespace):
    print("\nDeploying Kibana\n")
    add_deployment_dir(namespace, conf.KIBANA_CONF_DIR)


def add_logstash_cluster(namespace):
    print("\nDeploying LogStash\n")
    add_deployment_dir(namespace, conf.LOGSTASH_CONF_DIR)


def add_deployment_dir(namespace, dir_path, delete=False):
    with open(os.path.join(dir_path, 'dep_order.txt')) as f:
        dep_order = f.readline()
        dep_lst = [x.strip() for x in dep_order.split(',')]
        print(dep_lst)

    phrases_to_replace = ["(?<!_)NAMESPACE", "REP_ES_USER", "REP_ES_PASS"]
    values_for_replacement = [namespace, conf.ES_USER_LOCAL, conf.ES_PASS_LOCAL]
    for filename in dep_lst:
        # replace all phrases with the actual values if exists
        modified_file_path, is_change = ut.duplicate_file_and_replace_phrases(
            dir_path, filename, f"{namespace}_{filename}", phrases_to_replace, values_for_replacement
        )
        print(f"applying file: {filename}")
        with open(modified_file_path) as f:
            dep = yaml.safe_load(f)
            if modified_file_path != os.path.join(dir_path, filename) and is_change:
                # remove modified file
                ut.delete_file(modified_file_path)

            name = dep["metadata"]["name"]
            if dep['kind'] == 'StatefulSet':
                k8s_client = client.AppsV1Api()
                if not delete:
                    k8s_client.create_namespaced_stateful_set(body=dep, namespace=namespace)
                else:
                    k8s_client.delete_namespaced_stateful_set(name=name, namespace=namespace)
            elif dep['kind'] == 'DaemonSet':
                k8s_client = client.AppsV1Api()
                k8s_client.create_namespaced_daemon_set(body=dep, namespace=namespace)
            elif dep['kind'] == 'Deployment':
                k8s_client = client.AppsV1Api()
                k8s_client.create_namespaced_deployment(body=dep, namespace=namespace)
            elif dep['kind'] == 'Service':
                try:
                    k8s_client = client.CoreV1Api()
                    k8s_client.create_namespaced_service(body=dep, namespace=namespace)
                except ApiException as e:
                    if e.status == 409:
                        print(f"Service exists: {dep['metadata']['name']}")
                        continue
                    raise e
            elif dep['kind'] == 'PodDisruptionBudget':
                k8s_client = client.PolicyV1beta1Api()
                k8s_client.create_namespaced_pod_disruption_budget(body=dep, namespace=namespace)
            elif dep["kind"] == 'Role':
                k8s_client = client.RbacAuthorizationV1Api()
                k8s_client.create_namespaced_role(body=dep, namespace=namespace)
            elif dep["kind"] == 'ClusterRole':
                try:
                    k8s_client = client.RbacAuthorizationV1Api()
                    k8s_client.create_cluster_role(body=dep)
                except ApiException as e:
                    if e.status == 409:
                        print(f"cluster role already exists")
                        continue
                    raise e
            elif dep["kind"] == 'RoleBinding':
                k8s_client = client.RbacAuthorizationV1Api()
                dep["subjects"][0]["namespace"] = namespace
                k8s_client.create_namespaced_role_binding(body=dep, namespace=namespace)
            elif dep["kind"] == 'ClusterRoleBinding':
                k8s_client = client.RbacAuthorizationV1Api()
                try:
                    k8s_client.create_cluster_role_binding(body=dep)
                except ApiException as e:
                    if e.status == 409:
                        print(f"cluster role binding already exists")
                        continue
                    raise e
            elif dep["kind"] == 'ConfigMap':
                k8s_client = client.CoreV1Api()
                k8s_client.create_namespaced_config_map(body=dep, namespace=namespace)
            elif dep["kind"] == 'ServiceAccount':
                k8s_client = client.CoreV1Api()
                k8s_client.create_namespaced_service_account(body=dep, namespace=namespace)

    print("\nDone\n")


def remove_deployment_dir(namespace, dir_path):
    with open(os.path.join(dir_path, 'dep_order.txt')) as f:
        dep_order = f.readline()
        dep_lst = [x.strip() for x in dep_order.split(',')]
        print(dep_lst)

    for filename in dep_lst:
        print(f"deleting {filename}")
        with open(os.path.join(dir_path, filename)) as f:
            dep = yaml.safe_load(f)
            name = dep["metadata"]["name"]
            if dep['kind'] == 'StatefulSet':
                k8s_client = client.AppsV1Api()
                k8s_client.delete_namespaced_stateful_set(name=name, namespace=namespace)
            elif dep['kind'] == 'DaemonSet':
                k8s_client = client.AppsV1Api()
                k8s_client.delete_namespaced_daemon_set(name=name, namespace=namespace)
            elif dep['kind'] == 'Deployment':
                k8s_client = client.AppsV1Api()
                k8s_client.delete_namespaced_deployment(name=name, namespace=namespace)
            elif dep['kind'] == 'Service':
                k8s_client = client.CoreV1Api()
                k8s_client.delete_namespaced_service(name=name, namespace=namespace, grace_period_seconds=0)
                delete_func = k8s_client.delete_namespaced_service
                list_func = k8s_client.list_namespaced_service
                wait_for_namespaced_deletion(name, namespace, delete_func, list_func)
            elif dep['kind'] == 'PodDisruptionBudget':
                k8s_client = client.PolicyV1beta1Api()
                k8s_client.delete_namespaced_pod_disruption_budget(name=name, namespace=namespace)
            elif dep["kind"] == 'Role':
                k8s_client = client.RbacAuthorizationV1Api()
                k8s_client.delete_namespaced_role(name=name, namespace=namespace)
            elif dep["kind"] == 'RoleBinding':
                k8s_client = client.RbacAuthorizationV1Api()
                k8s_client.delete_namespaced_role_binding(name=name, namespace=namespace)
            elif dep["kind"] == 'ClusterRoleBinding':
                k8s_client = client.RbacAuthorizationV1Api()
                k8s_client.delete_cluster_role_binding(name=name)
            elif dep["kind"] == 'ConfigMap':
                k8s_client = client.CoreV1Api()
                k8s_client.delete_namespaced_config_map(name=name, namespace=namespace)
            elif dep["kind"] == 'ServiceAccount':
                k8s_client = client.CoreV1Api()
                k8s_client.delete_namespaced_service_account(name=name, namespace=namespace)

    print("\nDone\n")


def wait_for_namespaced_deletion(name, namespace, deletion_func, list_func, timeout=15):
    deleted = False
    orig_timeout = timeout
    while not deleted:
        # find by name and delete requested item
        for item in list_func(namespace).items:
            if item.metadata.name == name:
                if timeout < 0:
                    raise TimeoutError(f"{orig_timeout} was not enough for deleting item:\n{item}\n")
                deletion_func(name=name, namespace=namespace)
                print(f"service {name} was not deleted, retrying")
                time.sleep(1)
                timeout -= 1
        # validate item was deleted
        for item in list_func(namespace).items:
            deleted = True
            if item.metadata.name == name:
                deleted = False
    return deleted


def wait_for_daemonset_to_be_ready(name, namespace, timeout=None):
    wait_for_to_be_ready("daemonset", name, namespace, timeout=timeout)


def resolve_read_status_func(obj_name):
    if obj_name == "daemonset":
        return client.AppsV1Api().read_namespaced_daemon_set_status
    else:
        raise ValueError(f"resolve_read_status_func: {obj_name} is not a valid value")


def wait_for_to_be_ready(obj_name, name, namespace, timeout=None):
    start = datetime.now()
    while True:
        read_func = resolve_read_status_func(obj_name)
        resp = read_func(name=name, namespace=namespace)
        total_sleep_time = (datetime.now()-start).total_seconds()
        number_ready = resp.status.number_ready
        updated_number_scheduled = resp.status.updated_number_scheduled
        if number_ready and updated_number_scheduled and number_ready == updated_number_scheduled:
            print("Total time waiting for {3} {0} [size: {1}]: {2} sec".format(name, number_ready, total_sleep_time,
                                                                               obj_name))
            break
        print("{0}/{1} pods ready {2} sec               ".format(number_ready, updated_number_scheduled, total_sleep_time), end="\r")
        time.sleep(1)

        if timeout and total_sleep_time > timeout:
            raise Exception(f"Timeout waiting for {obj_name} to be ready")

