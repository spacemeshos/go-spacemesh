import base64
from datetime import datetime
import pytest
from pytest_testconfig import config as testconfig
import re
import random
from random import choice
from string import ascii_lowercase
import time

# noinspection PyUnresolvedReferences
import tests.config as cnf
from tests.context import ES
from tests.convenience import sleep_print_backwards
from tests.queries import query_message, poll_query_message
from tests.setup_utils import add_multi_clients
from tests.utils import get_conf, api_call, get_curr_ind


current_index = get_curr_ind()
timeout_factor = 1

# For purposes of these tests, we override the PoetProof protocol
gossip_message_query_fields = {'M': 'gossip message is new, processing', 'protocol': 'PoetProof'}


def query_bootstrap_es(namespace, bootstrap_po_name):
    hits = poll_query_message(current_index, namespace, bootstrap_po_name, {"M": "local node identity"}, expected=1)
    for h in hits:
        if getattr(h, 'key', None):
            return h.key
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
        cspec = get_conf(bs_info, testconfig['client'], testconfig['genesis_delta'])
        client_name = add_multi_clients(testconfig, setup_bootstrap.deployment_id, cspec, 1)[0]
        return client_name

    return _add_single_client()


@pytest.fixture()
def add_clients(setup_bootstrap, setup_clients):
    """
    add_clients returns a function for the user to run in order to add more clients

    :param setup_bootstrap: DeploymentInfo, bootstrap info
    :param setup_clients: DeploymentInfo, client info

    :return: function, _add_client
    """

    def _add_clients(num_of_clients, version=None, version_separator=''):
        # TODO make a generic function that _add_clients can use
        """
        adds a clients to namespace

        :param num_of_clients: int, number of replicas
        :param version: string, the wanted client version
        :param version_separator: string, separator to separate between client key and client version

        :return: list, all created client pods
        """
        if version and not isinstance(version, str):
            raise ValueError("version must be type string")

        if not setup_bootstrap.pods:
            raise Exception("Could not find bootstrap node")

        bs_info = setup_bootstrap.pods[0]

        client_key = 'client'
        if version:
            client_key += f'{version_separator}{version}'

        cspec = get_conf(bs_info, testconfig[client_key], testconfig['genesis_delta'])
        pods_names = add_multi_clients(testconfig, setup_bootstrap.deployment_id, cspec, size=num_of_clients)
        return pods_names

    return _add_clients


# ==============================================================================
#    TESTS
# ==============================================================================

def test_bootstrap(init_session, add_elk, add_node_pool, add_curl, setup_bootstrap):
    print("running test_bootstrap")
    sleep_print_backwards(10 * timeout_factor, "wait for the bootstrap logs to be available in ElasticSearch")
    bs_id = query_bootstrap_es(testconfig['namespace'], setup_bootstrap.pods[0]['name'])
    ass_err = f"setup_bootstrap.pods[0]['key'] = {setup_bootstrap.pods[0]['key']}, bootstrap returned ID: {bs_id}"
    assert setup_bootstrap.pods[0]['key'] == bs_id, ass_err


def test_client(init_session, add_elk, add_node_pool, add_curl, setup_clients, save_log_on_exit):
    fields = {'M': 'discovery_bootstrap'}
    timetowait = len(setup_clients.pods) * timeout_factor
    print(f"Sleeping {str(timetowait)} before checking bootstrap results")
    time.sleep(timetowait)

    peers = poll_query_message(indx=current_index,
                               namespace=testconfig['namespace'],
                               client_po_name=setup_clients.deployment_name,
                               fields=fields,
                               findFails=False,
                               expected=len(setup_clients.pods))

    assert len(peers) == len(setup_clients.pods)


def test_add_client(add_client):
    # Sleep a while before checking the node is bootstrapped
    time.sleep(20 * timeout_factor)
    fields = {'M': 'discovery_bootstrap'}

    hits = poll_query_message(indx=current_index,
                              namespace=testconfig['namespace'],
                              client_po_name=add_client,
                              fields=fields,
                              findFails=True,
                              expected=1)
    assert len(hits) == 1, "Could not find new Client bootstrap message pod:{0}".format(add_client)


def test_add_many_clients(init_session, add_elk, add_node_pool, setup_bootstrap, setup_clients):
    bs_info = setup_bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'], testconfig['genesis_delta'])

    pods = add_multi_clients(testconfig, setup_bootstrap.deployment_id, cspec, size=4)
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


def test_gossip(init_session, add_elk, add_node_pool, setup_clients, add_curl):
    initial = len(query_message(
        current_index, testconfig['namespace'], setup_clients.deployment_name, gossip_message_query_fields))
    # *note*: this already waits for bootstrap so we can send the msg right away.
    # send message to client via rpc
    client_ip = setup_clients.pods[0]['pod_ip']
    podname = setup_clients.pods[0]['name']
    print("Sending gossip from client ip: {0}/{1}".format(podname, client_ip))

    # todo: take out broadcast and rpcs to helper methods.
    api = 'v1/gateway/broadcastpoet'

    # this is messy: this gets passed to curl as a command, so it needs to be a string
    # grpc expects binary data as base64
    data = '{"data":"%s"}' % base64.b64encode(b"foo").decode('utf-8')
    out = api_call(client_ip, data, api, testconfig['namespace'])

    assert "{'status': {}}" in out

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
                               fields=gossip_message_query_fields,
                               findFails=False,
                               expected=total_expected_gossip)

    assert total_expected_gossip == len(after), "test_gossip: Total gossip messages in ES is not as expected"


def test_many_gossip_messages(setup_clients, add_elk, add_node_pool, add_curl):
    initial = len(query_message(
        current_index, testconfig['namespace'], setup_clients.deployment_name, gossip_message_query_fields))

    # *note*: this already waits for bootstrap so we can send the msg right away.
    # send message to client via rpc
    test_messages = 10
    for i in range(test_messages):
        rnd = random.randint(0, len(setup_clients.pods) - 1)
        client_ip = setup_clients.pods[rnd]['pod_ip']
        podname = setup_clients.pods[rnd]['name']
        print("Sending gossip from client ip: {0}/{1}".format(podname, client_ip))

        # todo: take out broadcast and rpcs to helper methods.
        api = 'v1/gateway/broadcastpoet'

        # this is messy: this gets passed to curl as a command, so it needs to be a string
        # grpc expects binary data as base64
        # it doesn't matter what the data contains as long as each is unique
        data = '{"data":"%s"}' % base64.b64encode(i.to_bytes(1, byteorder='big')).decode('utf-8')
        out = api_call(client_ip, data, api, testconfig['namespace'])
        assert "{'status': {}}" in out

        # Need to sleep for a while in order to enable the propagation of the gossip message - 0.5 sec for each node
        # TODO: check frequently before timeout so we might be able to finish earlier.
        gossip_propagation_sleep = 15 * timeout_factor  # currently we expect short propagation times.
        print('sleep for {0} sec to enable gossip propagation'.format(gossip_propagation_sleep))
        time.sleep(gossip_propagation_sleep)

        total_expected_gossip = initial + len(setup_clients.pods) * (i + 1)
        after = poll_query_message(indx=current_index,
                                   namespace=testconfig['namespace'],
                                   client_po_name=setup_clients.deployment_name,
                                   fields=gossip_message_query_fields,
                                   findFails=False,
                                   expected=total_expected_gossip)

        assertion_msg = "test_many_gossip_messages: Total gossip messages in ES is not as expected"
        assert total_expected_gossip == len(after), assertion_msg


def send_msgs(setup_clients, api, headers, total_expected_gossip, msg_size=10000, prop_sleep_time=20, num_of_msg=100,
              expected_ret="{'status': {}}", msg_field="data"):
    """
    sends a protocol message to a random node and asserts its propagation

    :param setup_clients: DeploymentInfo, clients info
    :param api: string, api path
    :param headers: string, protocol header fields
    :param total_expected_gossip: int, expected number of hits result
    :param msg_size: int, message size in bits
    :param prop_sleep_time: int, time to sleep before propagation is done
    :param num_of_msg: int
    :param expected_ret: string, expected query return status
    :param msg_field: string, message field
    currently this argument gets only one value but in the future for a more
    generic function we'll get a list of strings (10.11.19)
    """
    # in our case each pod contains one node
    pods_num = len(setup_clients.pods)
    print("Sending {0} gossip messages".format(num_of_msg))
    for i in range(num_of_msg):
        rnd = random.randint(0, pods_num - 1)
        client_ip = setup_clients.pods[rnd]['pod_ip']
        pod_name = setup_clients.pods[rnd]['name']
        print("Sending gossip {0} from client ip: {1}/{2}".format(i+1, pod_name, client_ip))

        # todo: take out broadcast and rpcs to helper methods.
        msg = "".join(choice(ascii_lowercase) for _ in range(msg_size))
        # TODO in the future this may be changed for a more generic function
        data = '{{"{msg_field}": "{msg}"}}'.format(msg_field=msg_field, msg=msg)
        out = api_call(client_ip, data, api, testconfig['namespace'])
        ass_err = f"test_invalid_msg: expected \"{expected_ret}\" and got \"{out}\""
        assert expected_ret in out, ass_err

    # we expect short propagation times
    gossip_propagation_sleep = (num_of_msg + prop_sleep_time) * timeout_factor
    print('sleep for {0} sec to enable gossip propagation'.format(gossip_propagation_sleep))
    time.sleep(gossip_propagation_sleep)

    # run one global query (for results from all pods), then run one query per pod to figure out which ones failed
    results = poll_query_message(indx=current_index,
                               namespace=testconfig['namespace'],
                               client_po_name=setup_clients.deployment_name,
                               fields=headers,
                               findFails=False,
                               expected=total_expected_gossip)

    # run some additional queries to make debugging easier
    if total_expected_gossip != len(results):
        for pod in setup_clients.pods:
            # query_fields = headers.copy()
            # query_fields['kubernetes.pod_name'] = pod['name']
            results_pod = query_message(current_index, testconfig['namespace'], pod['name'], headers)
            print("Count of results for pod {0}: {1}".format(pod['name'], len(results_pod)))

    err_msg = "msg_testing: Total gossip messages in ES is not as expected"
    err_msg += f"\nexpected {total_expected_gossip}, got {len(results)}"
    assert total_expected_gossip == len(results), err_msg


# Deploy X peers
# Wait for bootstrap
# Broadcast Y messages (make sure that all of them are live simultaneously)
# Validate that all nodes got exactly Y messages (X*Y messages)
# Sample few nodes and validate that they got all 5 messages
def test_many_gossip_sim(setup_clients, add_elk, add_node_pool, add_curl):
    api = 'v1/gateway/broadcastpoet'
    msg_size = 10000  # 1kb TODO: increase up to 2mb
    test_messages = 75 # default 100
    pods_num = len(setup_clients.pods)

    prev_num_of_msg = len(query_message(
        current_index, testconfig['namespace'], setup_clients.deployment_name, gossip_message_query_fields))
    # if msg is valid we should see the message at each node msg * pods(nodes)
    total_expected_gossip = prev_num_of_msg + test_messages * pods_num

    send_msgs(setup_clients, api, gossip_message_query_fields, total_expected_gossip, num_of_msg=test_messages)


def test_broadcast_unknown_protocol(setup_bootstrap, add_elk, add_node_pool, setup_clients, add_curl):
    api = 'v1/gateway/broadcastpoet'
    # protocol is modified
    headers = gossip_message_query_fields.copy()
    headers['protocol'] = 'unknown_protocol'
    msg_size = 10000  # 1kb TODO: increase up to 2mb
    test_messages = 10

    prev_num_of_msg = len(query_message(current_index, testconfig['namespace'], setup_clients.deployment_name, headers))
    # add only the number of previous messages
    # when there's a problem in our protocol we're not even sending
    total_expected_gossip = prev_num_of_msg

    send_msgs(setup_clients, api, headers, total_expected_gossip, num_of_msg=test_messages)


# Different client version on bootstrap:
# Deploy X peers with client version A
# Wait for bootstrap
# Deploy new "v2" peers with client version B<A
# Validate that the new nodes failed to bootstrap
# NOTE: this test is run in the end because it affects the network structure,
# it creates an additional pod with a "v2" client
# ALSO NOTE: The "v2" client is actually running an _earlier_ client version, this is confusing
def test_diff_client_ver(setup_bootstrap, add_elk, add_node_pool, setup_clients, add_curl, add_clients):
    num_of_v2_clients = 2
    v2_version = "v2"

    clients = add_clients(num_of_v2_clients, v2_version)

    # Sleep a while before checking the node is bootstrapped
    time.sleep(20 * timeout_factor)
    headers = {'M': 'discovery_bootstrap'}
    for cl in clients:
        hits = poll_query_message(indx=current_index,
                                  namespace=setup_bootstrap.deployment_id,
                                  client_po_name=cl,
                                  fields=headers,
                                  findFails=False)
        ass_err = f"client is not supposed to discover bootstrap, on: {cl}"
        assert len(hits) == 0, ass_err


# NOTE: this test is run in the end because it affects the network structure,
# it creates more pods and bootstrap them which will affect final query results
# an alternative to that would be to kill the pods when the test ends.
def test_late_bootstraps(init_session, add_elk, add_node_pool, setup_bootstrap, setup_clients):
    TEST_NUM = 10
    testnames = []

    for i in range(TEST_NUM):
        client = add_multi_clients(testconfig, setup_bootstrap.deployment_id,
                                   get_conf(setup_bootstrap.pods[0], testconfig['client'], testconfig['genesis_delta']),
                                   1)
        testnames.append((client[0], datetime.now()))

    # Need to sleep for a while in order to enable the
    # propagation of the gossip message
    time.sleep(TEST_NUM * timeout_factor)

    fields = {'M': 'discovery_bootstrap'}
    for i in testnames:
        hits = poll_query_message(indx=current_index,
                                  namespace=testconfig['namespace'],
                                  client_po_name=i[0],
                                  fields=fields,
                                  findFails=False,
                                  expected=1)

        assert len(hits) == 1, "Could not find new Client bootstrap message. client: {0}".format(i[0])
