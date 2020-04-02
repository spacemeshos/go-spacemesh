from elasticsearch_dsl import Search, Q
from kubernetes import client
from pytest_testconfig import config as testconfig
import time

from tests import queries
from tests.deployment import create_deployment
from tests import config as conf
from tests.conftest import DeploymentInfo
from tests.misc import CoreV1ApiClient
from tests.context import ES
from tests.queries import query_message
from tests.pod import delete_pod
from tests.setup_utils import add_multi_clients
from tests.utils import get_conf, get_curr_ind

# ==============================================================================
#    TESTS
# ==============================================================================


PERSISTENT_DATA={"M": "persistent data found"}
SYNC_DONE={"M": "sync done"}


def new_client_in_namespace(name_space, setup_bootstrap, cspec, num):
    resp = create_deployment(conf.CLIENT_DEPLOYMENT_FILE, name_space,
                             deployment_id=setup_bootstrap.deployment_id,
                             replica_size=num,
                             container_specs=cspec,
                             time_out=testconfig['deployment_ready_time_out'])
    client_info = DeploymentInfo(dep_id=setup_bootstrap.deployment_id)
    client_info.deployment_name = resp.metadata._name
    namespaced_pods = CoreV1ApiClient().list_namespaced_pod(namespace=name_space, include_uninitialized=True).items
    client_pods = list(filter(lambda i: i.metadata.name.startswith(client_info.deployment_name), namespaced_pods))

    client_info.pods = [{'name': c.metadata.name, 'pod_ip': c.status.pod_ip} for c in client_pods]
    print("Number of client pods: {0}".format(len(client_info.pods)))

    for c in client_info.pods:
        while True:
            resp = CoreV1ApiClient().read_namespaced_pod(name=c['name'], namespace=name_space)
            if resp.status.phase != 'Pending':
                break
            time.sleep(1)
    return client_info


def search_pod_logs(namespace, pod_name, term):
    current_index = get_curr_ind()
    api = ES().get_search_api()
    fltr = Q("match_phrase", kubernetes__pod_name=pod_name) & Q("match_phrase", kubernetes__namespace_name=namespace)
    s = Search(index=current_index, using=api).query('bool').filter(fltr).sort("time")
    res = s.execute()
    full = Search(index=current_index, using=api).query('bool').filter(fltr).sort("time").extra(size=res.hits.total)
    res = full.execute()
    hits = list(res.hits)
    print("Writing ${0} log lines for pod {1} ".format(len(hits), pod_name))
    with open('./logs/' + pod_name + '.txt', 'w') as f:
        for i in hits:
            if term in i.log:
                return True
    return False


def check_pod_logs(pod_name, data):
    current_index = get_curr_ind()
    res = query_message(current_index, testconfig['namespace'], pod_name, data, False)
    if res:
        return True
    return False


def test_sync_gradually_add_nodes(init_session, setup_bootstrap, save_log_on_exit):
    current_index = get_curr_ind()
    bs_info = setup_bootstrap.pods[0]

    gen_delt = testconfig['genesis_delta']
    cspec = get_conf(bs_info, testconfig['client'], gen_delt)
    cspec2 = get_conf(bs_info, testconfig['clientv2'], gen_delt)

    inf = add_multi_clients(testconfig, init_session, cspec, 10)

    del cspec.args['remote-data']
    del cspec.args['data-folder']

    num_clients = 4
    clients = [None] * num_clients
    clients[0] = add_multi_clients(testconfig, init_session, cspec2, 1, 'clientv2')[0]
    time.sleep(10)
    clients[1] = add_multi_clients(testconfig, init_session, cspec, 1, 'client')[0]
    time.sleep(20)
    clients[2] = add_multi_clients(testconfig, init_session, cspec, 1, 'client')[0]
    time.sleep(20)
    clients[3] = add_multi_clients(testconfig, init_session, cspec, 1, 'client')[0]

    print("take pod down ", clients[0])

    delete_pod(testconfig['namespace'], clients[0])

    print("sleep for 20 sec")
    time.sleep(20)

    print("waiting for pods to be done with sync")

    start = time.time()
    sleep = 30  # seconds
    num_iter = 25  # total of 5 minutes
    for i in range(num_iter):
        done = 0
        for j in range(0, num_clients):
            pod_name = clients[j]
            if not check_pod_logs(pod_name, SYNC_DONE):  # not all done
                print("pod " + pod_name + " still not done. Going to sleep")
                break  # stop check and sleep
            else:
                print("pod " + pod_name + " done")
                done = done + 1

        if done == num_clients:
            print("all pods done")
            break

        print("not done yet sleep for " + str(sleep) + " seconds")
        time.sleep(sleep)

    assert done == num_clients

    end = time.time()

    check_pod_logs(clients[0], PERSISTENT_DATA)
    queries.assert_equal_layer_hashes(current_index, testconfig['namespace'])

    print("it took " + str(end - start) + " to sync all nodes with " + cspec.args['expected-layers'] + "layers")
    print("done!!")
