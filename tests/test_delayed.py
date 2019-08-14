import time
from pytest_testconfig import config as testconfig
from kubernetes import client

from tests.deployment import create_deployment, delete_deployment
from tests.fixtures import set_namespace, load_config, init_session, set_docker_images, session_id, DeploymentInfo, init_session
from tests.test_bs import setup_clients, save_log_on_exit, setup_bootstrap, create_configmap
from tests.test_bs import current_index, wait_genesis, GENESIS_TIME, BOOT_DEPLOYMENT_FILE, CLIENT_DEPLOYMENT_FILE, get_conf
from tests.misc import CoreV1ApiClient
from tests.queries import ES, query_message
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


def test_add_delayed_nodes(init_session, setup_bootstrap, save_log_on_exit):
    bs_info = setup_bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'])

    layerDuration = int(testconfig['client']['args']['layer-duration-sec'])
    layersPerEpoch = int(testconfig['client']['args']['layers-per-epoch'])

    # start with 50 miners
    inf = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 50)

    # add 50 each epoch
    inf1 = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)
    time.sleep(10)



    inf2 = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)
    time.sleep(20)
    inf3 = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)
    time.sleep(20)
    inf4 = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)

    fields = {"M": "sync done"}

    start = time.time()

    for i in range(20):
        res = query_message(current_index, testconfig['namespace'], inf4.pods[0]['name'], fields, False)
        if res:
            print("last pod finished")
            print("asserting all pods ...")
            break
        sleep = 1
        print("not done yet sleep for " + str(sleep) + " minute")
        time.sleep(sleep * 60)

    end = time.time()

    assert res
    print("pod " + inf4.pods[0]['name'] + " done")

    res1 = query_message(current_index, testconfig['namespace'], inf1.pods[0]['name'], fields, False)
    assert res1
    print("pod " + inf1.pods[0]['name'] + " done")

    res2 = query_message(current_index, testconfig['namespace'], inf2.pods[0]['name'], fields, False)
    assert res2
    print("pod " + inf2.pods[0]['name'] + " done")

    res3 = query_message(current_index, testconfig['namespace'], inf3.pods[0]['name'], fields, False)
    assert res3
    print("pod " + inf3.pods[0]['name'] + " done")

    delete_deployment(inf.deployment_name, testconfig['namespace'])

    print("it took " + str(end - start) + "to sync all nodes with " + cspec.args['expected-layers'] + "layers")
    print("done!!")
