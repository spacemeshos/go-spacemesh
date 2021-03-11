import re
import yaml
import time

from os import path
from urllib3.exceptions import NewConnectionError, MaxRetryError, ConnectTimeoutError
from kubernetes import client
from pytest_testconfig import config as testconfig
from kubernetes.client.rest import ApiException
from tests.misc import CoreV1ApiClient


def wait_for_pod_to_be_ready(pod_name, name_space, time_out=None):
    total_sleep_time = 0
    while True:
        resp = CoreV1ApiClient().read_namespaced_pod(name=pod_name, namespace=name_space)
        if resp.status.phase == 'Running':
            print("Total time waiting for pod {0}: {1} sec".format(pod_name, total_sleep_time))
            break
        time.sleep(1)
        total_sleep_time += 1

        if time_out and total_sleep_time > time_out:
            raise Exception("Timeout waiting to pod to be ready")


def wait_to_pod_to_be_deleted(pod_name, name_space, time_out=None):
    total_sleep_time = 0
    while True:
        try:
            _ = CoreV1ApiClient().read_namespaced_pod(name=pod_name, namespace=name_space)
        except ApiException as e:
            if e.status == 404:
                print("Total time waiting for delete pod {0}: {1} sec".format(pod_name, total_sleep_time))
                break
        time.sleep(1)
        total_sleep_time += 1

        if time_out and total_sleep_time > time_out:
            raise Exception("Timeout waiting to delete pod")


def create_pod(file_name, name_space, deployment_id=None, container_specs=None):
    timeout = 70
    with open(path.join(path.dirname(__file__), file_name)) as f:
        dep = yaml.safe_load(f)

        # Set unique deployment id
        if deployment_id:
            dep['metadata']['generateName'] += '{0}-'.format(deployment_id)

        if container_specs:
            dep = container_specs.update_deployment(dep)

        k8s_api = CoreV1ApiClient()
        resp = k8s_api.create_namespaced_pod(namespace=name_space, body=dep)
        wait_for_pod_to_be_ready(resp.metadata._name, name_space, time_out=timeout)
        return resp


def delete_pod(pod_name, name_space):
    k8s_api = CoreV1ApiClient()
    resp = None

    try:
        resp = k8s_api.delete_namespaced_pod(name=pod_name,
                                             namespace=name_space,
                                             body=client.V1DeleteOptions(propagation_policy='Foreground',
                                                                         grace_period_seconds=5))
    except ApiException as e:
        if e.status == 404:
            return resp

    wait_to_pod_to_be_deleted(pod_name, name_space)
    return resp


def check_for_restarted_pods(namespace, specific_deployment_name=''):
    pods = []
    try:
        if specific_deployment_name:
            pods = CoreV1ApiClient().list_namespaced_pod(namespace,
                                                         label_selector=(
                                                             "name={0}".format(specific_deployment_name.split('-')[1]))).items
        else:
            pods = CoreV1ApiClient().list_namespaced_pod(namespace).items
    except (NewConnectionError, MaxRetryError, ConnectTimeoutError) as e:
            print('Could not list restarted pods. API Error: {0}'.format(str(e)))
            return pods

    restarted_pods = []
    for p in pods:
        if (p.status and p.status.container_statuses and
                p.status.container_statuses[0].restart_count > 0):  # Assuming there is only 1 container per pod
            restarted_pods.append(p.metadata.name)

    return restarted_pods


def search_phrase_in_pod_log(pod_name, name_space, container_name, phrase, timeout=60, group=None, retries=3):
    sleep_interval = 0.3
    total_sleep = 0
    retries_remaining = retries
    while True:
        try:
            read_namespaced_pod_log = CoreV1ApiClient().read_namespaced_pod_log
            pod_logs = read_namespaced_pod_log(name=pod_name, namespace=name_space, container=container_name)
        except ApiException as e:
            print(f"got an exception while reading pod log:\n{e}")
            if retries_remaining <= 0:
                raise ApiException(e)
            retries_remaining -= 1
            time.sleep(sleep_interval)
            print(f"retrying, retries left:\n{retries_remaining}")
            continue

        match = re.search(phrase, pod_logs)
        if match and group:
            return match.group(group)
        elif match:
            return match
        elif not match and total_sleep < timeout:
            time.sleep(1)
            total_sleep += 1
            retries_remaining = retries
        else:
            return None
