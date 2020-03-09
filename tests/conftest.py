from datetime import datetime, timedelta
from kubernetes import config as k8s_config
from kubernetes import client
from kubernetes.client.rest import ApiException
import os
import pytest
from pytest_testconfig import config as testconfig
import random
import string
import subprocess

from tests import pod
from tests import config as tests_conf
from tests.context import Context
from tests.misc import CoreV1ApiClient
from tests.setup_utils import setup_bootstrap_in_namespace, setup_clients_in_namespace
from tests.utils import api_call


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
        ret_str = f"DeploymentInfo:\n\tdeployment name: {self.deployment_name}\n\t"
        ret_str += f"deployment id: {self.deployment_id}\n\tpods number: {len(self.pods)}"
        return ret_str


class NetworkInfo:
    def __init__(self, namespace, bs_deployment_info, cl_deployment_info):
        self.namespace = namespace
        self.bootstrap = bs_deployment_info
        self.clients = cl_deployment_info


class NetworkDeploymentInfo:
    def __init__(self, dep_id, bs_deployment_info, cl_deployment_info):
        self.deployment_name = ''
        self.deployment_id = dep_id
        self.bootstrap = bs_deployment_info
        self.clients = cl_deployment_info

    def __str__(self):
        ret_str = f"NetworkDeploymentInfo:\n\tdeployment name: {self.deployment_name}\n\t"
        ret_str += f"deployment id: {self.deployment_id}\n\tbootstrap info:\n\t{self.bootstrap}\n\t"
        ret_str += f"client info:\n\t{self.clients}"
        return ret_str


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
        if 'clientv2' in testconfig.keys():
            testconfig['clientv2']['image'] = docker_image


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
              .format(testconfig['namespace']), "\n")
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
def init_session(load_config, set_namespace, set_docker_images, session_id):
    """
    init_session sets up a new testing environment using k8s with
    the given yaml config file
    :return: namespace id
    """
    return session_id


@pytest.fixture(scope='module')
def setup_bootstrap(init_session):
    """
    setup bootstrap initializes a session and adds a single bootstrap node
    :param init_session: sets up a new k8s env
    :return: DeploymentInfo type, containing the settings info of the new node
    """
    bootstrap_deployment_info = DeploymentInfo(dep_id=init_session)

    bootstrap_deployment_info = setup_bootstrap_in_namespace(testconfig['namespace'],
                                                             bootstrap_deployment_info,
                                                             testconfig['bootstrap'],
                                                             testconfig['genesis_delta'],
                                                             dep_time_out=testconfig['deployment_ready_time_out'])

    return bootstrap_deployment_info


@pytest.fixture(scope='module')
def setup_clients(init_session, setup_bootstrap):
    """
    setup clients adds new client nodes using suite file specifications

    :param init_session: setup a new k8s env
    :param setup_bootstrap: adds a single bootstrap node
    :return: client_info of type DeploymentInfo
             contains the settings info of the new client node
    """
    client_info = DeploymentInfo(dep_id=setup_bootstrap.deployment_id)
    client_info = setup_clients_in_namespace(testconfig['namespace'],
                                             setup_bootstrap.pods[0],
                                             client_info,
                                             testconfig['client'],
                                             testconfig['genesis_delta'],
                                             poet=setup_bootstrap.pods[0]['pod_ip'],
                                             dep_time_out=testconfig['deployment_ready_time_out'])

    return client_info


@pytest.fixture(scope='module')
def setup_mul_clients(init_session, setup_bootstrap):
    """
    setup_mul_clients adds all client nodes (those who have "client" in title)
    using suite file specifications

    :param init_session: setup a new k8s env
    :param setup_bootstrap: adds a single bootstrap node
    :return: list, client_infos a list of DeploymentInfo
             contains the settings info of the new clients nodes
    """
    clients_infos = []

    for key in testconfig:
        if "client" in key:
            client_info = DeploymentInfo(dep_id=setup_bootstrap.deployment_id)
            client_info = setup_clients_in_namespace(testconfig['namespace'],
                                                     setup_bootstrap.pods[0],
                                                     client_info,
                                                     testconfig[key],
                                                     testconfig['genesis_delta'],
                                                     poet=setup_bootstrap.pods[0]['pod_ip'],
                                                     dep_time_out=testconfig['deployment_ready_time_out'])
            clients_infos.append(client_info)

    return clients_infos


@pytest.fixture(scope='module')
def start_poet(init_session, add_curl, setup_bootstrap):
    bs_pod = setup_bootstrap.pods[0]
    namespace = testconfig['namespace']

    match = pod.search_phrase_in_pod_log(bs_pod['name'], namespace, 'poet',
                                         "REST proxy start listening on 0.0.0.0:80")
    if not match:
        raise Exception("Failed to read container logs in {0}".format("poet"))

    print("Starting PoET")
    out = api_call(bs_pod['pod_ip'], '{ "gatewayAddresses": ["127.0.0.1:9091"] }', 'v1/start', namespace, "80")
    assert out == "{}", "PoET start returned error {0}".format(out)
    print("PoET started")


@pytest.fixture(scope='module')
def save_log_on_exit(request):
    yield
    if testconfig['script_on_exit'] != '' and request.session.testsfailed == 1:
        p = subprocess.Popen([testconfig['script_on_exit'], testconfig['namespace']],
                             stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        p.communicate()


@pytest.fixture(scope='module')
def add_curl(request, init_session, setup_bootstrap):
    def _run_curl_pod():
        if not setup_bootstrap.pods:
            raise Exception("Could not find bootstrap node")

        pod.create_pod(tests_conf.CURL_POD_FILE, testconfig['namespace'])
        return True

    return _run_curl_pod()
