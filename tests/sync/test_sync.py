import time
from pytest_testconfig import config as testconfig
from kubernetes import client

from tests.deployment import create_deployment, delete_deployment
from tests.fixtures import set_namespace, load_config, init_session, set_docker_images, session_id, DeploymentInfo, init_session
from tests.test_bs import setup_clients, save_log_on_exit, setup_bootstrap, create_configmap
from tests.test_bs import current_index, wait_genesis, GENESIS_TIME, BOOT_DEPLOYMENT_FILE, CLIENT_DEPLOYMENT_FILE, get_conf
from tests.misc import CoreV1ApiClient
from tests.context import ES
from tests.queries import query_message
from elasticsearch_dsl import Search, Q

# ==============================================================================
#    TESTS
# ==============================================================================


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


def check_pod(podName):
    res = query_message(current_index, testconfig['namespace'], podName, {"M": "sync done"}, False)
    if res:
        return True
    return False


def test_sync_gradually_add_nodes(init_session, setup_bootstrap, save_log_on_exit):
    bs_info = setup_bootstrap.pods[0]

    cspec = get_conf(bs_info, testconfig['client'])

    inf = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 10)

    del cspec.args['remote-data']
    cspec.args['data-folder'] = ""

    num_clients = 4
    clients = [None] * num_clients
    clients[0] = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)
    time.sleep(10)
    clients[1] = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)
    time.sleep(20)
    clients[2] = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)
    time.sleep(20)
    clients[3] = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)

    start = time.time()
    sleep = 30 # seconds
    num_iter = 20 # total of 5 minutes
    for i in range(num_iter):
        done = 0
        for j in range(0, num_clients):
            podName = clients[j].pods[0]['name']
            if not check_pod(podName): # not all done
                print("pod " + podName + " still not done. Going to sleep")
                break # stop check and sleep
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

    delete_deployment(inf.deployment_name, testconfig['namespace'])

    print("it took " + str(end - start) + " to sync all nodes with " + cspec.args['expected-layers'] + "layers")
    print("done!!")
