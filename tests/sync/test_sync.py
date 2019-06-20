import time
from pytest_testconfig import config as testconfig
from kubernetes import client

from tests.deployment import create_deployment, delete_deployment
from tests.fixtures import set_namespace, load_config, init_session, set_docker_images, session_id, DeploymentInfo, init_session
from tests.test_bs import setup_poet, setup_clients, save_log_on_exit, setup_oracle, setup_bootstrap, create_configmap
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


def test_sync_gradually_add_nodes(set_namespace, setup_bootstrap, save_log_on_exit, init_session):
    bs_info = setup_bootstrap.pods[0]

    cspec = get_conf(bs_info)

    inf = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 10)

    cspec.args['data-folder'] = ""

    inf1 = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)
    time.sleep(10)
    inf2 = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)
    time.sleep(20)
    inf3 = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)
    time.sleep(20)
    inf4 = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)

    time.sleep(3*60)

    fields = {"M": "sync done"}
    res1 = query_message(current_index, testconfig['namespace'], inf1.pods[0]['name'], fields, False)
    res2 = query_message(current_index, testconfig['namespace'], inf2.pods[0]['name'], fields, False)
    res3 = query_message(current_index, testconfig['namespace'], inf3.pods[0]['name'], fields, False)
    res4 = query_message(current_index, testconfig['namespace'], inf4.pods[0]['name'], fields, False)
    assert res1
    assert res2
    assert res3
    assert res4

    delete_deployment(inf.deployment_name, testconfig['namespace'])

    print("done!!")
