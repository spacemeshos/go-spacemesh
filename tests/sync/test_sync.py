import time
from pytest_testconfig import config as testconfig
from kubernetes import client
from tests.deployment import create_deployment, delete_deployment
from tests import queries
from tests.conftest import DeploymentInfo
from tests.test_bs import save_log_on_exit, setup_bootstrap
from tests.test_bs import api_call, add_multi_clients, get_conf
from tests.test_bs import current_index, CLIENT_DEPLOYMENT_FILE, get_conf
from tests.misc import CoreV1ApiClient
from tests.context import ES
from tests.queries import query_message
from elasticsearch_dsl import Search, Q
from tests.pod import delete_pod

# ==============================================================================
#    TESTS
# ==============================================================================


PERSISTENT_DATA={"M": "persistent data found"}
SYNC_DONE={"M": "sync done"}

def new_client_in_namespace(name_space, setup_bootstrap, cspec, num):
    resp = create_deployment(CLIENT_DEPLOYMENT_FILE, name_space,
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


def check_pod_logs(podName, str):
    res = query_message(current_index, testconfig['namespace'], podName, str, False)
    if res:
        return True
    return False


def test_sync_gradually_add_nodes(init_session, setup_bootstrap, save_log_on_exit):
    bs_info = setup_bootstrap.pods[0]

    cspec = get_conf(bs_info, testconfig['client'])
    cspec2 = get_conf(bs_info, testconfig['clientv2'])

    inf = add_multi_clients(init_session, cspec, 10)

    del cspec.args['remote-data']
    del cspec.args['data-folder']

    num_clients = 4
    clients = [None] * num_clients
    clients[0] = add_multi_clients(init_session, cspec2, 1, 'clientv2')[0]
    time.sleep(10)
    clients[1] = add_multi_clients(init_session, cspec, 1, 'client')[0]
    time.sleep(20)
    clients[2] = add_multi_clients(init_session, cspec, 1, 'client')[0]
    time.sleep(20)
    clients[3] = add_multi_clients(init_session, cspec, 1, 'client')[0]

    print("take pod down ", clients[0])

    delete_pod(testconfig['namespace'], clients[0])

    print("sleep for 20 sec")
    time.sleep(20)

    print("waiting for pods to be done with sync")

    start = time.time()
    sleep = 30  # seconds
    num_iter = 20  # total of 5 minutes
    for i in range(num_iter):
        done = 0
        for j in range(0, num_clients):
            podName = clients[j]
            if not check_pod_logs(podName, SYNC_DONE):  # not all done
                print("pod " + podName + " still not done. Going to sleep")
                break  # stop check and sleep
            else:
                print("pod " + podName + " done")
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
