import os
import pytest
import string
import random
from kubernetes import config
from kubernetes import client
from pytest_testconfig import config as testconfig



class DeploymentInfo():
    def __init__(self, dep_id=None):
        self.deployment_name = ''
        self.deployment_id = dep_id if dep_id else DeploymentInfo.random_deployment_id()
        self.pods = []

    @staticmethod
    def random_deployment_id():
        # Just alphanumeric characters
        chars = string.ascii_lowercase + string.digits
        return ''.join((random.choice(chars)) for x in range(4))


@pytest.fixture(scope='session')
def load_config():
    kube_config_var = os.getenv('KUBECONFIG', '~/.kube/config')
    kube_config_path = os.path.expanduser(kube_config_var)
    if os.path.isfile(kube_config_path):
        config.load_kube_config(kube_config_path)
    else:
        raise Exception("KUBECONFIG file not found: {0}".format(kube_config_path))


@pytest.fixture(scope='session')
def set_docker_images():
    docker_image = os.getenv('CLIENT_DOCKER_IMAGE', '')
    if docker_image:
        print("Set docker images to: {0}".format(docker_image))
        testconfig['bootstrap']['image'] = docker_image
        testconfig['client']['image'] = docker_image


@pytest.fixture(scope='session')
def set_namespace(request, load_config):

    v1 = client.CoreV1Api()

    def _setup_namespace():
        k8s_namespace = os.getenv('K8S_NAMESPACE', '')
        if k8s_namespace != '':
            testconfig['namespace'] = k8s_namespace

        print("Run tests in namespace: {0}".format(testconfig['namespace']))
        namespaces_list = [ns.metadata.name for ns in v1.list_namespace().items]
        if testconfig['namespace'] in namespaces_list:
            return

        body = client.V1Namespace()
        body.metadata = client.V1ObjectMeta(name=testconfig['namespace'])
        v1.create_namespace(body)

    def fin():
        v1.delete_namespace(name=testconfig['namespace'], body=client.V1DeleteOptions())

    request.addfinalizer(fin)
    return _setup_namespace()

@pytest.fixture(scope='session')
def init_session(request, load_config, set_namespace, set_docker_images):
    pass

@pytest.fixture(scope='module')
def bootstrap_deployment_info():
    return DeploymentInfo()


@pytest.fixture(scope='module')
def client_deployment_info():
    def _create_client_info(boot_info):
        return DeploymentInfo(boot_info.deployment_id)
    return _create_client_info
