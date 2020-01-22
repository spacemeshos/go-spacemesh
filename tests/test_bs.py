from datetime import datetime, timedelta
from kubernetes import client
from kubernetes.client.rest import ApiException
from kubernetes.stream import stream
import pytest
from pytest_testconfig import config as testconfig
import pytz
import subprocess
import time

from tests import analyse, pod, deployment, queries, statefulset
from tests.convenience import sleep_print_backwards
from tests.tx_generator import config as conf
import tests.tx_generator.actions as actions
from tests.tx_generator.models.accountant import Accountant
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.conftest import DeploymentInfo, NetworkDeploymentInfo
from tests.hare.assert_hare import validate_hare
from tests.misc import ContainerSpec, CoreV1ApiClient


BOOT_DEPLOYMENT_FILE = './k8s/bootstrapoet-w-conf.yml'
BOOT_STATEFULSET_FILE = './k8s/bootstrapoet-w-conf-ss.yml'
CLIENT_DEPLOYMENT_FILE = './k8s/client-w-conf.yml'
CLIENT_STATEFULSET_FILE = './k8s/client-w-conf-ss.yml'
CURL_POD_FILE = './k8s/curl.yml'

BOOTSTRAP_PORT = 7513
ORACLE_SERVER_PORT = 3030
POET_SERVER_PORT = 50002

ELASTICSEARCH_URL = "http://{0}".format(testconfig['elastic']['host'])

GENESIS_TIME = pytz.utc.localize(datetime.utcnow() + timedelta(seconds=testconfig['genesis_delta']))

dt = datetime.now()
today_date = dt.strftime("%Y.%m.%d")
current_index = 'kubernetes_cluster-' + today_date


def get_conf(bs_info, client_config, setup_oracle=None, setup_poet=None, args=None):
    """
    get_conf gather specification information into one ContainerSpec object

    :param bs_info: DeploymentInfo, bootstrap info
    :param client_config: DeploymentInfo, client info
    :param setup_oracle: string, oracle ip
    :param setup_poet: string, poet ip
    :param args: list of strings, arguments for appendage in specification
    :return: ContainerSpec
    """
    client_args = {} if 'args' not in client_config else client_config['args']
    # append client arguments
    if args is not None:
        for arg in args:
            client_args[arg] = args[arg]

    # create a new container spec with client configuration
    cspec = ContainerSpec(cname='client', specs=client_config)

    # append oracle configuration
    if setup_oracle:
        client_args['oracle_server'] = 'http://{0}:{1}'.format(setup_oracle, ORACLE_SERVER_PORT)

    # append poet configuration
    if setup_poet:
        client_args['poet_server'] = '{0}:{1}'.format(setup_poet, POET_SERVER_PORT)

    cspec.append_args(bootnodes=node_string(bs_info['key'], bs_info['pod_ip'], BOOTSTRAP_PORT, BOOTSTRAP_PORT),
                      genesis_time=GENESIS_TIME.isoformat('T', 'seconds'))
    # append client config to ContainerSpec
    if len(client_args) > 0:
        cspec.append_args(**client_args)
    return cspec


def add_multi_clients(deployment_id, container_specs, size=2, client_title='client', ret_pods=False):
    """
    adds pods to a given namespace according to specification params

    :param deployment_id: string, namespace id
    :param container_specs:
    :param size: int, number of replicas
    :param client_title: string, client title in yml file (client, client_v2 etc)
    :param ret_pods: boolean, if 'True' RETURN a pods list (V1PodList)
    :return: list (strings), list of pods names
    """
    k8s_file, k8s_create_func = choose_k8s_object_create(testconfig[client_title],
                                                         CLIENT_DEPLOYMENT_FILE,
                                                         CLIENT_STATEFULSET_FILE)
    resp = k8s_create_func(k8s_file, testconfig['namespace'],
                           deployment_id=deployment_id,
                           replica_size=size,
                           container_specs=container_specs,
                           time_out=testconfig['deployment_ready_time_out'])

    print("\nadding new clients")
    client_pods = CoreV1ApiClient().list_namespaced_pod(testconfig['namespace'],
                                                        include_uninitialized=True,
                                                        label_selector=("name={0}".format(
                                                            resp.metadata._name.split('-')[1]))).items

    if ret_pods:
        ret_val = client_pods
    else:
        pods = []
        for c in client_pods:
            pod_name = c.metadata.name
            if pod_name.startswith(resp.metadata.name):
                pods.append(pod_name)
        ret_val = pods
        print(f"added new clients: {pods}\n")

    return ret_val


def choose_k8s_object_create(config, deployment_file, statefulset_file):
    dep_type = 'deployment' if 'deployment_type' not in config else config['deployment_type']
    if dep_type == 'deployment':
        return deployment_file, deployment.create_deployment
    elif dep_type == 'statefulset':
        # StatefulSets are intended to be used with stateful applications and distributed systems.
        # Pods in a StatefulSet have a unique ordinal index and a stable network identity.
        return statefulset_file, statefulset.create_statefulset
    else:
        raise Exception("Unknown deployment type in configuration. Please check your config.yaml")


def _setup_dep_ss_file_path(file_path, dep_type, node_type):
    # TODO we should modify this func to be a generic one
    """
    sets up deployment files
    :param file_path: string, a file path to overwrite the default file path value (BOOT_DEPLOYMENT_FILE,
    CLIENT_STATEFULSET_FILE, etc)
    :param dep_type: string, stateful or deployment
    :param node_type: string, client or bootstrap

    :return: string, string, deployment and stateful specification files paths
    """
    if node_type == 'bootstrap':
        dep_file_path = BOOT_DEPLOYMENT_FILE
        ss_file_path = BOOT_STATEFULSET_FILE
    elif node_type == 'client':
        dep_file_path = CLIENT_DEPLOYMENT_FILE
        ss_file_path = CLIENT_STATEFULSET_FILE
    else:
        raise ValueError(f"can not recognize node name: {node_type}")

    if file_path and dep_type == "statefulset":
        print(f"setting up stateful file path to {file_path}\n")
        ss_file_path = file_path
    elif file_path and dep_type == "deployment":
        print(f"setting up deployment file path to {file_path}\n")
        dep_file_path = file_path

    return dep_file_path, ss_file_path


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
    k8s_file, k8s_create_func = choose_k8s_object_create(bootstrap_config,
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


def setup_clients_in_namespace(namespace, bs_deployment_info, client_deployment_info, client_config, name="client",
                               file_path=None, oracle=None, poet=None, dep_time_out=120):
    # setting stateful and deployment configuration files
    # default deployment method is 'deployment'
    dep_method = client_config["deployment_type"] if "deployment_type" in client_config.keys() else "deployment"
    try:
        dep_file_path, ss_file_path = _setup_dep_ss_file_path(file_path, dep_method, 'client')
    except ValueError as e:
        print(f"error setting up client specification file: {e}")
        return None

    # this function used to be the way to extract the client title
    # in case we want a different title (client_v2 for example) we can specify it
    # directly in "name" input
    def _extract_label():
        return client_deployment_info.deployment_name.split('-')[1]

    cspec = get_conf(bs_deployment_info, client_config, oracle, poet)

    k8s_file, k8s_create_func = choose_k8s_object_create(client_config,
                                                         dep_file_path,
                                                         ss_file_path)
    resp = k8s_create_func(k8s_file, namespace,
                           deployment_id=client_deployment_info.deployment_id,
                           replica_size=client_config['replicas'],
                           container_specs=cspec,
                           time_out=dep_time_out)

    client_deployment_info.deployment_name = resp.metadata._name
    client_pods = (
        CoreV1ApiClient().list_namespaced_pod(namespace,
                                              include_uninitialized=True,
                                              label_selector=("name={0}".format(name))).items)

    client_deployment_info.pods = [{'name': c.metadata.name, 'pod_ip': c.status.pod_ip} for c in client_pods]
    return client_deployment_info


def api_call(client_ip, data, api, namespace, port="9090"):
    # todo: this won't work with long payloads - ( `Argument list too long` ). try port-forward ?
    res = stream(CoreV1ApiClient().connect_post_namespaced_pod_exec, name="curl", namespace=namespace,
                 command=["curl", "-s", "--request", "POST", "--data", data, f"http://{client_ip}:{port}/{api}"],
                 stderr=True, stdin=False, stdout=True, tty=False, _request_timeout=90)
    return res


def node_string(key, ip, port, discport):
    return "spacemesh://{0}@{1}:{2}?disc={3}".format(key, ip, port, discport)


# ==============================================================================
#    Fixtures
# ==============================================================================


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
                                             poet=setup_bootstrap.pods[0]['pod_ip'],
                                             dep_time_out=testconfig['deployment_ready_time_out'])

    return client_info


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
def setup_network(init_session, add_curl, setup_bootstrap, start_poet, setup_clients, wait_genesis):
    # This fixture deploy a complete Spacemesh network and returns only after genesis time is over
    _session_id = init_session
    network_deployment = NetworkDeploymentInfo(dep_id=_session_id,
                                               bs_deployment_info=setup_bootstrap,
                                               cl_deployment_info=setup_clients)
    return network_deployment


@pytest.fixture(scope='module')
def wait_genesis():
    # Make sure genesis time has not passed yet and sleep for the rest
    time_now = pytz.utc.localize(datetime.utcnow())
    delta_from_genesis = (GENESIS_TIME - time_now).total_seconds()
    if delta_from_genesis < 0:
        raise Exception("genesis_delta time={0}sec, is too short for this deployment. "
                        "delta_from_genesis={1}".format(testconfig['genesis_delta'], delta_from_genesis))
    else:
        print('sleep for {0} sec until genesis time'.format(delta_from_genesis))
        time.sleep(delta_from_genesis)


# The following fixture is currently not in used and mark for deprecation
@pytest.fixture(scope='module')
def create_configmap(request):
    def _create_configmap_in_namespace(nspace):
        # Configure ConfigMap metadata
        # This function assume that there is only 1 configMap in the each namespace
        configmap_name = testconfig['config_map_name']
        metadata = client.V1ObjectMeta(annotations=None,
                                       deletion_grace_period_seconds=30,
                                       labels=None,
                                       name=configmap_name,
                                       namespace=nspace)
        # Get File Content
        with open(testconfig['config_path'], 'r') as f:
            file_content = f.read()
        # Instantiate the configmap object
        d = {'config.toml': file_content}
        configmap = client.V1ConfigMap(api_version="v1",
                                       kind="ConfigMap",
                                       data=d,
                                       metadata=metadata)
        try:
            CoreV1ApiClient().create_namespaced_config_map(namespace=nspace, body=configmap, pretty='pretty_example')
        except ApiException as e:
            if eval(e.body)['reason'] == 'AlreadyExists':
                print('configmap: {0} already exist.'.format(configmap_name))
            raise e
        return configmap_name

    return _create_configmap_in_namespace(testconfig['namespace'])


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

        pod.create_pod(CURL_POD_FILE, testconfig['namespace'])
        return True

    return _run_curl_pod()


# ==============================================================================
#    TESTS
# ==============================================================================


def test_transactions(init_session, setup_network):
    # NOTE: because we're checking in this test for the TAP nonce this test
    #       should be the first test in this suite, because of a bug in the accountant
    #       with calculation for the TAP balance

    # create #new_acc_num new accounts by sending them coins from tap
    # check tap balance/nonce
    # sleep until new state is processed
    # send txs from new accounts and create new accounts
    # sleep until new state is processes
    # validate all accounts balance/nonce
    # send txs from all accounts between themselves
    # validate all accounts balance/nonce

    namespace = init_session
    wallet_api = WalletAPI(namespace, setup_network.clients.pods)

    tap_balance = wallet_api.get_balance_value(conf.acc_pub)
    tap_nonce = wallet_api.get_nonce_value(conf.acc_pub)
    acc = Accountant({conf.acc_pub: Accountant.set_tap_acc(balance=tap_balance, nonce=tap_nonce)})

    print("\n\n----- create new accounts ------")
    new_acc_num = 10
    amount = 50
    actions.send_coins_to_new_accounts(wallet_api, new_acc_num, amount, acc)

    print("assert tap's nonce and balance")
    ass_err = "tap did not have the matching nonce"
    assert actions.validate_nonce(wallet_api, acc, conf.acc_pub), ass_err
    ass_err = "tap did not have the matching balance"
    assert actions.validate_acc_amount(wallet_api, acc, conf.acc_pub), ass_err

    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    tts = layer_duration * conf.num_layers_until_process
    sleep_print_backwards(tts)

    print("\n\n------ create new accounts using the accounts created by tap ------")
    # add 1 because we have #new_acc_num new accounts and one tap
    tx_num = new_acc_num + 1
    amount = 5
    actions.send_tx_from_each_account(wallet_api, acc, tx_num, is_new_acc=True, amount=amount)

    tts = layer_duration * conf.num_layers_until_process
    sleep_print_backwards(tts)

    for acc_pub in acc.accounts:
        ass_err = f"account {acc_pub} did not have the matching balance"
        assert actions.validate_acc_amount(wallet_api, acc, acc_pub), ass_err

    for acc_pub in acc.accounts:
        ass_err = f"account {acc_pub} did not have the matching nonce"
        assert actions.validate_nonce(wallet_api, acc, acc_pub), ass_err

    print("\n\n------ send txs between all accounts ------")
    # send coins from all accounts between themselves (add 1 for tap)
    tx_num = new_acc_num * 2 + 1
    actions.send_tx_from_each_account(wallet_api, acc, tx_num)

    tts = layer_duration * conf.num_layers_until_process
    sleep_print_backwards(tts)

    for acc_pub in acc.accounts:
        ass_err = f"account {acc_pub} did not have the matching balance"
        assert actions.validate_acc_amount(wallet_api, acc, acc_pub), ass_err

    for acc_pub in acc.accounts:
        ass_err = f"account {acc_pub} did not have the matching nonce"
        assert actions.validate_nonce(wallet_api, acc, acc_pub), ass_err


def test_mining(init_session, setup_network):
    ns = init_session
    layer_avg_size = testconfig['client']['args']['layer-average-size']
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    # check only third epoch
    epochs = 5
    last_layer = epochs * layers_per_epoch

    layer_reached = queries.wait_for_latest_layer(testconfig["namespace"], last_layer, layers_per_epoch)

    total_pods = len(setup_network.clients.pods) + len(setup_network.bootstrap.pods)

    tts = 50
    sleep_print_backwards(tts)

    analyse.analyze_mining(testconfig['namespace'], layer_reached, layers_per_epoch, layer_avg_size, total_pods)

    validate_hare(current_index, ns)  # validate hare
