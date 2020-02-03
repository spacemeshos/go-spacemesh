from pytest_testconfig import config as testconfig

from tests import queries as q
from tests.convenience import sleep_print_backwards
from tests.test_bs import setup_network, add_curl, setup_bootstrap, start_poet, setup_clients, wait_genesis, get_conf
from tests.test_bs import add_multi_clients
from tests.utils import validate_blocks_per_nodes


# epoch i:
# start with x miners
# wait 2 epochs
#
# epoch i+2:
# add a miner
#
# epoch i+3
# validate total miner generated Tavg/x (floored) in i+2
# validate new miner created an ATX
# wait an epoch
#
# epoch i+4:
# validate total miner generated Tavg/x+1 (floored) in i+3
def test_add_node_validate_atx(init_session, setup_network):
    epochs_to_sleep = 2
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    layer_avg_size = int(testconfig['client']['args']['layer-average-size'])
    num_miners = int(testconfig['client']['replicas']) + 1  # add 1 for bs node

    print(f"\nlayer duration={layer_duration}, layers per epoch={layers_per_epoch}, layer avg size={layer_avg_size}")
    # sleep for 2 epochs
    tts = layers_per_epoch * layer_duration * epochs_to_sleep
    print(f"sleeping for {epochs_to_sleep} epoch")
    sleep_print_backwards(tts)

    # epoch i+2
    print("\n\n-------- current epoch", epochs_to_sleep, "--------")
    print("adding a new miner")
    bs_info = setup_network.bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'])
    _ = add_multi_clients(init_session, cspec, 1)

    # wait for next epoch
    print("wait until next epoch")
    last_layer = layers_per_epoch * (epochs_to_sleep + 1)
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # epoch i+3
    print("\n\n-------- current epoch", int(last_layer / layers_per_epoch), "--------")
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)

    # assert that each node has created layer_avg/number_of_nodes
    validate_blocks_per_nodes(block_map, last_layer, layers_per_epoch, layer_avg_size, num_miners)

    # wait an epoch
    last_layer = layers_per_epoch * (epochs_to_sleep + 2)
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)
    # epoch i+4
    print("\n\n-------- current epoch", int(last_layer / layers_per_epoch), "--------")
    num_miners += 1
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    # assert that each node has created layer_avg/number_of_nodes
    validate_blocks_per_nodes(block_map, last_layer, layers_per_epoch, layer_avg_size, num_miners)
