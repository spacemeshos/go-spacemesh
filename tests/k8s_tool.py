import os
import time
import sys
from kubernetes import config
from tests.misc import CoreV1ApiClient


def load_config():
    kube_config_var = os.getenv('KUBECONFIG', '~/.kube/config')
    kube_config_path = os.path.expanduser(kube_config_var)
    if os.path.isfile(kube_config_path):
        config.load_kube_config(kube_config_path)
    else:
        raise Exception("KUBECONFIG file not found: {0}".format(kube_config_path))


# Usage : python3.7 k8s_tool.py restarts namespace_name 10
def find_restarted_pods(namespace):
    namespaced_pods = CoreV1ApiClient().list_namespaced_pod(namespace=namespace, include_uninitialized=True).items
    pods = {}
    for pod in namespaced_pods:
        if pod is not None and pod.status is not None and pod.status.container_statuses is not None:
            for stat in pod.status.container_statuses:
                if stat.restart_count != 0:
                    pods[pod.metadata.name] = stat.restart_count
                break
    return pods

# todo,we can use find_restarted_pods in out testing code.
# todo: make this a better cli experience (help.. usage)


if __name__ == '__main__':
    print("loading config")
    load_config()
    args = sys.argv[1:]
    if args[0] == "restarts":
        if len(args) < 2:
            raise Exception("no namespace given")
        sleeptime = 5
        if len(args) >= 3:
            sleeptime = int(args[2])
        print("Checking pods every {0} sec".format(sleeptime))
        while True:
            time.sleep(sleeptime)
            pods = find_restarted_pods(args[1])
            if len(pods) > 0:
                print("Restarted!")
                for p in pods:
                    print(p + " - " + str(pods[p]))
            else:
                sys.stdout.write('.')
                sys.stdout.flush()
