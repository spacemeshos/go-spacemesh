import time
from pytest_testconfig import config as testconfig

from tests.client import new_client_in_namespace
from tests.deployment import delete_deployment
from tests.fixtures import set_namespace, load_config, init_session, set_docker_images, session_id, DeploymentInfo, init_session
from tests.test_bs import setup_clients, save_log_on_exit, setup_bootstrap, create_configmap
from tests.test_bs import current_index, wait_genesis, GENESIS_TIME, get_conf
from tests.context import ES
from tests.queries import query_message
from elasticsearch_dsl import Search, Q


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

# ==============================================================================
#    TESTS
# ==============================================================================


def test_sync_gradually_add_nodes(init_session, setup_bootstrap, save_log_on_exit):
    bs_info = setup_bootstrap.pods[0]

    cspec = get_conf(bs_info, testconfig['client'])
    time_out = testconfig['deployment_ready_time_out']
    inf = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 10, time_out)

    del cspec.args['remote-data']
    cspec.args['data-folder'] = ""

    num_clients = 4
    clients = [None] * num_clients
    clients[0] = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1, time_out)
    time.sleep(10)
    clients[1] = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1, time_out)
    time.sleep(20)
    clients[2] = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1, time_out)
    time.sleep(20)
    clients[3] = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1, time_out)

    start = time.time()
    sleep = 30 # seconds
    num_iter = 10 # total of 5 minutes
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
