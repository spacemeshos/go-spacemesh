import time
from pytest_testconfig import config as testconfig
from kubernetes import client

from tests.deployment import create_deployment, delete_deployment
from tests.fixtures import set_namespace, load_config, init_session, set_docker_images, session_id, DeploymentInfo, init_session
from tests.test_bs import setup_clients, save_log_on_exit, setup_bootstrap, create_configmap, add_curl
from tests.test_bs import current_index, wait_genesis, GENESIS_TIME, BOOT_DEPLOYMENT_FILE, CLIENT_DEPLOYMENT_FILE, get_conf
from tests.misc import CoreV1ApiClient
from tests.queries import ES, query_message, query_atx_published
from elasticsearch_dsl import Search, Q
from tests.hare.assert_hare import expect_hare


# ==============================================================================
#    TESTS
# ==============================================================================

def setup_clients_in_namespace(namespace, bs_deployment_info, client_deployment_info, client_config,
                               oracle=None, poet=None, dep_time_out=120):

    cspec = get_conf(bs_deployment_info, client_config, oracle, poet)

    resp = deployment.create_deployment(CLIENT_DEPLOYMENT_FILE, namespace,
                                        deployment_id=client_deployment_info.deployment_id,
                                        replica_size=client_config['replicas'],
                                        container_specs=cspec,
                                        time_out=dep_time_out)

    client_deployment_info.deployment_name = resp.metadata._name
    client_pods = (
        CoreV1ApiClient().list_namespaced_pod(namespace,
                                              include_uninitialized=True,
                                              label_selector=("name={0}".format(
                                                  client_deployment_info.deployment_name.split('-')[1]))).items)

    client_deployment_info.pods = [{'name': c.metadata.name, 'pod_ip': c.status.pod_ip} for c in client_pods]
    return client_deployment_info

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


# this is a path for travis's 10m timeout limit
# we reached the timeout because epochDuration happened to be greater than 10m
# duration is in seconds
def sleep_and_print(duration):
    print("Going to sleep total of %s" % duration)

    interval = 30 # each 30 seconds
    if duration <= interval:
        time.sleep(duration)
        return

    iters = int(duration / interval)
    print("Number of iterations is %s" % iters)
    for i in range(0, iters):
        print("Going to sleep for %s seconds" % interval) # print something for travis
        time.sleep(interval) # sleep interval

    # sleep the rest
    rem = duration % interval
    print("Going to sleep %s seconds" % rem)
    time.sleep(rem)
    print("Done")

def test_add_delayed_nodes(init_session, setup_bootstrap, save_log_on_exit):
    bs_info = setup_bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'], None, setup_bootstrap.pods[0]['pod_ip'])
    ns = testconfig['namespace']

    layerDuration = int(testconfig['client']['args']['layer-duration-sec'])
    layersPerEpoch = int(testconfig['client']['args']['layers-per-epoch'])
    epochDuration = layerDuration*layersPerEpoch

    # start with 20 miners
    startCount = 20
    inf = new_client_in_namespace(ns, setup_bootstrap, cspec, startCount)
    sleep_and_print(epochDuration) # wait epoch duration

    # add 10 each epoch
    numToAdd = 10
    count = 4
    clients = [None] * count
    for i in range(0, count):
        clients[i] = new_client_in_namespace(ns, setup_bootstrap, cspec, numToAdd)
        print("Added client batch ", i, clients[i].pods[i]['name'])
        sleep_and_print(epochDuration)

    print("Done adding clients. Going to wait for two epochs")
    # wait two more epochs
    sleep_and_print(2*epochDuration)

    # total = bootstrap + first clients + added clients
    total = 1 + startCount + count * numToAdd
    totalEpochs = 1 + count + 2
    totalLayers = layersPerEpoch * totalEpochs
    firstLayerOfLastEpoch = totalLayers-layersPerEpoch
    f = int(testconfig['client']['args']['hare-max-adversaries'])

    # validate
    print("Waiting one layer for logs")
    time.sleep(layerDuration) # wait one layer for logs to propagate

    print("Running validation")
    expect_hare(current_index, ns, firstLayerOfLastEpoch, totalLayers-1, total, f) # validate hare
    atxLastEpoch = query_atx_published(current_index, ns, firstLayerOfLastEpoch)
    assert len(atxLastEpoch) == total # validate #atxs in last epoch
