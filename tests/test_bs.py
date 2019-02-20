import pytest
import yaml
import time
import re
import subprocess
from os import path
from kubernetes import client, config
from node_info import NodeInfo
from pytest_testconfig import config as testconfig

from elasticsearch import Elasticsearch
from elasticsearch_dsl import Search, Q

BOOT_DEPLOYMENT_FILE='./k8s/bootstrap.yml'
CLIENT_DEPLOYMENT_FILE = './k8s/client.yml'


def update_bootsrap_node_in_client(yml_input, bs_ip, bs_port, bs_key):
    yml_input['spec']['template']['spec']['containers'][0]['args'][-1] = "{0}:{1}/{2}".format(bs_ip, bs_port, bs_key)
    return yml_input


def wait_to_deployment_to_be_ready(deployment_name, name_space):
    total_sleep_time = 0

    while True:
        resp = client.AppsV1Api().read_namespaced_deployment(name=deployment_name, namespace=name_space)
        if not resp.status.unavailable_replicas:
            print("Total time waiting for deployment {0}: {1} sec".format(deployment_name, total_sleep_time))
            break
        time.sleep(1)
        total_sleep_time += 1


def create_deployment(file_name,name_space, **kwargs):
    with open(path.join(path.dirname(__file__), file_name)) as f:
        dep = yaml.safe_load(f)
        if kwargs:
            dep = update_bootsrap_node_in_client(dep,
                                                 kwargs['bootstrap_ip'],
                                                 kwargs['bootstrap_port'],
                                                 kwargs['bootstrap_key'])

        k8s_beta = client.ExtensionsV1beta1Api()
        resp1 = k8s_beta.create_namespaced_deployment(body=dep, namespace=name_space)
        wait_to_deployment_to_be_ready(resp1.metadata._name, name_space)

        return resp1


def delete_deployment(deployment_name, name_space):
    k8s_beta = client.ExtensionsV1beta1Api()
    resp = k8s_beta.delete_namespaced_deployment(name=deployment_name,
                                                 namespace=name_space,
                                                 body=client.V1DeleteOptions(
                                                     propagation_policy='Foreground',
                                                     grace_period_seconds=5))
    return resp


def query_bootstrap_es(indx, namespace, bootstrap_po_name):
    es = Elasticsearch(['http://127.0.0.1:9200'])
    fltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
           Q("match_phrase", kubernetes__pod_name=bootstrap_po_name) & \
           Q("match_phrase", M="Local node identity")
    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits=list(s.scan())
    for h in hits:
        match = re.search(r"Local node identity \w+ (?P<bootstarap_key>\w+)", h.log)
        if match:
            return match.group('bootstarap_key')
    return None


def query_es_client_bootstrap(indx, namespace, client_po_name):
    es = Elasticsearch(['http://127.0.0.1:9200'])
    fltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
           Q("match_phrase", kubernetes__pod_name=client_po_name) & Q("match_phrase", M='discovery_bootstrap')
    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits = list(s.scan())

    print("Number of Hits: {0}".format(len(hits)))
    s=set([h.N for h in hits])
    return (len(s))


def query_es_gossip_message(indx, namespace, client_po_name):
    es = Elasticsearch(['http://127.0.0.1:9200'])
    fltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
           Q("match_phrase", kubernetes__pod_name=client_po_name) & Q("match_phrase", M='new_gossip_message')
    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits = list(s.scan())

    print("Number of gossip Hits: {0}".format(len(hits)))
    s=set([h.N for h in hits])
    return (len(s))


@pytest.fixture(scope='session')
def load_config():
    config.load_kube_config('/Users/isaac/.kube/config')

@pytest.fixture
def setup_bootstrap(request):

    def _setup_bootstrap_in_namespace(name_space):
        global bs_info
        bs_info = NodeInfo()
        resp=create_deployment(BOOT_DEPLOYMENT_FILE, name_space)

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
def setup_clients(request, setup_bootstrap):

    def _setup_clients_in_namespace(name_space):
        global bs_info, client_info
        client_info = NodeInfo()
        resp = create_deployment(CLIENT_DEPLOYMENT_FILE, name_space,
                                 bootstrap_ip=bs_info.bs_pod_ip,
                                 bootstrap_port='7513',
                                 bootstrap_key=bs_info.bs_key)
        client_info.bs_deployment_name = resp.metadata._name
        namespaced_pods = client.CoreV1Api().list_namespaced_pod(namespace=name_space, include_uninitialized=True).items
        client_pods = list(filter(lambda i: i.metadata.name.startswith(client_info.bs_deployment_name), namespaced_pods))

        print("Number of client pods: {0}".format(len(client_pods)))
        for c in client_pods:
            while True:
                resp = client.CoreV1Api().read_namespaced_pod(name=c.metadata.name, namespace=name_space)
                if resp.status.phase != 'Pending':
                    break
                time.sleep(1)
        return client_pods

    def fin():
        global client_info
        delete_deployment(client_info.bs_deployment_name, testconfig['namespace'])

    request.addfinalizer(fin)
    return _setup_clients_in_namespace(testconfig['namespace'])


#==============================================================================
#    TESTS
#==============================================================================

current_index = 'kubernetes_cluster-2019.02.2*'


def test_bootstrap(load_config, setup_bootstrap):
    time.sleep(10)
    assert setup_bootstrap.bs_key == query_bootstrap_es(current_index,
                                                        testconfig['namespace'],
                                                        setup_bootstrap.bs_pod_name)


def test_client(load_config, setup_clients):
    time.sleep(10)
    global client_info
    peers = query_es_client_bootstrap(current_index, testconfig['namespace'], client_info.bs_deployment_name)
    assert len(setup_clients) == peers


def test_gossip(load_config, setup_clients):
    global client_info

    # send message to client via rpc
    client_ip = setup_clients[0].status.pod_ip
    print("Sending gossip from client ip: {0}".format(client_ip))

    api = 'v1/broadcast'
    data = '{"data":"foo"}'
    p = subprocess.Popen(['./kubectl-cmd.sh', '%s' % client_ip, "%s" % data, api], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                         stderr=subprocess.PIPE)
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
