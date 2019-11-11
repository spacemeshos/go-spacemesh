import os
import pytest
import string
import random
from tests import pod
from kubernetes import config as k8s_config
from kubernetes import client
from pytest_testconfig import config as testconfig
from tests.misc import CoreV1ApiClient
from tests.context import Context

S = ' during'


def random_id(length):
    # Just alphanumeric characters
    chars = string.ascii_lowercase + string.digits
    return ''.join((random.choice(chars)) for x in range(length))


class DeploymentInfo:
    def __init__(self, dep_id=None):
        self.deployment_name = ''
        self.deployment_id = dep_id if dep_id else random_id(length=4)
        self.pods = []

    def __str__(self):
        return f"\n\tdeployment name: {self.deployment_name}\n\tdeployment id: {self.deployment_id}" \
               f"\n\tpods number: {len(self.pods)}"


class NetworkDeploymentInfo:
    def __init__(self, dep_id, bs_deployment_info, cl_deployment_info):
        self.deployment_name = ''
        self.deployment_id = dep_id
        self.bootstrap = bs_deployment_info
        self.clients = cl_deployment_info

    def __str__(self):
        return f"   deployment name: {self.deployment_name}\n  deployment id: {self.deployment_id}\n" \
               f"   bootstrap info: \n{self.bootstrap}\n    client info: \n{self.clients}"


@pytest.fixture(scope='session')
def load_config():
    kube_config_var = os.getenv('KUBECONFIG', '~/.kube/config')
    kube_config_path = os.path.expanduser(kube_config_var)
    print("kubeconfig file is: {0}".format(kube_config_path))
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


@pytest.fixture(scope='session')
def set_docker_images():
    docker_image = os.getenv('CLIENT_DOCKER_IMAGE', '')
    if docker_image:
        print("Set docker images to: {0}".format(docker_image))
        testconfig['bootstrap']['image'] = docker_image
        testconfig['client']['image'] = docker_image


@pytest.fixture(scope='session')
def session_id():
    return random_id(length=5)


@pytest.fixture(scope='session')
def set_namespace(request, session_id, load_config):

    v1 = CoreV1ApiClient()

    def _setup_namespace():

        if testconfig['namespace'] == '':
            testconfig['namespace'] = session_id

        print("\nRun tests in namespace: {0}".format(testconfig['namespace']))
        print("Kibana URL: http://kibana.spacemesh.io/app/kibana#/discover?_g=(refreshInterval:(pause:!t,value:0))&_a=("
              "columns:!(M),filters:!(('$state':(store:appState),meta:(alias:!n,disabled:!f,index:'8a91d9d0-c24f-11e9-9"
              "a59-a76b835079b3',key:kubernetes.namespace_name,negate:!f,params:(query:{0},type:phrase),type:phrase,val"
              "ue:{0}),query:(match:(kubernetes.namespace_name:(query:{0},type:phrase))))))"
              .format(testconfig['namespace']))
        namespaces_list = [ns.metadata.name for ns in v1.list_namespace().items]
        if testconfig['namespace'] in namespaces_list:
            return

        body = client.V1Namespace()
        body.metadata = client.V1ObjectMeta(name=testconfig['namespace'])
        v1.create_namespace(body)

    def fin():
        # On teardown we wish to report on pods that were restarted by k8s during the test
        restarted_pods = pod.check_for_restarted_pods(testconfig['namespace'])
        if restarted_pods:
            print('\n\nAttention!!! The following pods were restarted{1} test: {0}\n\n'.format(restarted_pods, S))

        if hasattr(request, 'param') and request.param == 'doNotDeleteNameSpace':
            print("\nDo not delete namespace: {0}".format(testconfig['namespace']))
        else:
            print("\nDeleting test namespace: {0}".format(testconfig['namespace']))
            v1.delete_namespace(name=testconfig['namespace'], body=client.V1DeleteOptions())

    request.addfinalizer(fin)
    return _setup_namespace()


@pytest.fixture(scope='session')
def init_session(load_config, set_namespace, set_docker_images, session_id):
    """
    init_session sets up a new testing environment using k8s with
    the given yaml config file
    :return: namespace id
    """
    return session_id
