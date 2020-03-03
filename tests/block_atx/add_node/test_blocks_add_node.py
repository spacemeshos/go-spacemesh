from pytest_testconfig import config as testconfig

from tests import queries as q
from tests.setup_utils import add_multi_clients
from tests.setup_network import setup_network
from tests.utils import validate_blocks_per_nodes, get_pod_id, get_conf


# epoch i:
# start with x miners
# wait 2 epochs
#
# epoch i+2:
# add a miner
#
# epoch i+3
# validate total miner generated Tavg/x (floored) in i+2
#
# epoch i+4:
# validate total miner generated Tavg/x (floored) in i+3
# validate number of ATXs = num_miner + 1 in i+3
#
# epoch i+5
# validate total miner generated Tavg/x (floored) in i+4
#
# epoch i+6
# validate total miner generated Tavg/x+1 (floored) in i+5
def test_add_node_validate_atx(init_session, setup_network):
    curr_epoch = 0
    epochs_to_sleep = 2
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    layer_avg_size = int(testconfig['client']['args']['layer-average-size'])
    num_miners = int(testconfig['client']['replicas']) + 1  # add 1 for bs node

    print(f"\nlayer duration={layer_duration}, layers per epoch={layers_per_epoch}, layer avg size={layer_avg_size}")
    # wait for 2 epochs
    last_layer = epochs_to_sleep * layers_per_epoch
    print(f"wait until second epoch to layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+2 ==========================
    curr_epoch += epochs_to_sleep
    print("\n\n-------- current epoch", curr_epoch, "--------")
    print("adding a new miner")
    bs_info = setup_network.bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'], testconfig['genesis_delta'])
    new_pod_name = add_multi_clients(testconfig, init_session, cspec, 1)[0]

    # wait for next epoch
    last_layer = layers_per_epoch * (curr_epoch + 1)
    print(f"wait until next epoch to layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+3 ==========================
    curr_epoch += 1
    print("\n\n-------- current epoch", curr_epoch, "--------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    # we're querying for block creation without epoch constrain, this will result
    # with epochs where new or deleted nodes will return 0 blocks in certain epochs
    # we should ignore those
    new_pod_id = get_pod_id(init_session, new_pod_name)
    ignore_lst = [new_pod_id]
    validate_blocks_per_nodes(block_map, 0, last_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=ignore_lst)

    # wait an epoch
    prev_layer = last_layer
    last_layer = layers_per_epoch * (curr_epoch + 1)
    print(f"wait until next epoch to layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+4 ==========================
    curr_epoch += 1
    print("\n\n-------- current epoch", curr_epoch, "--------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    # assert that each node has created layer_avg/number_of_nodes
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=ignore_lst)

    print("-------- validating all nodes ATX creation in last epoch --------")
    atx_hits = q.query_atx_per_epoch(init_session, curr_epoch - 1)
    assert len(atx_hits) == num_miners + 1  # add 1 for new miner
    print("-------- validation succeed --------")

    last_layer = layers_per_epoch * (curr_epoch + 2)
    print(f"wait 2 epochs for layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+6 ==========================
    curr_epoch += 2
    print("\n\n-------- current epoch", curr_epoch, "--------")
    # previous epoch all nodes are supposed to know our new node ATX
    num_miners += 1
    # assert each node has created layer_avg/number_of_nodes
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    prev_layer = last_layer - layers_per_epoch
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners)
