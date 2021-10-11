from datetime import datetime
from kubernetes import client
from kubernetes.client.rest import ApiException
import os
import time
import yaml

import tests.utils as ut

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
            raise Exception("Timeout waiting for deployment to be ready")

    return total_sleep_time


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
    file_path, filename = ut.get_filename_and_path(file_name)
    mod_file_path, is_changed = ut.duplicate_file_and_replace_phrases(file_path, filename, f"{name_space}_{filename}",
                                                                      ["(?<!_)NAMESPACE"], [name_space])
    with open(mod_file_path) as f:
        dep = yaml.safe_load(f)
        if mod_file_path != os.path.join(file_path, file_name) and is_changed:
            ut.delete_file(mod_file_path)

        # Set unique deployment id
        if deployment_id:
            dep['metadata']['generateName'] += '{0}-'.format(deployment_id)

        # Set replica size
        dep['spec']['replicas'] = replica_size
        if container_specs:
            dep = container_specs.update_deployment(dep)

        k8s_beta = client.AppsV1Api()
        resp1 = k8s_beta.create_namespaced_deployment(body=dep, namespace=name_space)
        wait_to_deployment_to_be_ready(resp1.metadata._name, name_space, time_out=time_out)

    return resp1


def delete_deployment(deployment_name, name_space):
    try:
        k8s_beta = client.AppsV1Api()
        resp = k8s_beta.delete_namespaced_deployment(name=deployment_name,
                                                     namespace=name_space,
                                                     body=client.V1DeleteOptions(propagation_policy='Foreground',
                                                                                 grace_period_seconds=5))
    except ApiException as e:
        if e.status == 404:
            return resp

    wait_to_deployment_to_be_deleted(deployment_name, name_space)
    return resp
