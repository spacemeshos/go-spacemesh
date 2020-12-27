from kubernetes import config as k8s_config
import os
import requests

from kubernetes import client
from kubernetes.client.rest import ApiException
from kubernetes.stream import stream


class K8SApiClient(client.ApiClient):

    def __init__(self):
        client_config = client.Configuration()
        client_config.connection_pool_maxsize = 32
        super(K8SApiClient, self).__init__(configuration=client_config)


class CoreV1ApiClient(client.CoreV1Api):

    def __init__(self):
        super(CoreV1ApiClient, self).__init__(api_client=K8SApiClient())


def singleton(cls):
    instance = [None]

    def wrapper(*args, **kwargs):
        if instance[0] is None:
            instance[0] = cls(*args, **kwargs)
        return instance[0]

    return wrapper


@singleton
class Context:
    def __init__(self):
        k8s_context = os.getenv("KUBECONTEXT")
        if not k8s_context:
            try:
                # Get current context
                _, active_context = k8s_config.list_kube_config_contexts()
                self.k8s_context = active_context['name']
                print("Going to use Kubernetes context: {0}".format(self.k8s_context))
            except Exception as e:
                raise Exception("Unknown Context. Please check 'KUBECONTEXT' environment variable")
        else:
            self.k8s_context = k8s_context

    def get(self):
        return self.k8s_context


def load_config():
    kube_config_var = os.getenv('KUBECONFIG', '~/.kube/config')
    kube_config_path = os.path.expanduser(kube_config_var)
    print("kube config file is: {0}".format(kube_config_path))
    if os.path.isfile(kube_config_path):
        kube_config_context = Context().get()
        print("Loading config: {0} context: {1}".format(kube_config_path, kube_config_context))
        k8s_config.load_kube_config(config_file=kube_config_path, context=kube_config_context)
    else:
        # Assuming in cluster config
        try:
            print("Loading incluster config")
            k8s_config.load_incluster_config()
        except Exception as e:
            raise Exception("KUBECONFIG file not found: {0}\nException: {1}".format(kube_config_path, e))


def api_call(client_ip, data, api, namespace, port="9093", retry=3, interval=1):
    # todo: this won't work with long payloads - ( `Argument list too long` ). try port-forward ?
    res = None
    while True:
        try:
            res = stream(CoreV1ApiClient().connect_post_namespaced_pod_exec, name="curl", namespace=namespace,
                         command=["curl", "-s", "--request", "POST", "--data", data, f"http://{client_ip}:{port}/{api}"],
                         stderr=True, stdin=False, stdout=True, tty=False, _request_timeout=90)
        except ApiException as e:
            print(f"got an ApiException while streaming: {e}")
            import time
            print(f"sleeping for {interval} seconds before trying again")
            time.sleep(interval)
            retry -= 1
            if retry < 0:
                raise ApiException(e)
            continue
        else:
            break

    return res


def aws_api_call(client_ip, data, api, port="9093"):
    url = f"http://{client_ip}:{port}/{api}"
    return requests.post(url, data=data)


def get_client_lst(_namespace, label="client"):
    client_pods = CoreV1ApiClient().list_namespaced_pod(_namespace,
                                                        include_uninitialized=True,
                                                        label_selector="name={}".format(label)).items

    return client_pods


def get_clients_names_and_ips(_namespace):
    """
    return all client names and ips in the form of
    [{"name": ..., "ip": ...}, ...]

    :param _namespace: str, namespace to query

    :return: list, a list of dictionaries with name and ip

    """

    meta_dict_fmt = {"name": "", "pod_ip": ""}
    label_client = "client"

    cl_lst = get_client_lst(_namespace, label_client)
    pods_meta = []
    for cl in cl_lst:
        pod_name = cl.metadata.name
        if label_client in pod_name:
            pods_meta.append(dict(meta_dict_fmt, name=pod_name, pod_ip=cl.status.pod_ip))

    return pods_meta
