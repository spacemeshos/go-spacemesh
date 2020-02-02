from pytest_testconfig import config as testconfig

from tests.convenience import sleep_print_backwards
from tests.test_bs import setup_network, add_curl, setup_bootstrap, start_poet, setup_clients, wait_genesis
from tests import queries as q
from tests.test_bs import add_multi_clients


# epoch i:
# start with x miners
# wait an epoch
#
# epoch i+1:
# validate total miner generated Tavg/x (floored)
# add a miner
# validate miner created an ATX
# wait an epoch
#
# epoch i+2:
# validate total miner generated Tavg/x+1 (floored)
def test_add_node_validate_atx(init_session, setup_network):
    ns = init_session
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    num_miners = int(testconfig['client']['replicas'])

    # epoch i
    tts = layers_per_epoch * layer_duration
    print("sleeping for an epoch")
    sleep_print_backwards(tts)

    # epoch i+1
    create_msg_hits = q.get_block_creation_msgs(ns, ns)
    num_of_created_blocks = len(create_msg_hits)
    assert num_of_created_blocks ==
