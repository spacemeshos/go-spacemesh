from datetime import datetime, timedelta

from tests import queries, analyse
from tests import pod, deployment
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
from tests.queries import ES
from tests.hare.assert_hare import validate_hare

BOOT_DEPLOYMENT_FILE = './k8s/bootstrapoet-w-conf.yml'
CLIENT_DEPLOYMENT_FILE = './k8s/client-w-conf.yml'
CLIENT_POD_FILE = './k8s/single-client-w-conf.yml'
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


def setup_bootstrap_in_namespace(namespace, bs_deployment_info, bootstrap_config, oracle=None, poet=None, dep_time_out=120):

    bootstrap_args = {} if 'args' not in bootstrap_config else bootstrap_config['args']
    cspec = ContainerSpec(cname='bootstrap', specs=bootstrap_config)

    if oracle:
        bootstrap_args['oracle_server'] = 'http://{0}:{1}'.format(oracle, ORACLE_SERVER_PORT)

    if poet:
        bootstrap_args['poet_server'] = '{0}:{1}'.format(poet, POET_SERVER_PORT)

    cspec.append_args(genesis_time=GENESIS_TIME.isoformat('T', 'seconds'),
                      **bootstrap_args)

    resp = deployment.create_deployment(BOOT_DEPLOYMENT_FILE, namespace,
                                        deployment_id=bs_deployment_info.deployment_id,
                                        replica_size=bootstrap_config['replicas'],
                                        container_specs=cspec,
                                        time_out=dep_time_out)

    bs_deployment_info.deployment_name = resp.metadata._name
    # The tests assume we deploy only 1 bootstrap
    bootstrap_pod_json = (
        client.CoreV1Api().list_namespaced_pod(namespace=namespace,
                                               label_selector=(
                                                   "name={0}".format(
                                                       bs_deployment_info.deployment_name.split('-')[1]))).items[0])
    bs_pod = {'name': bootstrap_pod_json.metadata.name}

    while True:
        resp = client.CoreV1Api().read_namespaced_pod(name=bs_pod['name'], namespace=namespace)
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

# ==============================================================================
#    Fixtures
# ==============================================================================


def setup_server(deployment_name, deployment_file, namespace):
    deployment_name_prefix = deployment_name.split('-')[1]
    namespaced_pods = CoreV1ApiClient().list_namespaced_pod(namespace,
                                                            label_selector=(
                                                                "name={0}".format(deployment_name_prefix))).items
    if namespaced_pods:
        # if server already exist -> delete it
        deployment.delete_deployment(deployment_name, namespace)

    resp = deployment.create_deployment(deployment_file, namespace, time_out=testconfig['deployment_ready_time_out'])
    namespaced_pods = CoreV1ApiClient().list_namespaced_pod(namespace,
                                                            label_selector=(
                                                                "name={0}".format(deployment_name_prefix))).items
    if not namespaced_pods:
        raise Exception('Could not setup Server: {0}'.format(deployment_name))

    ip = namespaced_pods[0].status.pod_ip
    if ip is None:
        print("{0} IP was None, trying again..".format(deployment_name_prefix))
        time.sleep(3)
        # retry
        namespaced_pods = CoreV1ApiClient().list_namespaced_pod(namespace,
                                                                label_selector=(
                                                                    "name={0}".format(deployment_name_prefix))).items
        ip = namespaced_pods[0].status.pod_ip
        if ip is None:
            raise Exception("Failed to retrieve {0} ip address".format(deployment_name_prefix))

    return ip


@pytest.fixture(scope='module')
def setup_bootstrap(request, init_session):
    bootstrap_deployment_info = DeploymentInfo(dep_id=init_session)

    return setup_bootstrap_in_namespace(testconfig['namespace'],
                                        bootstrap_deployment_info,
                                        testconfig['bootstrap'],
                                        dep_time_out=testconfig['deployment_ready_time_out'])


def node_string(key, ip, port, discport):
    return "spacemesh://{0}@{1}:{2}?disc={3}".format(key, ip, port, discport)


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


def setup_clients_in_namespace(namespace, bs_deployment_info, client_deployment_info, client_config,
                               oracle=None, poet=None, dep_time_out=120):

    cspec = get_conf(bs_deployment_info, client_config, oracle, poet)

    resp = deployment.create_deployment(CLIENT_DEPLOYMENT_FILE, namespace,
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


def add_single_client(deployment_id, container_specs):
    resp = pod.create_pod(CLIENT_POD_FILE,
                          testconfig['namespace'],
                          deployment_id=deployment_id,
                          container_specs=container_specs)

    client_name = resp.metadata.name
    print("Add new client: {0}".format(client_name))
    return client_name


def add_multi_clients(deployment_id, container_specs, size=2):
    resp = deployment.create_deployment(file_name=CLIENT_DEPLOYMENT_FILE,
                                        name_space=testconfig['namespace'],
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


# The following fixture should not be used if you wish to add many clients during test.
# Instead you should call add_single_client directly
@pytest.fixture()
def add_client(request, setup_bootstrap, setup_clients):
    global client_name

    def _add_single_client():
        global client_name
        if not setup_bootstrap.pods:
            raise Exception("Could not find bootstrap node")

        bs_info = setup_bootstrap.pods[0]
        cspec = get_conf(bs_info, testconfig['client'])
        client_name = add_single_client(setup_bootstrap.deployment_id, cspec)
        return client_name

    return _add_single_client()


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


def api_call(client_ip, data, api, namespace):
    # todo: this won't work with long payloads - ( `Argument list too long` ). try port-forward ?
    res = stream(CoreV1ApiClient().connect_post_namespaced_pod_exec, name="curl", namespace=namespace,
                 command=["curl", "-s", "--request", "POST", "--data", data, "http://" + client_ip + ":9090/" + api],
                 stderr=True, stdin=False, stdout=True, tty=False, _request_timeout=90)
    return res

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

    api = 'v1/submittransaction'
    txGen = tx_generator.TxGenerator()
    txBytes = txGen.generate("0000000000000000000000000000000000002222", 0, 246, 642, 100)
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
