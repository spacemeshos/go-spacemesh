import os
import time

from elasticsearch import Elasticsearch
from kubernetes import config, client


ELASTIC_URL_FMT = "http://elastic-{0}.spacemesh.io"


def singleton(cls):
    instance = [None]

    def wrapper(*args, **kwargs):
        if instance[0] is None:
            instance[0] = cls(*args, **kwargs)
        return instance[0]

    return wrapper


@singleton
class ES:

    def __init__(self, namespace):
        self.namespace = namespace
        self.es_ip = self.get_elastic_ip()
        self.es = Elasticsearch(self.es_ip, port=9200, timeout=90, http_auth=('elastic', 'gavradon'))

    def get_elastic_ip(self):
        k8s_client = client.CoreV1Api()
        services = k8s_client.list_namespaced_service(namespace=self.namespace)
        for serv in services.items:
            if serv.metadata.name == 'elasticsearch-master':
                return serv.status.load_balancer.ingress[0].ip

    def get_search_api(self):
        return self.es


@singleton
class Context:
    def __init__(self):

        K8S_CONTEXT = os.getenv("KUBECONTEXT")
        if not K8S_CONTEXT:
            try:
                # Get current context
                _, active_context = config.list_kube_config_contexts()
                self.k8s_context = active_context['name']
                print("Going to use Kubernetes context: {0}".format(self.k8s_context))
            except Exception as e:
                raise Exception("Unknown Context. Please check 'KUBECONTEXT' environment variable")
        else:
            self.k8s_context = K8S_CONTEXT

    def get(self):
        return self.k8s_context

    def get_cluster_name(self):
        # example of context: gke_[project name]_[region]_[cluster name]
        return self.k8s_context.split('_')[3]


def generate_elastic_url(cluster_name):
    return ELASTIC_URL_FMT.format(cluster_name.split('-')[1])
