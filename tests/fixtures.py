import os
import pytest
import string
import random
from kubernetes import config


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


@pytest.fixture(scope='module')
def load_config():
    kube_config_var = os.getenv('KUBECONFIG', '~/.kube/config')
    kube_config_path = os.path.expanduser(kube_config_var)
    if os.path.isfile(kube_config_path):
        config.load_kube_config(kube_config_path)
    else:
        raise Exception("KUBECONFIG file not found: {0}".format(kube_config_path))


@pytest.fixture(scope='module')
def bootstrap_deployment_info():
    return DeploymentInfo()


@pytest.fixture(scope='module')
def client_deployment_info():
    def _create_client_info(boot_info):
        return DeploymentInfo(boot_info.deployment_id)

    return _create_client_info
