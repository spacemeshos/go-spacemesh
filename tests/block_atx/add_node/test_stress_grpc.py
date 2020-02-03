from pytest_testconfig import config as testconfig

from tests.convenience import sleep_print_backwards
from tests.test_bs import setup_network, add_curl, setup_bootstrap, start_poet, setup_clients, wait_genesis
from tests import queries as q
from tests.test_bs import add_multi_clients


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

    # sleep for 2 epochs
    tts = layers_per_epoch * layer_duration * epochs_to_sleep
    print(f"sleeping for {epochs_to_sleep} epoch")
    sleep_print_backwards(tts)

    # epoch i+2
    print("\ncurrent epoch", epochs_to_sleep)
    _ = add_multi_clients(init_session, setup_network.bootstrap, 1)

    # wait for next epoch
    last_layer = layers_per_epoch * (epochs_to_sleep + 1)
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # epoch i+3
    print("\ncurrent epoch", int(last_layer / layers_per_epoch))
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    # assert that each node has created layer_avg/number_of_nodes
    print(f"validating blocks per nodes up to layer {last_layer}")
    # TODO #@! this should go into a func
    for node in block_map:
        node_layers = block_map[node]["layers"]
        blocks_in_relevant_layers = sum([len(node_layers[str(x)]) for x in range(layers_per_epoch, last_layer)
                                         if str(x) in node_layers])
        # need to deduct blocks created in first genesis epoch since it does not follow general mining rules by design
        blocks_created_per_layer = blocks_in_relevant_layers / (last_layer - layers_per_epoch)
        wanted_avg_block_per_node = max(1, int(layer_avg_size / num_miners))
        assert blocks_created_per_layer / wanted_avg_block_per_node == 1
    # TODO #@!
    # wait an epoch
    last_layer = layers_per_epoch * (epochs_to_sleep + 2)
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)
    # epoch i+4
    print("\ncurrent epoch", int(last_layer / layers_per_epoch))
    num_miners += 1
    print(f"validating blocks per nodes up to layer {last_layer}")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    # assert that each node has created layer_avg/number_of_nodes
    for node in block_map:
        node_lays = block_map[node]["layers"]
        blocks_in_relevant_layers = sum([len(node_lays[str(x)]) for x in range(layers_per_epoch, last_layer)
                                         if str(x) in node_lays])
        # need to deduct blocks created in first genesis epoch since it does not follow general mining rules by design
        blocks_created_per_layer = blocks_in_relevant_layers / (last_layer - layers_per_epoch)
        wanted_avg_block_per_node = max(1, int(layer_avg_size / num_miners))
        assert blocks_created_per_layer / wanted_avg_block_per_node == 1
