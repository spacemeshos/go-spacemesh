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


def setup_bootstrap_in_namespace(namespace, bs_deployment_info, bootstrap_config, oracle=None, poet=None,
                                 file_path=None, dep_time_out=120):
    """
    adds a bootstrap node to a specific namespace

    :param namespace: string, session id
    :param bs_deployment_info: DeploymentInfo, bootstrap info, metadata
    :param bootstrap_config: dictionary, bootstrap specifications
    :param oracle: string, oracle ip
    :param poet: string, poet ip
    :param file_path: string, optional, full path to deployment yaml
    :param dep_time_out: int, deployment timeout

    :return: DeploymentInfo, bootstrap info with a list of active pods
    """
    # setting stateful and deployment configuration files
    dep_method = bootstrap_config["deployment_type"] if "deployment_type" in bootstrap_config.keys() else "deployment"
    try:
        dep_file_path, ss_file_path = _setup_dep_ss_file_path(file_path, dep_method, 'bootstrap')
    except ValueError as e:
        print(f"error setting up bootstrap specification file: {e}")
        return None

    def _extract_label():
        name = bs_deployment_info.deployment_name.split('-')[1]
        return name

    bootstrap_args = {} if 'args' not in bootstrap_config else bootstrap_config['args']
    cspec = ContainerSpec(cname='bootstrap', specs=bootstrap_config)

    if oracle:
        bootstrap_args['oracle_server'] = 'http://{0}:{1}'.format(oracle, ORACLE_SERVER_PORT)

    if poet:
        bootstrap_args['poet_server'] = '{0}:{1}'.format(poet, POET_SERVER_PORT)

    cspec.append_args(genesis_time=GENESIS_TIME.isoformat('T', 'seconds'),
                      **bootstrap_args)

    # choose k8s creation function (deployment/stateful) and the matching k8s file
    k8s_file, k8s_create_func = misc.choose_k8s_object_create(bootstrap_config,
                                                              dep_file_path,
                                                              ss_file_path)
    # run the chosen creation function
    resp = k8s_create_func(k8s_file, namespace,
                           deployment_id=bs_deployment_info.deployment_id,
                           replica_size=bootstrap_config['replicas'],
                           container_specs=cspec,
                           time_out=dep_time_out)

    bs_deployment_info.deployment_name = resp.metadata._name
    # The tests assume we deploy only 1 bootstrap
    bootstrap_pod_json = (
        CoreV1ApiClient().list_namespaced_pod(namespace=namespace,
                                              label_selector=(
                                                  "name={0}".format(
                                                      _extract_label()))).items[0])
    bs_pod = {'name': bootstrap_pod_json.metadata.name}

    while True:
        resp = CoreV1ApiClient().read_namespaced_pod(name=bs_pod['name'], namespace=namespace)
        if resp.status.phase != 'Pending':
            break
        time.sleep(1)

    bs_pod['pod_ip'] = resp.status.pod_ip

    match = pod.search_phrase_in_pod_log(bs_pod['name'], namespace, 'bootstrap',
                                         r"Local node identity >> (?P<bootstrap_key>\w+)")

    if not match:
        raise Exception("Failed to read container logs in {0}".format('bootstrap'))

    bs_pod['key'] = match.group('bootstrap_key')
    bs_deployment_info.pods = [bs_pod]

    return bs_deployment_info


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
                                                             dep_time_out=testconfig['deployment_ready_time_out'])

    return bootstrap_deployment_info
