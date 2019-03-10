from datetime import datetime, timedelta
from fixtures import load_config
import os
from os import path
import pytest
import pytz
import re
import subprocess
import time
import yaml
from kubernetes import client
from kubernetes.client.rest import ApiException
from pytest_testconfig import config as testconfig

from elasticsearch import Elasticsearch
from elasticsearch_dsl import Search, Q

from node_info import NodeInfo

BOOT_DEPLOYMENT_FILE = './k8s/bootstrap-w-conf.yml'
CLIENT_DEPLOYMENT_FILE = './k8s/client-w-conf.yml'
ORACLE_DEPLOYMENT_FILE = './k8s/oracle.yml'

ELASTICSEARCH_URL = "http://{0}".format(testconfig['elastic']['host'])
REPLACEABLE_ARGS = ['randcon', 'oracle_server', 'bootnodes', 'genesis_time']

GENESIS_TIME = pytz.utc.localize(datetime.utcnow() + timedelta(seconds=testconfig['genesis_delta']))


def update_args_in_deployment_yaml(yml_input, **kwargs):
    args = yml_input['spec']['template']['spec']['containers'][0]['args']
    for k in kwargs:
        if k not in REPLACEABLE_ARGS:
            raise Exception("Unknown arg: {0} - parsing error")
        replaced = False
        for i, arg in enumerate(args):
            if arg[2:].replace('-', '_') == k:
                # replace the value
                args[i + 1] = kwargs[k]
                replaced = True
                break
        if not replaced:
            args += (['--{0}'.format(k.replace('_', '-')), kwargs[k]])
    yml_input['spec']['template']['spec']['containers'][0]['args'] = args
    return yml_input


def get_elastic_search_api():
    elastic_password = os.getenv('ES_PASSWD')
    if not elastic_password:
        raise Exception("Unknown Elasticsearch password. Please check 'ES_PASSWD' environment variable")
    es = Elasticsearch([ELASTICSEARCH_URL],
                       http_auth=(testconfig['elastic']['username'], elastic_password), port=80)
    return es


def wait_to_deployment_to_be_ready(deployment_name, name_space, time_out=None):
    total_sleep_time = 0
    while True:
        resp = client.AppsV1Api().read_namespaced_deployment(name=deployment_name, namespace=name_space)
        if not resp.status.unavailable_replicas:
            print("Total time waiting for deployment {0}: {1} sec".format(deployment_name, total_sleep_time))
            break
        time.sleep(1)
        total_sleep_time += 1

        if time_out and total_sleep_time > time_out:
            raise Exception("Timeout waiting to deployment to be ready")


def create_deployment(file_name, name_space, **kwargs):
    with open(path.join(path.dirname(__file__), file_name)) as f:
        dep = yaml.safe_load(f)
        if kwargs:
            dep = update_args_in_deployment_yaml(dep, **kwargs)

        k8s_beta = client.ExtensionsV1beta1Api()
        resp1 = k8s_beta.create_namespaced_deployment(body=dep, namespace=name_space)
        wait_to_deployment_to_be_ready(resp1.metadata._name, name_space,
                                       time_out=testconfig['deployment_ready_time_out'])
        return resp1


def delete_deployment(deployment_name, name_space):
    k8s_beta = client.ExtensionsV1beta1Api()
    resp = k8s_beta.delete_namespaced_deployment(name=deployment_name,
                                                 namespace=name_space,
                                                 body=client.V1DeleteOptions(propagation_policy='Foreground',
                                                                             grace_period_seconds=5))
    return resp


def query_bootstrap_es(indx, namespace, bootstrap_po_name):
    es = get_elastic_search_api()
    fltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
           Q("match_phrase", kubernetes__pod_name=bootstrap_po_name) & \
           Q("match_phrase", M="Local node identity")
    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits = list(s.scan())
    for h in hits:
        match = re.search(r"Local node identity \w+ (?P<bootstarap_key>\w+)", h.log)
        if match:
            return match.group('bootstarap_key')
    return None


def query_es_client_bootstrap(indx, namespace, client_po_name):
    es = get_elastic_search_api()
    fltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
           Q("match_phrase", kubernetes__pod_name=client_po_name) & Q("match_phrase", M='discovery_bootstrap')
    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits = list(s.scan())

    print("Number of Hits in ES: {0}".format(len(hits)))
    s = set([h.N for h in hits])
    return len(s)


def query_es_gossip_message(indx, namespace, client_po_name):
    es = get_elastic_search_api()
    fltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
           Q("match_phrase", kubernetes__pod_name=client_po_name) & Q("match_phrase", M='new_gossip_message')
    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits = list(s.scan())

    print("Number of gossip Hits: {0}".format(len(hits)))
    s = set([h.N for h in hits])
    return len(s)


# ==============================================================================
#    Fixtures
# ==============================================================================

@pytest.fixture(scope='module')
def setup_oracle():
    namespaced_pods = client.CoreV1Api().list_namespaced_pod(testconfig['namespace'],
                                                             label_selector="name=oracle").items
    if not namespaced_pods:
        resp = create_deployment(ORACLE_DEPLOYMENT_FILE, testconfig['namespace'])
        namespaced_pods = client.CoreV1Api().list_namespaced_pod(testconfig['namespace'],
                                                                 label_selector="name=oracle").items
        if not namespaced_pods:
            raise Exception('Could not setup Oracle Server')
    return namespaced_pods[0].status.pod_ip


@pytest.fixture
def setup_bootstrap(request, load_config, setup_oracle, create_configmap):
    def _setup_bootstrap_in_namespace(name_space):
        global bs_info
        bs_info = NodeInfo()
        resp = create_deployment(BOOT_DEPLOYMENT_FILE, name_space,
                                 oracle_server='http://{0}:3030'.format(setup_oracle),
                                 genesis_time=GENESIS_TIME.isoformat('T', 'seconds'))

        bs_info.bs_deployment_name = resp.metadata._name
        namespaced_pods = client.CoreV1Api().list_namespaced_pod(namespace=name_space).items
        bootstrap_pod = next(filter(lambda i: i.metadata.name.startswith(bs_info.bs_deployment_name), namespaced_pods))
        bs_info.bs_pod_name = bootstrap_pod.metadata.name

        while True:
            resp = client.CoreV1Api().read_namespaced_pod(name=bs_info.bs_pod_name, namespace=name_space)
            if resp.status.phase != 'Pending':
                break
            time.sleep(1)

        bs_info.bs_pod_ip = resp.status.pod_ip
        bootstrap_pod_logs = client.CoreV1Api().read_namespaced_pod_log(name=bs_info.bs_pod_name, namespace=name_space)
        match = re.search(r"Local node identity >> (?P<bootstarap_key>\w+)", bootstrap_pod_logs)
        bs_info.bs_key = match.group('bootstarap_key')
        return bs_info

    def fin():
        global bs_info
        delete_deployment(bs_info.bs_deployment_name, testconfig['namespace'])

    request.addfinalizer(fin)
    return _setup_bootstrap_in_namespace(testconfig['namespace'])


@pytest.fixture
def setup_clients(request, setup_oracle, setup_bootstrap):
    def _setup_clients_in_namespace(name_space):
        global bs_info, client_info
        client_info = NodeInfo()
        resp = create_deployment(CLIENT_DEPLOYMENT_FILE, name_space,
                                 bootnodes="{0}:{1}/{2}".format(bs_info.bs_pod_ip, '7513', bs_info.bs_key),
                                 oracle_server='http://{0}:3030'.format(setup_oracle),
                                 genesis_time=GENESIS_TIME.isoformat('T', 'seconds'))
        client_info.bs_deployment_name = resp.metadata._name
        namespaced_pods = client.CoreV1Api().list_namespaced_pod(namespace=name_space, include_uninitialized=True).items
        client_pods = list(
            filter(lambda i: i.metadata.name.startswith(client_info.bs_deployment_name), namespaced_pods))

        print("Number of client pods: {0}".format(len(client_pods)))
        for c in client_pods:
            while True:
                resp = client.CoreV1Api().read_namespaced_pod(name=c.metadata.name, namespace=name_space)
                if resp.status.phase != 'Pending':
                    break
                time.sleep(1)

        # Make sure genesis time has not passed yet and sleep for the rest
        time_now = pytz.utc.localize(datetime.utcnow())
        delta_from_genesis = (GENESIS_TIME - time_now).total_seconds()
        if delta_from_genesis < 0:
            raise Exception("genesis_delta time is too short for this deployment")
        else:
            print('sleep for {0} sec until genesis time'.format(delta_from_genesis))
            time.sleep(delta_from_genesis)
        return client_pods

    def fin():
        global client_info
        delete_deployment(client_info.bs_deployment_name, testconfig['namespace'])

    request.addfinalizer(fin)
    return _setup_clients_in_namespace(testconfig['namespace'])


@pytest.fixture
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
        with open('../config.toml', 'r') as f:
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

    def fin():
        client.CoreV1Api().delete_namespaced_config_map(testconfig['config_map_name'],
                                                        testconfig['namespace'],
                                                        body=client.V1DeleteOptions(propagation_policy='Foreground',
                                                                                    grace_period_seconds=5))

    request.addfinalizer(fin)
    return _create_configmap_in_namespace(testconfig['namespace'])


@pytest.fixture
def save_log_on_exit(request):
    yield
    if testconfig['script_on_exit'] != '' and request.session.testsfailed == 1:
        p = subprocess.Popen([testconfig['script_on_exit']],
                             stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        (out, err) = p.communicate()

# ==============================================================================
#    TESTS
# ==============================================================================

current_index = 'kubernetes_cluster-2019.03.*'


def test_bootstrap(setup_bootstrap):
    # wait for the bootstrap logs to be available in ElasticSearch
    time.sleep(5)
    assert setup_bootstrap.bs_key == query_bootstrap_es(current_index,
                                                        testconfig['namespace'],
                                                        setup_bootstrap.bs_pod_name)


def test_client(load_config, setup_clients, save_log_on_exit):
    global client_info
    peers = query_es_client_bootstrap(current_index, testconfig['namespace'], client_info.bs_deployment_name)
    assert peers == len(setup_clients)


def test_gossip(load_config, setup_clients):
    global client_info

    # send message to client via rpc
    client_ip = setup_clients[0].status.pod_ip
    print("Sending gossip from client ip: {0}".format(client_ip))

    api = 'v1/broadcast'
    data = '{"data":"foo"}'
    p = subprocess.Popen(['./kubectl-cmd.sh', '%s' % client_ip, "%s" % data, api],
                         stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    (out, err) = p.communicate()
    assert '{"value":"ok"}' in out.decode("utf-8")

    time.sleep(40)
    peers_for_gossip = query_es_gossip_message(current_index, testconfig['namespace'], client_info.bs_deployment_name)
    assert len(setup_clients) == peers_for_gossip


def test_transaction(load_config, setup_clients):
    global client_info

    # choose client to run on
    client_ip = setup_clients[0].status.pod_ip

    api = 'v1/nonce'
    data = '{"address":"1"}'
    p = subprocess.Popen(['./kubectl-cmd.sh', '%s' % client_ip, "%s" % data, api], stdin=subprocess.PIPE,
                         stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    (out, err) = p.communicate()
    assert '{"value":"0"}' in out.decode("utf-8")
    match = re.search(r"{\"value\":\"(?P<nonce_val>\d+)\"}", out.decode("utf-8"))
    assert match
    nonce_val = int(match.group("nonce_val"))
    assert 0 == nonce_val

    api = 'v1/submittransaction'
    data = '{"sender":"1","reciever":"222","nonce":"0","amount":"100"}'
    p = subprocess.Popen(['./kubectl-cmd.sh', '%s' % client_ip, "%s" % data, api], stdin=subprocess.PIPE,
                         stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    (out, err) = p.communicate()
    assert '{"value":"ok"}' in out.decode("utf-8")
