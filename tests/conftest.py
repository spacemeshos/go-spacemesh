from datetime import datetime
from kubernetes import config as k8s_config
from kubernetes import client
import os
import pytest
from pytest_testconfig import config as testconfig
import random
import string
import subprocess

from tests import config as conf
from tests import pod
from tests.context import Context, ES
from tests.convenience import str2bool
from tests.app_engine.gcloud_tasks.add_task_to_queue import create_google_cloud_task
from tests.k8s_handler import add_elastic_cluster, add_kibana_cluster, add_fluent_bit_cluster, \
    wait_for_daemonset_to_be_ready
from tests.misc import CoreV1ApiClient
from tests.node_pool_deployer import NodePoolDep
from tests.setup_utils import setup_bootstrap_in_namespace, setup_clients_in_namespace
from tests.utils import api_call, wait_for_minimal_elk_cluster_ready


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


def pytest_configure():
    # set a global variable to share index date between fixtures
    pytest.index_date = None


def pytest_addoption(parser):
    # add command line flags
    # delns - whether or not to delete the namespace after test was done
    parser.addoption(
        "--delns", action="store", default=True, help="whether or not to delete the namespace at the end of the run"
    )
    # namespace - current namespace value, if None a namespace will be randomly created
    parser.addoption("--namespace", action="store", default=None, help="namespace name")
    parser.addoption("--dump", action="store", default=False, help="whether or not to dump ES when done")


@pytest.fixture(scope='session')
def delete_ns(request):
    return str2bool(request.config.getoption("--delns"))


@pytest.fixture(scope='session')
def input_namespace(request):
    return request.config.getoption("--namespace")


@pytest.fixture(scope='session')
def input_dump(request):
    return str2bool(request.config.getoption("--dump"))


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
        print("++Set docker images to: {0}".format(docker_image))
        testconfig['bootstrap']['image'] = docker_image
        testconfig['client']['image'] = docker_image
        if 'clientv2' in testconfig.keys():
            print(testconfig['clientv2'])
            # some should not be replaced!
            if testconfig['clientv2'].get('noreplace', False):
                print("not replacing clientv2 docker image since replace is set to False")
            else:
                print("Set docker clientv2 images to: {0}".format(docker_image))
                testconfig['clientv2']['image'] = docker_image
        else:
            print("no other config")
            print(testconfig.keys())


@pytest.fixture(scope='session')
def session_id(input_namespace):
    if input_namespace:
        return input_namespace
    return random_id(length=5)


@pytest.fixture(scope='session')
def set_namespace(request, session_id, load_config, delete_ns):
    v1 = CoreV1ApiClient()
    if testconfig['namespace'] == '':
        testconfig['namespace'] = session_id

    print("\nRun tests in namespace: {0}".format(testconfig['namespace']))
    namespaces_list = [ns.metadata.name for ns in v1.list_namespace().items]
    if testconfig['namespace'] in namespaces_list:
        raise ValueError(f"namespace: {testconfig['namespace']} already exists!")

    body = client.V1Namespace()
    body.metadata = client.V1ObjectMeta(name=testconfig['namespace'])
    v1.create_namespace(body)


@pytest.fixture(scope='session')
def init_session(load_config, teardown, set_namespace, set_docker_images, session_id):
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
    out = api_call(bs_pod['pod_ip'], '{ "gatewayAddresses": ["127.0.0.1:9092"] }', 'v1/start', namespace, "80")
    assert out == "{}", "PoET start returned error {0}".format(out)
    print("PoET started")


@pytest.fixture(scope='module')
def save_log_on_exit(request):
    # pytest considers any code after `yield` to be teardown code
    # see: https://docs.pytest.org/en/reorganize-docs/yieldfixture.html
    yield
    if testconfig['script_on_exit'] != '' and request.session.testsfailed == 1:
        p = subprocess.Popen([testconfig['script_on_exit'], testconfig['namespace']],
                             stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        p.communicate()


@pytest.fixture(scope='module')
def add_curl(request, init_session):
    def _run_curl_pod():
        pod.create_pod(conf.CURL_POD_FILE, testconfig['namespace'])
        return True

    return _run_curl_pod()


@pytest.fixture(scope='module')
def add_node_pool(session_id):
    """
    memory should be represented by number of megabytes, \d+M

    :return:
    """
    deployer = NodePoolDep(testconfig)
    _, time_elapsed = deployer.add_node_pool()
    print(f"total time waiting for clients node pool creation: {time_elapsed}")
    # wait for fluent bit daemonset to be ready after node pool creation
    wait_for_daemonset_to_be_ready("fluent-bit", session_id, timeout=60)
    return time_elapsed


@pytest.fixture(scope='module')
def add_elk(init_session, request):
    # get today's date for filebeat data index
    pytest.index_date = datetime.utcnow().date().strftime("%Y.%m.%d")
    add_elastic_cluster(init_session)
    add_fluent_bit_cluster(init_session)
    add_kibana_cluster(init_session)
    wait_for_minimal_elk_cluster_ready(init_session)


@pytest.fixture(scope='session')
def teardown(request, session_id, delete_ns, input_dump):
    # pytest considers any code after `yield` to be teardown code
    # see: https://docs.pytest.org/en/reorganize-docs/yieldfixture.html
    yield
    # dump ES content either if tests has failed of whether is_dump param was set to True in the test config file
    is_dump = request.session.testsfailed > 0 or input_dump
    dump_params = {}
    if is_dump:
        dump_params = {
            "index_date": pytest.index_date,
            "es_ip": ES(session_id).get_elastic_ip(),
            "es_user": conf.ES_USER_LOCAL,
            "es_pass": conf.ES_PASS_LOCAL,
            "main_es_ip": conf.MAIN_ES_IP,
            "dump_queue_name": conf.DUMP_QUEUE_NAME,
            "dump_queue_zone": conf.DUMP_QUEUE_ZONE,
        }
    queue_params = {
        "project_id": conf.PROJECT_ID,
        "queue_name": conf.TD_QUEUE_NAME,
        "queue_zone": conf.TD_QUEUE_ZONE,
    }
    payload = {
        "namespace": session_id,
        "is_delns": delete_ns,
        "is_dump": is_dump,
        "project_id": conf.PROJECT_ID,
        "pool_name": f"pool-{session_id}",
        "cluster_name": conf.CLUSTER_NAME,
        "node_pool_zone": conf.CLUSTER_ZONE,
    }
    create_google_cloud_task(queue_params, payload, **dump_params)
