import os
import pytest
from kubernetes import config


@pytest.fixture(scope='session')
def load_config():
    kube_config_var = os.getenv('KUBECONFIG', '~/.kube/config')
    kube_config_path = os.path.expanduser(kube_config_var)
    if os.path.isfile(kube_config_path):
        config.load_kube_config(kube_config_path)
    else:
        raise Exception("KUBECONFIG file not found: {0}".format(kube_config_path))
