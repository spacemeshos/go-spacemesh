import time
from datetime import datetime, timedelta
from pytest_testconfig import config as testconfig
from tests.fixtures import init_session, load_config, set_namespace, session_id, set_docker_images
from tests.test_bs import query_bootstrap_es, setup_bootstrap, setup_oracle, setup_poet, create_configmap, setup_clients
from tests.test_bs import query_message, save_log_on_exit, add_client, api_call, add_curl

ELASTICSEARCH_URL = "http://{0}".format(testconfig['elastic']['host'])


# ==============================================================================
#    TESTS
# ==============================================================================

dt = datetime.now()
todaydate = dt.strftime("%Y.%m.%d")
current_index = 'kubernetes_cluster-' + todaydate


def test_bootstrap(setup_bootstrap):
    # wait for the bootstrap logs to be available in ElasticSearch
    time.sleep(5)
    assert setup_bootstrap.pods[0]['key'] == query_bootstrap_es(current_index,
                                                                testconfig['namespace'],
                                                                setup_bootstrap.pods[0]['name'])


def test_client(setup_clients, save_log_on_exit):
    fields = {'M':'discovery_bootstrap'}
    timetowait = len(setup_clients.pods)/2
    print("Sleeping " + str(timetowait) + " before checking out bootstrap results")
    time.sleep(timetowait)
    peers = query_message(current_index, testconfig['namespace'], setup_clients.deployment_name, fields, True)
    assert len(set(peers)) == len(setup_clients.pods)


def test_add_client(add_client):

    # Sleep a while before checking the node is bootstarped
    time.sleep(20)
    fields = {'M': 'discovery_bootstrap'}
    hits = query_message(current_index, testconfig['namespace'], add_client, fields, True)
    assert len(hits) == 1, "Could not find new Client bootstrap message"


def test_gossip(setup_clients, add_curl):
    fields = {'M':'new_gossip_message', 'protocol': 'api_test_gossip'}
    # *note*: this already waits for bootstrap so we can send the msg right away.
    # send message to client via rpc
    client_ip = setup_clients.pods[0]['pod_ip']
    podname = setup_clients.pods[0]['name']
    print("Sending gossip from client ip: {0}/{1}".format(podname, client_ip))

    # todo: take out broadcast and rpcs to helper methods.
    api = 'v1/broadcast'
    data = '{"data":"foo"}'
    out = api_call(client_ip, data, api, testconfig['namespace'])
    assert '{"value":"ok"}' in out.decode("utf-8")

    # Need to sleep for a while in order to enable the propagation of the gossip message - 0.5 sec for each node
    # TODO: check frequently before timeout so we might be able to finish earlier.
    gossip_propagation_sleep = len(setup_clients.pods) / 2 # currently we expect short propagation times.
    print('sleep for {0} sec to enable gossip propagation'.format(gossip_propagation_sleep))
    time.sleep(gossip_propagation_sleep)

    peers_for_gossip = query_message(current_index, testconfig['namespace'], setup_clients.deployment_name, fields, True)
    assert len(setup_clients.pods) == len(set(peers_for_gossip))