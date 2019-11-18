import os
import yaml
import time
from datetime import datetime
from kubernetes import client
from kubernetes.client.rest import ApiException


def wait_to_statefulset_to_be_deleted(statefulset_name, name_space, time_out=None):
    total_sleep_time = 0
    while True:
        try:
            resp = client.AppsV1Api().read_namespaced_stateful_set(name=statefulset_name, namespace=name_space)
        except ApiException as e:
            if e.status == 404:
                print("Total time waiting for delete statefulset {0}: {1} sec".format(statefulset_name, total_sleep_time))
                break
        time.sleep(1)
        total_sleep_time += 1

        if time_out and total_sleep_time > time_out:
            raise Exception("Timeout waiting to delete statefulset")


def wait_to_statefulset_to_be_ready(statefulset_name, name_space, time_out=None):
    start = datetime.now()
    while True:
        resp = client.AppsV1Api().read_namespaced_stateful_set(name=statefulset_name, namespace=name_space)
        total_sleep_time = (datetime.now()-start).total_seconds()
        if resp.status.replicas == resp.status.ready_replicas:
            ready_replicas = resp.status.ready_replicas
            print("Total time waiting for statefulset {0} [size: {1}]: {2} sec\n".format(statefulset_name,
                                                                                       ready_replicas,
                                                                                       total_sleep_time))
            break
        print("{0}/{1} pods ready {2} sec               ".format(resp.status.ready_replicas, resp.status.replicas,
                                                                 total_sleep_time), end="\r")
        time.sleep(1)

        if time_out and total_sleep_time > time_out:
            raise Exception("Timeout waiting for statefulset to be ready")


def create_statefulset(file_name, name_space, deployment_id=None, replica_size=1, container_specs=None, time_out=None):
    resp1 = None
    with open(os.path.join(os.path.dirname(__file__), file_name)) as f:
        dep = yaml.safe_load(f)

        # Set unique deployment id
        if deployment_id:
            dep['metadata']['name'] += '-{0}'.format(deployment_id)

        # Set replica size
        dep['spec']['replicas'] = replica_size
        if container_specs:
            dep = container_specs.update_deployment(dep)

        k8s_beta = client.AppsV1Api()
        resp1 = k8s_beta.create_namespaced_stateful_set(body=dep, namespace=name_space)
        wait_to_statefulset_to_be_ready(resp1.metadata._name, name_space, time_out=time_out)
    return resp1


def delete_statefulset(statefulset_name, name_space):
    try:
        k8s_beta = client.AppsV1Api()
        resp = k8s_beta.delete_namespaced_stateful_set(name=statefulset_name,
                                                       namespace=name_space,
                                                       body=client.V1DeleteOptions(propagation_policy='Foreground',
                                                                                   grace_period_seconds=5))
    except ApiException as e:
        if e.status == 404:
            return resp

    wait_to_statefulset_to_be_deleted(statefulset_name, name_space)
    return resp
