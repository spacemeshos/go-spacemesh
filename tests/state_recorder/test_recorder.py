from pytest_testconfig import config as testconfig

from tests import queries as q
from tests.node_api import shutdown_miners
from tests.gcloud.storage.storage_handler import upload_whole_network
from tests.setup_network import setup_network
from tests.utils import exec_on_pod


def test_recorder(init_session, setup_network):
    # collect all client ips
    ips_for_shutdown = [pod['pod_ip'] for pod in setup_network.clients.pods]
    # add BS node ip
    ips_for_shutdown.append(setup_network.bootstrap.pods[0]['pod_ip'])
    # wait for a certain point in time
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    num_miners = int(testconfig['client']['replicas']) + 1
    # wait for 2 epochs
    epochs_to_wait = 3
    last_layer = epochs_to_wait * layers_per_epoch
    print(f"wait until second epoch to layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch, num_miners, interval=2)
    # shutdown all miners
    print("shutting down all miners")
    shutdown_miners(init_session, ips_for_shutdown)
    # shutdown poet
    # TODO: move this to a function #@!qq
    cmd = ["/bin/sh", "-c", "./bin/grpc_shutdown"]
    pod_name = setup_network.bootstrap.pods[0]['name']
    print("shutting down poet")
    exec_on_pod(pod_name, init_session, cmd, "poet")
    upload_whole_network(init_session)

