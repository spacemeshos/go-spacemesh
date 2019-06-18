from datetime import datetime, timedelta

from tests import queries, analyse
from tests import pod, deployment
from tests.fixtures import load_config, DeploymentInfo, NetworkDeploymentInfo
from tests.fixtures import init_session, set_namespace, set_docker_images, session_id
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
from tests.misc import ContainerSpec
from tests.queries import ES


BOOT_DEPLOYMENT_FILE = './k8s/bootstrap-w-conf.yml'
CLIENT_DEPLOYMENT_FILE = './k8s/client-w-conf.yml'
CLIENT_POD_FILE = './k8s/single-client-w-conf.yml'
CURL_POD_FILE = './k8s/curl.yml'
ORACLE_DEPLOYMENT_FILE = './k8s/oracle.yml'
POET_DEPLOYMENT_FILE = './k8s/poet.yml'

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


# ==============================================================================
#    Fixtures
# ==============================================================================

def setup_server(deployment_name, deployment_file, namespace):
    deployment_name_prefix = deployment_name.split('-')[0]
    namespaced_pods = client.CoreV1Api().list_namespaced_pod(namespace,
                                                             label_selector=(
                                                                 "name={0}".format(deployment_name_prefix))).items
    if namespaced_pods:
        # if server already exist -> delete it
        deployment.delete_deployment(deployment_name, namespace)

    resp = deployment.create_deployment(deployment_file, namespace, time_out=testconfig['deployment_ready_time_out'])
    namespaced_pods = client.CoreV1Api().list_namespaced_pod(namespace,
                                                             label_selector=(
                                                                 "name={0}".format(deployment_name_prefix))).items
    if not namespaced_pods:
        raise Exception('Could not setup Server: {0}'.format(deployment_name))
    return namespaced_pods[0].status.pod_ip


@pytest.fixture(scope='module')
def setup_poet(request):
    poet_deployment_name = 'poet'
    return setup_server(poet_deployment_name, POET_DEPLOYMENT_FILE, testconfig['namespace'])


@pytest.fixture(scope='module')
def setup_oracle(request):
    oracle_deployment_name = 'oracle'
    return setup_server(oracle_deployment_name, ORACLE_DEPLOYMENT_FILE, testconfig['namespace'])


@pytest.fixture(scope='module')
def setup_bootstrap(request, init_session, setup_oracle, setup_poet, create_configmap):

    bootstrap_deployment_info = DeploymentInfo(dep_id=init_session)

    def _setup_bootstrap_in_namespace(name_space):
        bootstrap_args = {} if 'args' not in testconfig['bootstrap'] else testconfig['bootstrap']['args']

        cspec = ContainerSpec(cname='bootstrap',
                              cimage=testconfig['bootstrap']['image'],
                              centry=[testconfig['bootstrap']['command']])

        cspec.append_args(oracle_server='http://{0}:{1}'.format(setup_oracle, ORACLE_SERVER_PORT),
                          poet_server='{0}:{1}'.format(setup_poet, POET_SERVER_PORT),
                          genesis_time=GENESIS_TIME.isoformat('T', 'seconds'),
                          **bootstrap_args)

        resp = deployment.create_deployment(BOOT_DEPLOYMENT_FILE, name_space,
                                            deployment_id=bootstrap_deployment_info.deployment_id,
                                            replica_size=testconfig['bootstrap']['replicas'],
                                            container_specs=cspec,
                                            time_out=testconfig['deployment_ready_time_out'])

        bootstrap_deployment_info.deployment_name = resp.metadata._name
        # The tests assume we deploy only 1 bootstrap
        bootstrap_pod_json = (
            client.CoreV1Api().list_namespaced_pod(namespace=name_space,
                                                   label_selector=(
                                                       "name={0}".format(
                                                           bootstrap_deployment_info.deployment_name.split('-')[0]))).items[0])
        bs_pod = {'name': bootstrap_pod_json.metadata.name}

        while True:
            resp = client.CoreV1Api().read_namespaced_pod(name=bs_pod['name'], namespace=name_space)
            if resp.status.phase != 'Pending':
                break
            time.sleep(1)

        bs_pod['pod_ip'] = resp.status.pod_ip
        bootstrap_pod_logs = client.CoreV1Api().read_namespaced_pod_log(name=bs_pod['name'], namespace=name_space)
        match = re.search(r"Local node identity >> (?P<bootstrap_key>\w+)", bootstrap_pod_logs)
        bs_pod['key'] = match.group('bootstrap_key')
        bootstrap_deployment_info.pods = [bs_pod]
        return bootstrap_deployment_info
    return _setup_bootstrap_in_namespace(testconfig['namespace'])


def node_string(key, ip, port, discport):
    return "spacemesh://{0}@{1}:{2}?disc={3}".format(key, ip, port, discport)

@pytest.fixture(scope='module')
def setup_clients(request, init_session, setup_oracle, setup_poet, setup_bootstrap):

    client_info = DeploymentInfo(dep_id=setup_bootstrap.deployment_id)

    def _setup_clients_in_namespace(name_space):
        bs_info = setup_bootstrap.pods[0]

        client_args = {} if 'args' not in testconfig['client'] else testconfig['client']['args']

        cspec = ContainerSpec(cname='client',
                              cimage=testconfig['client']['image'],
                              centry=[testconfig['client']['command']])

        print("I'm checking poet... {}".format(setup_poet))
        if setup_poet is None:
            raise Exception("failed starting a poet")

        cspec.append_args(bootnodes=node_string(bs_info['key'], bs_info['pod_ip'], BOOTSTRAP_PORT, BOOTSTRAP_PORT),
                          oracle_server='http://{0}:{1}'.format(setup_oracle, ORACLE_SERVER_PORT),
                          poet_server='{0}:{1}'.format(setup_poet, POET_SERVER_PORT),
                          genesis_time=GENESIS_TIME.isoformat('T', 'seconds'),
                          **client_args)

        resp = deployment.create_deployment(CLIENT_DEPLOYMENT_FILE, name_space,
                                            deployment_id=setup_bootstrap.deployment_id,
                                            replica_size=testconfig['client']['replicas'],
                                            container_specs=cspec,
                                            time_out=testconfig['deployment_ready_time_out'])

        client_info.deployment_name = resp.metadata._name
        client_pods = (
            client.CoreV1Api().list_namespaced_pod(name_space,
                                                   include_uninitialized=True,
                                                   label_selector=(
                                                       "name={0}".format(
                                                           client_info.deployment_name.split('-')[0]))).items)

        client_info.pods = [{'name': c.metadata.name, 'pod_ip': c.status.pod_ip} for c in client_pods]
        return client_info

    return _setup_clients_in_namespace(testconfig['namespace'])


@pytest.fixture(scope='module')
def setup_network(request, init_session, setup_oracle, setup_poet,
                  setup_bootstrap, setup_clients, add_curl, wait_genesis):

    # This fixture deploy a complete Spacemesh network and returns only after genesis time is over
    network_deployment = NetworkDeploymentInfo(dep_id=init_session,
                                               oracle_deployment_info=setup_oracle,
                                               poet_deployment_info=setup_poet,
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
    client_pods = (
        client.CoreV1Api().list_namespaced_pod(testconfig['namespace'],
                                               include_uninitialized=True,
                                               label_selector=(
                                                   "name={0}".format(
                                                       resp.metadata._name.split('-')[0]))).items)
    pods = []
    for c in client_pods:
        pod_name= c.metadata.name
        if pod_name.startswith(resp.metadata.name):
            pods.append(pod_name)
    return pods


def get_conf(bs_info, setup_poet=None, setup_oracle= None, args=None):
    client_args = {} if 'args' not in testconfig['client'] else testconfig['client']['args']

    if args is not None:
        for arg in args:
            client_args[arg] = args[arg]

    cspec = ContainerSpec(cname='client',
                          cimage=testconfig['client']['image'],
                          centry=[testconfig['client']['command']])
    cspec.append_args(bootnodes=node_string(bs_info['key'], bs_info['pod_ip'], BOOTSTRAP_PORT, BOOTSTRAP_PORT),
                      genesis_time=GENESIS_TIME.isoformat('T', 'seconds'))

    if setup_oracle is not None:
        cspec.append_args(oracle_server='http://{0}:{1}'.format(setup_oracle, ORACLE_SERVER_PORT))
    if setup_poet is not None:
        cspec.append_args(poet_server='{0}:{1}'.format(setup_poet, POET_SERVER_PORT))

    if len(client_args) > 0:
        cspec.append_args(**client_args)

    return cspec

# The following fixture should not be used if you wish to add many clients during test.
# Instead you should call add_single_client directly
@pytest.fixture()
def add_client(request, setup_oracle, setup_poet, setup_bootstrap, setup_clients):

    global client_name

    def _add_single_client():
        global client_name
        if not setup_bootstrap.pods:
            raise Exception("Could not find bootstrap node")

        bs_info = setup_bootstrap.pods[0]

        cspec = get_conf(bs_info, setup_poet, setup_oracle)

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
    res = stream(client.CoreV1Api().connect_post_namespaced_pod_exec, name="curl", namespace=namespace, command=["curl", "-s", "--request",  "POST", "--data", data, "http://" + client_ip + ":9090/" + api], stderr=True, stdin=False, stdout=True, tty=False, _request_timeout=90)
    return res


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
            client.CoreV1Api().create_namespaced_config_map(namespace=nspace,
                                                            body=configmap,
                                                            pretty='pretty_example')
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
def add_curl(request, setup_bootstrap):

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
    # choose client to run on
    client_ip = setup_network.clients.pods[0]['pod_ip']

    api = 'v1/nonce'
    data = '{"address":"1"}'
    print("checking nonce")
    out = api_call(client_ip, data, api, testconfig['namespace'])
    assert "{'value': '0'}" in out
    print("nonce ok")

    api = 'v1/submittransaction'
    data = '{"srcAddress":"1","dstAddress":"222","nonce":"0","amount":"100"}'
    print("submitting transaction")
    out = api_call(client_ip, data, api, testconfig['namespace'])
    print(out)
    assert "{'value': 'ok'}" in out
    print("submit transaction ok")
    print("wait for confirmation ")
    api = 'v1/balance'
    data = '{"address":"222"}'
    end = start = time.time()

    for x in range(7):
        time.sleep(60)
        print("... ")
        out = api_call(client_ip, data, api, testconfig['namespace'])
        if "{'value': '100'}" in out:
            end = time.time()
            break

    print("test took {:.3f} seconds ".format(end-start))
    assert "{'value': '100'}" in out
    print("balance ok")


def test_mining(setup_network):
    # choose client to run on
    client_ip = setup_network.clients.pods[0]['pod_ip']

    api = 'v1/nonce'
    data = '{"address":"1"}'
    print("checking nonce")
    out = api_call(client_ip, data, api, testconfig['namespace'])
    # assert "{'value': '0'}" in out
    # print("nonce ok")

    api = 'v1/submittransaction'
    data = '{"srcAddress":"1","dstAddress":"222","nonce":"0","amount":"100"}'
    print("submitting transaction")
    out = api_call(client_ip, data, api, testconfig['namespace'])
    print(out)
    assert "{'value': 'ok'}" in out
    print("submit transaction ok")
    print("wait for confirmation ")
    api = 'v1/balance'
    data = '{"address":"222"}'
    end = start = time.time()
    layer_avg_size = 20
    last_layer = 9
    layers_per_epoch = 3
    # deviation = 0.2
    last_epoch = last_layer / layers_per_epoch

    queries.wait_for_latest_layer(testconfig["namespace"], last_layer)
    print("test took {:.3f} seconds ".format(end-start))
    total_pods = len(setup_network.clients.pods) + len(setup_network.bootstrap.pods)
    analyse.analyze_mining(testconfig['namespace'], last_layer, layers_per_epoch, layer_avg_size, total_pods)


''' todo: when atx flow stabilized re enable this test
def test_atxs_nodes_up(setup_bootstrap, setup_clients, add_curl, wait_genesis, setup_poet, setup_oracle):
    # choose client to run on
    client_ip = setup_clients.pods[0]['pod_ip']

    api = 'v1/nonce'
    data = '{"address":"1"}'
    print("checking nonce")
    out = api_call(client_ip, data, api, testconfig['namespace'])

    api = 'v1/submittransaction'
    data = '{"srcAddress":"1","dstAddress":"222","nonce":"0","amount":"100"}'
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
