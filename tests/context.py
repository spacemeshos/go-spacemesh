import os

from elasticsearch import Elasticsearch
from kubernetes import config


def singleton(cls):
    instance = [None]

    def wrapper(*args, **kwargs):
        if instance[0] is None:
            instance[0] = cls(*args, **kwargs)
        return instance[0]

    return wrapper


@singleton
class ES:

    def __init__(self):
        ES_PASSWD = os.getenv("ES_PASSWD")
        if not ES_PASSWD:
            raise Exception("Unknown Elasticsearch password. Please check 'ES_PASSWD' environment variable")

        ctxt = Context()
        cluster_name = ctxt.get_cluster_name()
        es_url = generate_elastic_url(cluster_name)
        print("ES_PASSWD is {0}".format(ES_PASSWD))
        self.es = Elasticsearch(es_url, http_auth=("spacemesh", ES_PASSWD), port=80, timeout=90)

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
    return "http://elastic-{0}.spacemesh.io".format(cluster_name.split('-')[1])