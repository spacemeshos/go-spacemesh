import os
import pytest
import string
import random
from tests import pod
from kubernetes import config
from kubernetes import client
from pytest_testconfig import config as testconfig
from tests.misc import CoreV1ApiClient
from tests.context import Context


def random_id(length):
    # Just alphanumeric characters
    chars = string.ascii_lowercase + string.digits
    return ''.join((random.choice(chars)) for x in range(length))


class DeploymentInfo():
    def __init__(self, dep_id=None):
        self.deployment_name = ''
        self.deployment_id = dep_id if dep_id else random_id(length=4)
        self.pods = []


class NetworkDeploymentInfo():
    def __init__(self, dep_id, bs_deployment_info, cl_deployment_info):
        self.deployment_name = ''
        self.deployment_id = dep_id
        self.bootstrap = bs_deployment_info
        self.clients = cl_deployment_info


@pytest.fixture(scope='session')
def load_config():
    kube_config_var = os.getenv('KUBECONFIG', '~/.kube/config')
    kube_config_path = os.path.expanduser(kube_config_var)
    print("kubeconfig file is: {0}".format(kube_config_path))
    if os.path.isfile(kube_config_path):
        kube_config_context = Context().get()
        print("Loading config: {0} context: {1}".format(kube_config_path, kube_config_context))
        config.load_kube_config(config_file=kube_config_path, context=kube_config_context)
    else:
        # Assuming in cluster config
        try:
            print("Loading incluster config")
            config.load_incluster_config()
        except:
            raise Exception("KUBECONFIG file not found: {0}".format(kube_config_path))


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
            print('\n\nAttention!!! The following pods were restarted during test: {0}\n\n'.format(restarted_pods))

        if hasattr(request, 'param') and request.param == 'doNotDeleteNameSpace':
            print("\nDo not delete namespace: {0}".format(testconfig['namespace']))
        else:
            print("\nDeleting test namespace: {0}".format(testconfig['namespace']))
            v1.delete_namespace(name=testconfig['namespace'], body=client.V1DeleteOptions())

    request.addfinalizer(fin)
    return _setup_namespace()


@pytest.fixture(scope='session')
def init_session(request, load_config, set_namespace, set_docker_images, session_id):
    return session_id
