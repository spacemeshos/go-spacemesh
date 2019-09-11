import json
from datetime import datetime, timedelta
import random
from tests import queries, analyse
from tests import pod, deployment, statefulset
from tests.fixtures import load_config, DeploymentInfo, NetworkDeploymentInfo
from tests.fixtures import init_session, set_namespace, set_docker_images, session_id
from tests import tx_generator
import pytest
import pytz
import re
import subprocess
import time
from kubernetes import client
from kubernetes.client.rest import ApiException
from kubernetes.stream import stream
from pytest_testconfig import config as testconfig
from elasticsearch_dsl import Search, Q
from tests.misc import ContainerSpec, CoreV1ApiClient
from tests.context import ES
from tests.hare.assert_hare import validate_hare
import pprint

from tests.ed25519.eddsa import genkeypair

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


def query_bootstrap_es(indx, namespace, bootstrap_po_name):
    es = ES.get_search_api()
    fltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
           Q("match_phrase", kubernetes__pod_name=bootstrap_po_name) & \
           Q("match_phrase", M="Local node identity")
    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits = list(s.scan())
    for h in hits:
        match = re.search(r"Local node identity \w+ (?P<bootstrap_key>\w+)", h.log)
        if match:
            return match.group('bootstrap_key')
    return None


def get_conf(bs_info, client_config, setup_oracle=None, setup_poet=None, args=None):

    client_args = {} if 'args' not in client_config else client_config['args']

    if args is not None:
        for arg in args:
            client_args[arg] = args[arg]

    cspec = ContainerSpec(cname='client', specs=client_config)

    if setup_oracle:
        client_args['oracle_server'] = 'http://{0}:{1}'.format(setup_oracle, ORACLE_SERVER_PORT)

    if setup_poet:
        client_args['poet_server'] = '{0}:{1}'.format(setup_poet, POET_SERVER_PORT)

    cspec.append_args(bootnodes=node_string(bs_info['key'], bs_info['pod_ip'], BOOTSTRAP_PORT, BOOTSTRAP_PORT),
                      genesis_time=GENESIS_TIME.isoformat('T', 'seconds'))

    if len(client_args) > 0:
        cspec.append_args(**client_args)
    return cspec


def add_multi_clients(deployment_id, container_specs, size=2):

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
    pods = []
    for c in client_pods:
        pod_name = c.metadata.name
        if pod_name.startswith(resp.metadata.name):
            pods.append(pod_name)
    return pods


def choose_k8s_object_create(config, deployment_file, statefulset_file):
    dep_type = 'deployment' if 'deployment_type' not in config else config['deployment_type']
    if dep_type == 'deployment':
        return deployment_file, deployment.create_deployment
    elif dep_type == 'statefulset':
        return statefulset_file, statefulset.create_statefulset
    else:
        raise Exception("Unknown deployment type in configuration. Please check your config.yaml")


def setup_bootstrap_in_namespace(namespace, bs_deployment_info, bootstrap_config, oracle=None, poet=None, dep_time_out=120):

    bootstrap_args = {} if 'args' not in bootstrap_config else bootstrap_config['args']
    cspec = ContainerSpec(cname='bootstrap', specs=bootstrap_config)

    if oracle:
        bootstrap_args['oracle_server'] = 'http://{0}:{1}'.format(oracle, ORACLE_SERVER_PORT)

    if poet:
        bootstrap_args['poet_server'] = '{0}:{1}'.format(poet, POET_SERVER_PORT)

    cspec.append_args(genesis_time=GENESIS_TIME.isoformat('T', 'seconds'),
                      **bootstrap_args)

    k8s_file, k8s_create_func = choose_k8s_object_create(bootstrap_config,
                                                         BOOT_DEPLOYMENT_FILE,
                                                         BOOT_STATEFULSET_FILE)
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
                                                      bs_deployment_info.deployment_name.split('-')[1]))).items[0])
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


def setup_clients_in_namespace(namespace, bs_deployment_info, client_deployment_info, client_config,
                               oracle=None, poet=None, dep_time_out=120):

    cspec = get_conf(bs_deployment_info, client_config, oracle, poet)

    k8s_file, k8s_create_func = choose_k8s_object_create(client_config,
                                                         CLIENT_DEPLOYMENT_FILE,
                                                         CLIENT_STATEFULSET_FILE)
    resp = k8s_create_func(k8s_file, namespace,
                           deployment_id=client_deployment_info.deployment_id,
                           replica_size=client_config['replicas'],
                           container_specs=cspec,
                           time_out=dep_time_out)

    client_deployment_info.deployment_name = resp.metadata._name
    client_pods = (
        CoreV1ApiClient().list_namespaced_pod(namespace,
                                              include_uninitialized=True,
                                              label_selector=("name={0}".format(
                                                  client_deployment_info.deployment_name.split('-')[1]))).items)

    client_deployment_info.pods = [{'name': c.metadata.name, 'pod_ip': c.status.pod_ip} for c in client_pods]
    return client_deployment_info


def api_call(client_ip, data, api, namespace):
    # todo: this won't work with long payloads - ( `Argument list too long` ). try port-forward ?
    res = stream(CoreV1ApiClient().connect_post_namespaced_pod_exec, name="curl", namespace=namespace,
                 command=["curl", "-s", "--request", "POST", "--data", data, "http://" + client_ip + ":9090/" + api],
                 stderr=True, stdin=False, stdout=True, tty=False, _request_timeout=90)
    return res


def node_string(key, ip, port, discport):
    return "spacemesh://{0}@{1}:{2}?disc={3}".format(key, ip, port, discport)


# ==============================================================================
#    Fixtures
# ==============================================================================


@pytest.fixture(scope='module')
def setup_bootstrap(request, init_session):
    bootstrap_deployment_info = DeploymentInfo(dep_id=init_session)

    return setup_bootstrap_in_namespace(testconfig['namespace'],
                                        bootstrap_deployment_info,
                                        testconfig['bootstrap'],
                                        dep_time_out=testconfig['deployment_ready_time_out'])


@pytest.fixture(scope='module')
def setup_clients(request, init_session, setup_bootstrap):

    client_info = DeploymentInfo(dep_id=setup_bootstrap.deployment_id)
    return setup_clients_in_namespace(testconfig['namespace'], setup_bootstrap.pods[0],
                                      client_info,
                                      testconfig['client'],
                                      poet=setup_bootstrap.pods[0]['pod_ip'],
                                      dep_time_out=testconfig['deployment_ready_time_out'])


@pytest.fixture(scope='module')
def setup_network(request, init_session, setup_bootstrap, setup_clients, add_curl, wait_genesis):
    # This fixture deploy a complete Spacemesh network and returns only after genesis time is over
    network_deployment = NetworkDeploymentInfo(dep_id=init_session,
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


# The following fixture is currently not in used and mark for deprecattion
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
        (out, err) = p.communicate()


@pytest.fixture(scope='module')
def add_curl(request, init_session, setup_bootstrap):
    def _run_curl_pod():
        if not setup_bootstrap.pods:
            raise Exception("Could not find bootstrap node")

        resp = pod.create_pod(CURL_POD_FILE,
                              testconfig['namespace'])
        return True

    return _run_curl_pod()


# ==============================================================================
#    TESTS
# ==============================================================================

dt = datetime.now()
todaydate = dt.strftime("%Y.%m.%d")
current_index = 'kubernetes_cluster-' + todaydate


def test_transaction(setup_network):
    ns = testconfig['namespace']

    # choose client to run on
    client_ip = setup_network.clients.pods[0]['pod_ip']

    api = 'v1/nonce'
    data = '{"address":"1"}'
    print("checking nonce")
    out = api_call(client_ip, data, api, testconfig['namespace'])
    assert "{'value': '0'}" in out
    print("nonce ok")

    api = 'v1/submittransaction'

    txGen = tx_generator.TxGenerator()
    txBytes = txGen.generate("0000000000000000000000000000000000002222", 0, 123, 321, 100)
    data = '{"tx":'+ str(list(txBytes)) + '}'

    print("submitting transaction")
    out = api_call(client_ip, data, api, testconfig['namespace'])
    print(out)
    assert "{'value': 'ok'" in out
    print("submit transaction ok")
    print("wait for confirmation ")
    api = 'v1/balance'
    data = '{"address":"0000000000000000000000000000000000002222"}'
    end = start = time.time()

    for x in range(7):
        time.sleep(60)
        print("... ")
        out = api_call(client_ip, data, api, testconfig['namespace'])
        if "{'value': '100'}" in out:
            end = time.time()
            break

    print("test took {:.3f} seconds ".format(end - start))
    assert "{'value': '100'}" in out
    print("balance ok")

    validate_hare(current_index, ns)  # validate hare


def test_transactions(setup_network):

    DEBUG = False

    tap_init_amount  = 10000
    tap = "7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

    tx_cost = 3 #.Mul(trans.GasPrice, tp.gasCost.BasicTxCost)

    nonceapi = 'v1/nonce'
    submitapi = 'v1/submittransaction'
    balanceapi = 'v1/balance'

    accounts = { tap: { "priv": "81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce5", "nonce": 0, "send": [], "recv": [] } }

    def random_node():
        # rnd = random.randint(0, len(setup_network.clients.pods)-1)
        return setup_network.clients.pods[0]['pod_ip'], setup_network.clients.pods[0]['name']

    def expected_balance(acc):
        balance = sum([int(tx["amount"]) for tx in accounts[acc]["recv"]]) - sum(int(int(tx["amount"]) + (int(tx["gasprice"])*tx_cost)) for tx in accounts[acc]["send"])
        if DEBUG:
            print("balance calculated for {0}, {1}, everything: {2}", acc, balance, pprint.pformat(accounts[acc]))
        return balance

    def random_account():
        pk = random.choice(list(accounts.keys()))
        return pk

    def new_account():
        priv, pub = genkeypair()
        strpub = bytes.hex(pub)
        accounts[strpub] = { "priv": bytes.hex(priv), "nonce": 0, "recv": [], "send": [] }
        return strpub

    def transfer(frm, to, amount=None, gasprice=None, gaslimit=None):
        pod_ip, pod_name = random_node()
        txGen = tx_generator.TxGenerator(pub=frm, pri=accounts[frm]['priv'])
        if amount is None:
            amount = random.randint(1, expected_balance(frm) - (1*tx_cost) )
        if gasprice is None:
            gasprice = 1
        if gaslimit is None:
            gaslimit = gasprice+1
        txBytes = txGen.generate(to, accounts[frm]['nonce'], gaslimit, gasprice, amount)
        accounts[frm]['nonce'] += 1
        data = '{"tx":'+ str(list(txBytes)) + '}'
        print("submit transaction from {3} to {0} of {1} with gasprice {2}".format(to, amount, gasprice, frm))
        out = api_call(pod_ip, data, submitapi, testconfig['namespace'])
        if DEBUG:
            print(data)
            print(out)
        if "{'value': 'ok'}" in out:
            accounts[to]["recv"].append({ "from": bytes.hex(txGen.publicK), "amount": amount, "gasprice": gasprice})
            accounts[frm]["send"].append({"to": to, "amount": amount, "gasprice": gasprice})
            return True
        if DEBUG:
            print("THE ERROR")
            print(out)
            print('/THE ERROR')
        return False

    def test_account(acc, init_amount=0):
        pod_ip, pod_name = random_node()
        #check nonce
        data = '{"address":"'+ acc +'"}'
        print("checking {0} nonce".format(acc))
        out = api_call(pod_ip, data, nonceapi, testconfig['namespace'])
        if DEBUG:
            print(out)
        if str(accounts[acc]['nonce']) in out:
            # check balance
            data = '{"address":"' + str(acc) + '"}'
            if DEBUG:
                print(data)
            out = api_call(pod_ip, data, balanceapi, testconfig['namespace'])
            if DEBUG:
                print(out)
            balance = init_amount
            balance = balance + expected_balance(acc)
            print("expecting balance: {0}".format(balance))
            if "{'value': '" + str(balance) + "'}" in out:
                print( "{0}, balance ok ({1})".format(str(acc), out))
                return True
            return False
        return False


    # send tx to client via rpc
    TEST_TXS = 10
    for i in range(TEST_TXS):
        pod_ip, pod_name = random_node()
        print(str("Sending tx from client: {0}/{1}").format(pod_name, pod_ip))
        balance = tap_init_amount + expected_balance(tap)
        if balance < 10: # Stop sending if the tap is out of money
            break
        amount = random.randint(1, int(balance/2))
        strpub = new_account()
        print("TAP NONCE {0}".format(accounts[tap]['nonce']))
        assert transfer(tap, strpub, amount=amount), "Transfer from tap failed"
        print("TAP NONCE {0}".format(accounts[tap]['nonce']))

    ready = 0
    for x in range(int(testconfig['client']['args']['layers-per-epoch'])*2): # wait for two epochs (genesis)
        ready = 0
        print("...")
        time.sleep(float(testconfig['client']['args']['layer-duration-sec']))
        print("checking tap nonce")
        if test_account(tap, tap_init_amount):
            print("nonce ok")
            for pk in accounts:
                if pk == tap:
                    continue
                print("checking account")
                print(pk)
                assert test_account(pk), "account {0} didn't have the expected nonce and balance".format(pk)
                ready+=1
            break
    assert ready == len(accounts)-1, "Not all accounts received sent txs" # one for 0 counting and one for tap.


    def is_there_a_valid_acc(min_balance, excpect=[]):
        for acc in accounts:
            if expected_balance(acc) - 1*tx_cost > min_balance and acc not in excpect:
                return True
        return False

    ## IF LOGEVITY THE CODE BELOW SHOULD RUN FOREVER

    TEST_TXS2 = 10
    newaccounts = []
    for i in range(TEST_TXS2):
        if not is_there_a_valid_acc(100, newaccounts):
            break

        pod_ip, pod_name = random_node()
        print("Sending tx from client: {0}/{1}".format(pod_name, pod_ip))
        if i % 2 == 0:
            # create new acc
            acc = random_account()
            while acc in newaccounts or expected_balance(acc) < 100:
                acc = random_account()

            pub = new_account()
            newaccounts.append(pub)
            assert transfer(acc, pub), "Transfer from {0} to {1} (new account) failed".format(acc, pub)
        else:
            accfrom = random_account()
            accto = random_account()
            while accfrom == accto or accfrom in newaccounts or expected_balance(accfrom) < 100:
                accfrom = random_account()
                accto = random_account()
            assert transfer(accfrom, accto), "Transfer from {0} to {1} failed".format(accfrom, accto)

    ready = 0

    for x in range(int(testconfig['client']['args']['layers-per-epoch'])*3):
        time.sleep(float(testconfig['client']['args']['layer-duration-sec']))
        print("...")
        ready = 0
        for pk in accounts:
            if test_account(pk, init_amount=tap_init_amount if pk is tap else 0):
                ready+=1

        if ready == len(accounts):
            break

    assert ready == len(accounts), "Not all accounts got the sent txs got: {0}, want: {1}".format(ready, len(accounts)-1)


def test_mining(setup_network):
    ns = testconfig['namespace']

    # choose client to run on
    client_ip = setup_network.clients.pods[0]['pod_ip']

    api = 'v1/nonce'
    data = '{"address":"1"}'
    print("checking nonce")
    out = api_call(client_ip, data, api, testconfig['namespace'])
    # assert "{'value': '0'}" in out
    # print("nonce ok")
    nonce = int(json.loads(out.replace("\'", "\""))['value'])

    api = 'v1/submittransaction'
    txGen = tx_generator.TxGenerator()
    txBytes = txGen.generate("0000000000000000000000000000000000002222", nonce, 246, 642, 100)
    data = '{"tx":'+ str(list(txBytes)) + '}'
    print("submitting transaction")
    out = api_call(client_ip, data, api, testconfig['namespace'])
    print(out)
    assert "{'value': 'ok'" in out
    print("submit transaction ok")
    print("wait for confirmation ")
    api = 'v1/balance'
    data = '{"address":"0000000000000000000000000000000000002222"}'
    end = start = time.time()

    layer_avg_size = testconfig['client']['args']['layer-average-size']
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    # check only third epoch
    epochs = 5
    last_layer = epochs*layers_per_epoch

    queries.wait_for_latest_layer(testconfig["namespace"], last_layer)
    print("test took {:.3f} seconds ".format(end - start))

    total_pods = len(setup_network.clients.pods) + len(setup_network.bootstrap.pods)
    time.sleep(50)
    analyse.analyze_mining(testconfig['namespace'], last_layer, layers_per_epoch, layer_avg_size, total_pods)

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
