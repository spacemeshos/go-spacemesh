import os
import pytest
import string
import random
from kubernetes import config
from kubernetes import client
from pytest_testconfig import config as testconfig


def random_id(length):
    # Just alphanumeric characters
    chars = string.ascii_lowercase + string.digits
    return ''.join((random.choice(chars)) for x in range(length))


class DeploymentInfo():
    def __init__(self, dep_id=None):
        self.deployment_name = ''
        self.deployment_id = dep_id if dep_id else random_id(length=4)
        self.pods = []


@pytest.fixture(scope='session')
def load_config():
    kube_config_var = os.getenv('KUBECONFIG', '~/.kube/config')
    kube_config_path = os.path.expanduser(kube_config_var)
    if os.path.isfile(kube_config_path):
        config.load_kube_config(kube_config_path)
    else:
        raise Exception("KUBECONFIG file not found: {0}".format(kube_config_path))


@pytest.fixture(scope='session')
def session_id():
    return random_id(length=5)


@pytest.fixture(scope='session')
def set_namespace(request, session_id, load_config):

    v1 = client.CoreV1Api()

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
        v1.delete_namespace(name=testconfig['namespace'], body=client.V1DeleteOptions())

    request.addfinalizer(fin)
    return _setup_namespace()

