import random
import re
import time
import pytest
from datetime import datetime
from random import choice
from string import ascii_lowercase

from pytest_testconfig import config as testconfig

# noinspection PyUnresolvedReferences
from tests.context import ES
from tests.fixtures import init_session, load_config, set_namespace, session_id, set_docker_images

from tests.queries import query_message, poll_query_message
from tests.test_bs import add_multi_clients, get_conf
from tests.test_bs import api_call
# noinspection PyUnresolvedReferences
from tests.test_bs import setup_bootstrap, setup_clients, save_log_on_exit, add_curl


dt = datetime.now()
todaydate = dt.strftime("%Y.%m.%d")
current_index = 'kubernetes_cluster-' + todaydate
timeout_factor = 1


def query_bootstrap_es(indx, namespace, bootstrap_po_name):
    hits = poll_query_message(current_index, namespace, bootstrap_po_name, { "M": "Local node identity" }, expected=1)
    for h in hits:
        match = re.search(r"Local node identity >> (?P<bootstrap_key>\w+)", h.M)
        if match:
            return match.group('bootstrap_key')
    return None

# ==============================================================================
#    Fixtures
# ==============================================================================


# The following fixture should not be used if you wish to add many clients during test.
@pytest.fixture()
def add_client(request, setup_bootstrap, setup_clients):
    global client_name

    def _add_single_client():
        global client_name
        if not setup_bootstrap.pods:
            raise Exception("Could not find bootstrap node")

        bs_info = setup_bootstrap.pods[0]
        cspec = get_conf(bs_info, testconfig['client'])
        client_name = add_multi_clients(setup_bootstrap.deployment_id, cspec, 1)[0]
        return client_name

    return _add_single_client()


# ==============================================================================
#    TESTS
# ==============================================================================

def test_bootstrap(init_session, setup_bootstrap):
    # wait for the bootstrap logs to be available in ElasticSearch
    time.sleep(10 * timeout_factor)
    assert setup_bootstrap.pods[0]['key'] == query_bootstrap_es(current_index,
                                                                testconfig['namespace'],
                                                                setup_bootstrap.pods[0]['name'])


def test_client(init_session, setup_clients, add_curl, save_log_on_exit):
    fields = {'M': 'discovery_bootstrap'}
    timetowait = len(setup_clients.pods) * timeout_factor
    print("Sleeping " + str(timetowait) + " before checking out bootstrap results")
    time.sleep(timetowait)

    peers = poll_query_message(indx=current_index,
                               namespace=testconfig['namespace'],
                               client_po_name=setup_clients.deployment_name,
                               fields=fields,
                               findFails=False,
                               expected=len(setup_clients.pods))

    assert len(peers) == len(setup_clients.pods)


def test_add_client(add_client):
    # Sleep a while before checking the node is bootstarped
    time.sleep(20 * timeout_factor)
    fields = {'M': 'discovery_bootstrap'}

    hits = poll_query_message(indx=current_index,
                              namespace=testconfig['namespace'],
                              client_po_name=add_client,
                              fields=fields,
                              findFails=True,
                              expected=1)
    assert len(hits) == 1, "Could not find new Client bootstrap message pod:{0}".format(add_client)


def test_add_many_clients(init_session, setup_bootstrap, setup_clients):
    bs_info = setup_bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'])

    pods = add_multi_clients(setup_bootstrap.deployment_id, cspec, size=4)
    time.sleep(40 * timeout_factor)  # wait for the new clients to finish bootstrap and for logs to get to elasticsearch
    fields = {'M': 'discovery_bootstrap'}
    for p in pods:
        hits = poll_query_message(indx=current_index,
                                  namespace=testconfig['namespace'],
                                  client_po_name=p,
                                  fields=fields,
                                  findFails=True,
                                  expected=1)
        assert len(hits) == 1, "Could not find new Client bootstrap message pod:{0}".format(p)


def test_gossip(init_session, setup_clients, add_curl):
    fields = {'M': 'new_gossip_message', 'protocol': 'api_test_gossip'}
    initial = len(query_message(current_index, testconfig['namespace'], setup_clients.deployment_name, fields))
    # *note*: this already waits for bootstrap so we can send the msg right away.
    # send message to client via rpc
    client_ip = setup_clients.pods[0]['pod_ip']
    podname = setup_clients.pods[0]['name']
    print("Sending gossip from client ip: {0}/{1}".format(podname, client_ip))

    # todo: take out broadcast and rpcs to helper methods.
    api = 'v1/broadcast'
    data = '{"data":"foo"}'
    out = api_call(client_ip, data, api, testconfig['namespace'])

    assert "{'value': 'ok'}" in out

    # Need to sleep for a while in order to enable the propagation of the gossip message - 0.5 sec for each node
    # TODO: check frequently before timeout so we might be able to finish earlier.
    gossip_propagation_sleep = len(
        setup_clients.pods) * timeout_factor / 2  # currently we expect short propagation times.
    print('sleep for {0} sec to enable gossip propagation'.format(gossip_propagation_sleep))
    time.sleep(gossip_propagation_sleep)

    total_expected_gossip = initial + len(setup_clients.pods)
    after = poll_query_message(indx=current_index,
                               namespace=testconfig['namespace'],
                               client_po_name=setup_clients.deployment_name,
                               fields=fields,
                               findFails=False,
                               expected=total_expected_gossip)

    assert total_expected_gossip == len(after), "test_gossip: Total gossip messages in ES is not as expected"


def test_many_gossip_messages(setup_clients, add_curl):
    fields = {'M': 'new_gossip_message', 'protocol': 'api_test_gossip'}
    initial = len(query_message(current_index, testconfig['namespace'], setup_clients.deployment_name, fields))

    # *note*: this already waits for bootstrap so we can send the msg right away.
    # send message to client via rpc
    TEST_MESSAGES = 10
    for i in range(TEST_MESSAGES):
        rnd = random.randint(0, len(setup_clients.pods) - 1)
        client_ip = setup_clients.pods[rnd]['pod_ip']
        podname = setup_clients.pods[rnd]['name']
        print("Sending gossip from client ip: {0}/{1}".format(podname, client_ip))

        # todo: take out broadcast and rpcs to helper methods.
        api = 'v1/broadcast'
        data = '{"data":"foo' + str(i) + '"}'
        out = api_call(client_ip, data, api, testconfig['namespace'])
        assert "{'value': 'ok'}" in out

        # Need to sleep for a while in order to enable the propagation of the gossip message - 0.5 sec for each node
        # TODO: check frequently before timeout so we might be able to finish earlier.
        gossip_propagation_sleep = 15 * timeout_factor  # currently we expect short propagation times.
        print('sleep for {0} sec to enable gossip propagation'.format(gossip_propagation_sleep))
        time.sleep(gossip_propagation_sleep)

        total_expected_gossip = initial + len(setup_clients.pods) * (i + 1)
        after = poll_query_message(indx=current_index,
                                   namespace=testconfig['namespace'],
                                   client_po_name=setup_clients.deployment_name,
                                   fields=fields,
                                   findFails=False,
                                   expected=total_expected_gossip)

        assert total_expected_gossip == len(after), "test_many_gossip_messages: Total gossip messages in ES is not as expected"


def test_many_gossip_sim(setup_clients, add_curl):
    msg_size = 10000  # 1kb TODO: increase up to 2mb
    fields = {'M': 'new_gossip_message', 'protocol': 'api_test_gossip'}
    TEST_MESSAGES = 100

    initial = len(query_message(current_index, testconfig['namespace'], setup_clients.deployment_name, fields))

    for i in range(TEST_MESSAGES):
        rnd = random.randint(0, len(setup_clients.pods) - 1)
        client_ip = setup_clients.pods[rnd]['pod_ip']
        podname = setup_clients.pods[rnd]['name']
        print("Sending gossip from client ip: {0}/{1}".format(podname, client_ip))

        # todo: take out broadcast and rpcs to helper methods.
        start = datetime.utcnow()
        api = 'v1/broadcast'
        msg = "".join(choice(ascii_lowercase) for i in range(msg_size))
        data = '{"data":"' + msg + '"}'
        out = api_call(client_ip, data, api, testconfig['namespace'])
        assert "{'value': 'ok'}" in out

    gossip_propagation_sleep = (TEST_MESSAGES + 20) * timeout_factor  # currently we expect short propagation times.
    print('sleep for {0} sec to enable gossip propagation'.format(gossip_propagation_sleep))
    time.sleep(gossip_propagation_sleep)

    total_expected_gossip = initial + len(setup_clients.pods) * TEST_MESSAGES
    after = poll_query_message(indx=current_index,
                               namespace=testconfig['namespace'],
                               client_po_name=setup_clients.deployment_name,
                               fields=fields,
                               findFails=False,
                               expected=total_expected_gossip)

    assert total_expected_gossip == len(after), "test_many_gossip_sim: Total gossip messages in ES is not as expected"


# - Deploy X peers
# - Wait for bootstrap
# - Broadcast 5 messages (make sure that all of them are live simultaneously)
# - Validate that all nodes got exactly 5 messages
# - Sample few nodes and validate that they got all 5 messages
def test_msg_rcv(setup_bootstrap, setup_clients):
    pass


# NOTE : this test is ran in the end because it affects the network structure,
# it creates more pods and bootstrap them which will affect final query results
# an alternative to that would be to kill the pods when the test ends.
def test_late_bootstraps(init_session, setup_bootstrap, setup_clients):
    # Sleep a while before checking the node is bootstarped
    TEST_NUM = 10
    testnames = []

    for i in range(TEST_NUM):
        client = add_multi_clients(setup_bootstrap.deployment_id,
                                   get_conf(setup_bootstrap.pods[0], testconfig['client']),
                                   1)
        testnames.append((client[0], datetime.now()))

    time.sleep(TEST_NUM * timeout_factor)

    fields = {'M': 'discovery_bootstrap'}
    for i in testnames:
        hits = poll_query_message(indx=current_index,
                                  namespace=testconfig['namespace'],
                                  client_po_name= i[0],
                                  fields=fields,
                                  findFails=False,
                                  expected=1)

        assert len(hits) == 1, "Could not find new Client bootstrap message. client: {0}".format(i[0])
