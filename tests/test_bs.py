import copy
from datetime import datetime, timedelta
from enum import Enum
import json
from kubernetes import client
from kubernetes.client.rest import ApiException
from kubernetes.stream import stream
import pprint
import pytest
import pytz
import random
import subprocess
import sys
import time
from typing import List

from pytest_testconfig import config as testconfig
from tests import analyse, pod, deployment, queries, statefulset, tx_generator
from tests.conftest import DeploymentInfo, NetworkDeploymentInfo
from tests.ed25519.eddsa import genkeypair
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


def add_multi_clients(deployment_id, container_specs, size=2, ret_pods=False):
    """
    adds pods to a given namespace according to specification params

    :param deployment_id: string, namespace id
    :param container_specs:
    :param size: int, number of replicas
    :param ret_pods: boolean, if 'True' RETURN a pods list (V1PodList)
    :return: list (strings), list of pods names
    """
    k8s_file, k8s_create_func = choose_k8s_object_create(testconfig['client'],
                                                         CLIENT_DEPLOYMENT_FILE,
                                                         CLIENT_STATEFULSET_FILE)
    resp = k8s_create_func(k8s_file, testconfig['namespace'],
                           deployment_id=deployment_id,
                           replica_size=size,
                           container_specs=container_specs,
                           time_out=testconfig['deployment_ready_time_out'])

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
    setup clients adds new client nodes from k8s suite file

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
    out = api_call(bs_pod['pod_ip'], '{ "gatewayAddresses": ["127.0.0.1:9091"] }',  'v1/start', namespace, "80")
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

dt = datetime.now()
today_date = dt.strftime("%Y.%m.%d")
current_index = 'kubernetes_cluster-' + today_date


class Api(Enum):
    NONCE = 'v1/nonce'
    SUBMIT_TRANSACTION = 'v1/submittransaction'
    BALANCE = 'v1/balance'


class Ansi:
    RED = "\u001b[31m"
    GREEN = "\u001b[32m"
    YELLOW = "\u001b[33m"

    BRIGHT_BLACK = "\u001b[30;1m"
    BRIGHT_WHITE = "\u001b[37;1m"
    BRIGHT_YELLOW = "\u001b[33;1m"

    RESET = "\u001b[0m"


if not hasattr(sys.stderr, "isatty") or not sys.stderr.isatty():
    [setattr(Ansi, color, '') for color in dir(Ansi) if color[0] != '_']


class ClientWrapper:
    def __init__(self, pod_, namespace):
        self.ip = pod_['pod_ip']
        self.name = pod_['name']
        self.namespace = namespace
        print("\nUsing client:", Ansi.BRIGHT_WHITE + "{0.name} @ {0.ip}".format(self) + Ansi.RESET)

    def call(self, api, data):
        print(Ansi.YELLOW + "calling api: {} ({})...".format(api.name, api.value), end="")
        out = api_call(self.ip, json.dumps(data), api.value, self.namespace)
        print(" done.", Ansi.BRIGHT_BLACK, out.replace("'", '"'), Ansi.RESET)

        try:
            out_json = json.loads(out.replace("'", '"'))
            return out_json
        except json.JSONDecodeError as e:
            print(f"could not load json from api output: {e}\noutput={out}")

    def __str__(self):
        return f"client wrapper:\n\tip={self.ip}\n\tname={self.name}\n\tnamespace={self.namespace}"


def test_transaction(setup_network):
    ns = testconfig['namespace']
    print(f"{setup_network}")

    # choose client to run on
    client_wrapper = ClientWrapper(setup_network.clients.pods[0], ns)
    print(f"{client_wrapper}")
    out = client_wrapper.call(Api.NONCE, {'address': '1'})
    assert out.get('value') == '0'
    print("nonce ok")

    tx_gen = tx_generator.TxGenerator()
    dst = "0000000000000000000000000000000000002222"
    tx_bytes = tx_gen.generate(dst, nonce=0, gasLimit=123, fee=321, amount=100)
    out = client_wrapper.call(Api.SUBMIT_TRANSACTION, {'tx': list(tx_bytes)})
    assert out.get('value') == 'ok'
    print("submit transaction ok")
    print("wait for confirmation ")
    end = start = time.time()

    iterations_to_try = 7
    for x in range(iterations_to_try):
        layer_duration = testconfig['client']['args']['layer-duration-sec']
        print("\nSleeping layer duration ({}s)... {}/{}".format(layer_duration, x+1, iterations_to_try))
        time.sleep(float(layer_duration))

        out = client_wrapper.call(Api.BALANCE, {'address': dst})
        if out.get('value') == '100':
            end = time.time()
            break

    print("test took {:.3f} seconds ".format(end - start))
    assert out.get('value') == '100'
    print("balance ok")

    validate_hare(current_index, ns)  # validate hare


def test_transactions(setup_network):
    debug = False

    tap_init_amount = 10000
    tap = "7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

    class Transaction:
        def __init__(self, amount: int, fee: int, origin: str = None, dest: str = None):
            self.amount = amount
            self.fee = fee
            self.origin = origin
            self.dest = dest

        def __repr__(self):
            return "<TX: amount={0.amount}, fee={0.fee}, origin={0.origin}, dest={0.dest}>".format(self)

    class Account:
        def __init__(self, priv: str, nonce: int = 0, send: List[Transaction] = None, recv: List[Transaction] = None):
            self.priv = priv
            self.nonce = nonce
            self.send = send or []
            self.recv = recv or []

        def __repr__(self):
            return "<Account: priv={}, nonce={}, len(send)={}, len(recv)={}>".format(self.priv, self.nonce,
                                                                                     len(self.send), len(self.recv))

    accounts = {tap: Account(priv="81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce5")}

    def random_node() -> ClientWrapper:
        # rnd = random.randint(0, len(setup_network.clients.pods)-1)
        return ClientWrapper(setup_network.clients.pods[0], testconfig['namespace'])

    def expected_balance(acc: str, accounts_snapshot: dict = None):
        accounts_snapshot = accounts if accounts_snapshot is None else accounts_snapshot
        balance = (sum([tx.amount for tx in accounts_snapshot[acc].recv]) -
                   sum(tx.amount + tx.fee for tx in accounts_snapshot[acc].send))
        if debug:
            print("account={}, balance={}, everything={}".format(acc[-40:][:5], balance, pprint.pformat(accounts_snapshot[acc])))
        return balance

    def random_account(accounts_snapshot: dict = None) -> str:
        accounts_snapshot = accounts if accounts_snapshot is None else accounts_snapshot
        return random.choice(list(accounts_snapshot.keys()))

    def new_account() -> str:
        priv, pub = genkeypair()
        str_pub = bytes.hex(pub)
        accounts[str_pub] = Account(priv=bytes.hex(priv))
        return str_pub

    def transfer(client_wrapper: ClientWrapper, frm: str, to: str, amount=None, fee=None, gas_limit=None,
                 accounts_snapshot: dict = None):
        tx_gen = tx_generator.TxGenerator(pub=frm, pri=accounts[frm].priv)
        if amount is None:
            amount = random.randint(1, expected_balance(frm, accounts_snapshot) - 1)
        if fee is None:
            fee = 1
        if gas_limit is None:
            gas_limit = fee + 1
        tx_bytes = tx_gen.generate(to, accounts[frm].nonce, gas_limit, fee, amount)
        accounts[frm].nonce += 1
        data = {'tx': list(tx_bytes)}
        if debug:
            print(data)
        print("submit transaction from {} to {} of {} with fee {}".format(frm[-40:][:5], to[-40:][:5], amount, fee))
        out = client_wrapper.call(Api.SUBMIT_TRANSACTION, data)
        if out.get('value') == 'ok':
            accounts[to].recv.append(Transaction(amount, fee, origin=frm))
            accounts[frm].send.append(Transaction(amount, fee, dest=to))
            if accounts_snapshot:
                accounts_snapshot[frm].send.append(Transaction(amount, fee, dest=to))
            return True
        return False

    def test_account(client_wrapper: ClientWrapper, acc: str, init_amount: int = 0):
        # check nonce
        data = {'address': acc}
        if debug:
            print(data)
        out = client_wrapper.call(Api.NONCE, data)
        print("checking {} nonce...".format('tap' if acc == tap else acc[-40:][:5]), end="")
        if out.get('value') == str(accounts[acc].nonce):
            print(Ansi.GREEN + " ok ({})".format(out.get('value')) + Ansi.RESET)
            # check balance
            out = client_wrapper.call(Api.BALANCE, data)
            balance = init_amount
            balance = balance + expected_balance(acc)
            print("checking {} balance...".format('tap' if acc == tap else acc[-40:][:5]), end="")
            if out.get('value') == str(balance):
                print(Ansi.GREEN + " ok ({})".format(out.get('value')) + Ansi.RESET)
                return True
            print(Ansi.RED + " expected {} but got {}".format(balance, out.get('value')) + Ansi.RESET)
            return False
        print(Ansi.RED + " expected {} but got {}".format(str(accounts[acc].nonce), out.get('value')) + Ansi.RESET)
        return False

    def initialize_tap():
        client_wrapper = random_node()

        nonlocal tap_init_amount
        tap_init_amount = int(client_wrapper.call(Api.BALANCE, {'address': tap}).get('value'))
        accounts[tap].nonce = int(client_wrapper.call(Api.NONCE, {'address': tap}).get('value'))
    initialize_tap()

    def print_title(title: str):
        title = "≡≡ {} ≡≡".format(title)
        print("\n  " + Ansi.BRIGHT_YELLOW + "≡" * len(title))
        print("  " + title)
        print("  " + "≡" * len(title) + Ansi.RESET)

    def send_txs_from_tap():
        print_title("Sending transactions from tap")
        client_wrapper = random_node()
        test_txs = 10
        for i in range(test_txs):
            balance = tap_init_amount + expected_balance(tap)
            if balance < 10:  # Stop sending if the tap is out of money
                break
            amount = random.randint(1, int(balance/2))
            str_pub = new_account()
            assert transfer(client_wrapper, tap, str_pub, amount), "Transfer from tap failed"

        print_title("Ensuring that all transactions were received")
        iterations_to_try = int(testconfig['client']['args']['layers-per-epoch']) * 2  # wait for two epochs (genesis)
        ready_accounts = set()
        for x in range(iterations_to_try):
            layer_duration = testconfig['client']['args']['layer-duration-sec']
            print("\nSleeping layer duration ({}s)... {}/{}".format(layer_duration, x+1, iterations_to_try))
            time.sleep(float(layer_duration))
            if tap not in ready_accounts and test_account(client_wrapper, tap, tap_init_amount):
                ready_accounts.add(tap)
            for pk in accounts:
                if pk == tap or pk in ready_accounts:
                    continue
                if test_account(client_wrapper, pk):
                    ready_accounts.add(pk)
            if accounts.keys() == ready_accounts:
                break
        assert accounts.keys() == ready_accounts, \
            "Not all accounts received sent txs. Accounts that aren't ready: %s" % (accounts.keys()-ready_accounts)
    send_txs_from_tap()

    def is_there_a_valid_acc(min_balance, accounts_snapshot: dict = None):
        accounts_snapshot = accounts if accounts_snapshot is None else accounts_snapshot
        for acc in accounts_snapshot:
            if expected_balance(acc, accounts_snapshot) - 1 > min_balance:
                return True
        return False

    # IF LONGEVITY THE CODE BELOW SHOULD RUN FOREVER
    def send_txs_from_random_accounts():
        print_title("Sending transactions from random accounts")
        client_wrapper = random_node()
        accounts_snapshot = copy.deepcopy(accounts)
        test_txs2 = 10
        for i in range(test_txs2):
            if not is_there_a_valid_acc(100, accounts_snapshot):
                break
            if i % 2 == 0:
                # create new acc
                acc = random_account(accounts_snapshot)
                while expected_balance(acc, accounts_snapshot) < 100:
                    acc = random_account(accounts_snapshot)

                pub = new_account()
                assert transfer(client_wrapper, acc, pub, accounts_snapshot=accounts_snapshot), \
                    "Transfer from {} to {} (new account) failed".format(acc, pub)
            else:
                acc_from = random_account(accounts_snapshot)
                acc_to = random_account(accounts_snapshot)
                while acc_from == acc_to or expected_balance(acc_from, accounts_snapshot) < 100:
                    acc_from = random_account(accounts_snapshot)
                    acc_to = random_account()
                assert transfer(client_wrapper, acc_from, acc_to, accounts_snapshot=accounts_snapshot), \
                    "Transfer from {} to {} failed".format(acc_from, acc_to)

        print_title("Ensuring that all transactions were received")
        ready = 0
        iterations_to_try = int(testconfig['client']['args']['layers-per-epoch']) * 2
        for x in range(iterations_to_try):
            layer_duration = testconfig['client']['args']['layer-duration-sec']
            print("\n[{}/{}] Sleeping layer duration ({}s)...".format(x+1, iterations_to_try, layer_duration))
            time.sleep(float(layer_duration))
            ready = 0
            for pk in accounts:
                if test_account(client_wrapper, pk, init_amount=tap_init_amount if pk is tap else 0):
                    ready += 1

            print(Ansi.BRIGHT_WHITE + "≡≡≡≡≡ ready accounts: {}/{} ≡≡≡≡≡".format(ready, len(accounts)) + Ansi.RESET)
            if ready == len(accounts):
                break

        assert ready == len(accounts), "Not all accounts got the sent txs got: {}, want: {}".format(ready, len(accounts)-1)
    send_txs_from_random_accounts()
    send_txs_from_random_accounts()


def test_mining(setup_network):
    ns = testconfig['namespace']

    client_wrapper = ClientWrapper(setup_network.clients.pods[0], ns)

    tx_gen = tx_generator.TxGenerator()
    addr = bytes.hex(tx_gen.publicK)

    print("checking nonce")
    out = client_wrapper.call(Api.NONCE, {'address': addr})
    nonce = int(out.get('value'))
    print("checking balance")
    out = client_wrapper.call(Api.BALANCE, {'address': addr})
    balance = int(out.get('value'))

    tx_bytes = tx_gen.generate("0000000000000000000000000000000000002222", nonce, 246, 1, min(100, balance-1))
    print("submitting transaction")
    out = client_wrapper.call(Api.SUBMIT_TRANSACTION, {'tx': list(tx_bytes)})
    print(out)
    assert out.get('value') == 'ok'
    print("submit transaction ok")
    print("wait for confirmation ")
    end = start = time.time()

    layer_avg_size = testconfig['client']['args']['layer-average-size']
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    # check only third epoch
    epochs = 5
    last_layer = epochs*layers_per_epoch

    layer_reached = queries.wait_for_latest_layer(testconfig["namespace"], last_layer, layers_per_epoch)
    print("test took {:.3f} seconds ".format(end - start))

    total_pods = len(setup_network.clients.pods) + len(setup_network.bootstrap.pods)
    time.sleep(50)
    analyse.analyze_mining(testconfig['namespace'], layer_reached, layers_per_epoch, layer_avg_size, total_pods)

    validate_hare(current_index, ns)  # validate hare


''' todo: when atx flow stabilized re enable this test
def test_atxs_nodes_up(setup_bootstrap, setup_clients, add_curl, wait_genesis, setup_poet, setup_oracle):
    # choose client to run on
    client_ip = setup_clients.pods[0]['pod_ip']

    api = 'v1/nonce'
    data = '{"address":"1"}'
    print("checking nonce")
    out = api_call(client_ip, data, api, testconfig['namespace'])

    api = 'v1/submittransaction'
    txGen = xdr.TxGenerator()
    txBytes = txGen.generate("2222", 0, 123, 321, 100)
    data = '{"tx":'+ str(list(txBytes)) + '}'
    print("submitting transaction")
    out = api_call(client_ip, data, api, testconfig['namespace'])
    print(out.decode("utf-8"))
    assert '{"value":"ok"}' in out.decode("utf-8")
    print("submit transaction ok")

    end = start = time.time()
    layer_avg_size = 20
    last_layer = 8
    layers_per_epoch = 3
    deviation = 0.2
    extra_nodes = 20
    last_epoch = last_layer / layers_per_epoch

    queries.wait_for_latest_layer(testconfig["namespace"], layers_per_epoch)

    added_clients = []
    for i in range(0, extra_nodes):
        c = add_single_client(setup_bootstrap.deployment_id,
                              get_conf(setup_bootstrap.pods[0], setup_poet, setup_oracle))
        added_clients.append(c)

    queries.wait_for_latest_layer(testconfig["namespace"], last_layer)

    print("test took {:.3f} seconds ".format(end - start))

    # need to filter out blocks that have come from last layer
    blockmap = queries.get_blocks_per_node(testconfig["namespace"])
    # count all blocks arrived in relevant layers
    total_blocks = sum([len(blockmap[x]) for x in blockmap])
    atxmap = queries.get_atx_per_node(testconfig["namespace"])
    total_atxs = sum([len(atxmap[x]) for x in atxmap])

    total_pods = len(setup_clients.pods) + len(setup_bootstrap.pods) + extra_nodes

    print("atx created " + str(total_atxs))
    print("blocks created " + str(total_blocks))

    assert total_pods == len(blockmap)
    # assert (1 - deviation) < (total_blocks / last_layer) / layer_avg_size < (1 + deviation)
    # assert total_atxs == int((last_layer / layers_per_epoch) + 1) * total_pods

    # assert that a node has created one atx per epoch
    for node in atxmap:
        mp = set()
        for blk in atxmap[node]:
            mp.add(blk[4])
        if node not in added_clients:
            assert len(atxmap[node]) / int((last_layer / layers_per_epoch) + 1) == 1
        else:
            assert len(atxmap[node]) / int((last_layer / layers_per_epoch)) == 1

        print("mp " + ','.join(mp) + " node " + node + " atxmap " + str(atxmap[node]))
        if node not in added_clients:
            assert len(mp) == int((last_layer / layers_per_epoch) + 1)
        else:
            assert len(mp) == int((last_layer / layers_per_epoch))

    # assert that each node has created layer_avg/number_of_nodes
    mp = set()
    for node in blockmap:
        for blk in blockmap[node]:
            mp.add(blk[0])
        print("blocks:" + str(len(blockmap[node])) + "in layers" + str(len(mp)) + " " + str(layer_avg_size / total_pods))
        assert (len(blockmap[node]) / last_layer) / int((layer_avg_size / total_pods) + 0.5) <= 1.5
'''
