import os
import time
import sys
from kubernetes import config
from kubernetes import client


def load_config():
    kube_config_var = os.getenv('KUBECONFIG', '~/.kube/config')
    kube_config_path = os.path.expanduser(kube_config_var)
    if os.path.isfile(kube_config_path):
        config.load_kube_config(kube_config_path)
    else:
        raise Exception("KUBECONFIG file not found: {0}".format(kube_config_path))


def find_restarted_pods(namespace):
    namespaced_pods = client.CoreV1Api().list_namespaced_pod(namespace=namespace, include_uninitialized=True).items
    pods = {}
    for pod in namespaced_pods:
        if pod is not None and pod.status is not None and pod.status.container_statuses is not None:
            for stat in pod.status.container_statuses:
                pods[pod.metadata.name] = stat.restart_count
                if stat.restart_count != 0:
                    print(pod.metadata.name + " " + str(stat.restart_count))
                break
    return pods


if __name__ == '__main__':
    print("loading config")
    load_config()
    args = sys.argv[1:]
    if args[0] == "restarts":
        if args[1] is None:
            raise Exception("no namespace given")
        while True:
            time.sleep(5)
            find_restarted_pods(args[1])