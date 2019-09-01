import time
from pytest_testconfig import config as testconfig

from tests.client import new_client_in_namespace
from tests.deployment import create_deployment
from tests.fixtures import set_namespace, load_config, init_session, set_docker_images, session_id, DeploymentInfo
from tests.test_bs import setup_clients, save_log_on_exit, setup_bootstrap
from tests.test_bs import current_index, wait_genesis, GENESIS_TIME, CLIENT_DEPLOYMENT_FILE, get_conf
from tests.misc import CoreV1ApiClient
from tests.queries import query_atx_published
from tests.hare.assert_hare import expect_hare


# this is a path for travis's 10m timeout limit
# we reached the timeout because epochDuration happened to be greater than 10m
# duration is in seconds
def sleep_and_print(duration):

    def sleep_and_print_inner(interval):
        print_interval = 30
        if interval <= print_interval:
            print("Sleep for %s seconds" % interval)
            time.sleep(interval)
            return

        print("Sleep for %s seconds" % print_interval)  # print something for travis
        time.sleep(print_interval)
        interval = interval-print_interval
        sleep_and_print_inner(interval)

    print("Going to sleep total of %s seconds" % duration)
    sleep_and_print_inner(duration)
    print("Done")

# ==============================================================================
#    TESTS
# ==============================================================================


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
