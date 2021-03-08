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
    dep_info, api_handler = setup_network
    # TODO: this should be 1 not 0
    curr_epoch = 0
    epochs_to_sleep = 2
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    layer_avg_size = int(testconfig['client']['args']['layer-average-size'])
    num_miners = int(testconfig['client']['replicas']) + 1  # add 1 for bs node

    print(f"\nlayer duration={layer_duration}, layers per epoch={layers_per_epoch}, layer avg size={layer_avg_size}")
    # wait for 2 epochs
    api_handler.wait_for_epoch(epochs_to_sleep)

    # ========================== epoch 2 ==========================
    curr_epoch += epochs_to_sleep
    print("\n\n-------- current epoch", curr_epoch, "--------")
    print("adding a new miner")
    bs_info = dep_info.bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'], testconfig['genesis_delta'])
    new_pod_name_list, new_pod_ip_list = add_multi_clients(testconfig, init_session, cspec, 1, ret_ip=True)
    # adding nodes IP to api handler ip list for future communication
    api_handler.extend_ips(new_pod_ip_list)
    # wait for next epoch
    last_layer = layers_per_epoch * (curr_epoch + 1)
    api_handler.wait_for_next_epoch()

    # ========================== epoch 3 ==========================
    epoch_2 = curr_epoch
    curr_epoch += 1
    new_pod_id = get_pod_id(init_session, new_pod_name_list[0])
    # check if new client published ATX in epoch i+2
    new_pod_published_atx_epoch_2 = q.node_published_atx(init_session, new_pod_id, epoch_2)
    print("\n\n-------- current epoch", curr_epoch, "--------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    # we're querying for block creation without epoch constrain, this will result
    # with epochs where new or deleted nodes will return 0 blocks in certain epochs
    # we should ignore those
    ignore_lst = [new_pod_id]
    first_layer = epochs_to_sleep * layers_per_epoch
    atx_epoch_2 = q.query_atx_per_epoch(init_session, curr_epoch - 1)
    print(f"found {len(atx_epoch_2)} ATXs in epoch {curr_epoch-1}")
    validate_blocks_per_nodes(block_map, first_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=ignore_lst)
    # wait an epoch
    prev_layer = last_layer
    last_layer = layers_per_epoch * (curr_epoch + 1)
    print(f"wait until next epoch to layer {last_layer}")
    api_handler.wait_for_next_epoch()

    # ========================== epoch 4 ==========================
    # TODO: move curr epoch here and add last epoch variable
    epoch_3 = curr_epoch
    curr_epoch += 1
    new_pod_published_atx_epoch_3 = q.node_published_atx(init_session, new_pod_id, epoch_3)
    last_layer = layers_per_epoch * (curr_epoch + 1)
    api_handler.wait_for_layer(last_layer, timeout=layers_per_epoch * layer_duration + 10)
    print("\n\n-------- current epoch", curr_epoch, "--------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    # assert that each node has created layer_avg/number_of_nodes
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")

    atx_epoch_3 = q.query_atx_per_epoch(init_session, curr_epoch - 1)
    print(f"found {len(atx_epoch_3)} ATXs in epoch {curr_epoch-1}")

    # As new client was started in the epoch i+2, it may or may not publish an ATX in that epoch.
    # The amount of miners depends on this condition.
    num_miners_epoch_4 = num_miners + 1 if new_pod_published_atx_epoch_2 else num_miners
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners_epoch_4,
                              ignore_lst=ignore_lst)

    print("-------- validating all nodes ATX creation in last epoch --------")
    atx_hits = q.query_atx_per_epoch(init_session, curr_epoch - 1)

    print(f"found {len(atx_hits)} ATXs in epoch {curr_epoch-1}")

    # Sometimes, the new client doesn't create an ATX in the next epochs.
    expected_miners_epoch_3 = num_miners + 1 if new_pod_published_atx_epoch_3 else num_miners

    assert len(atx_hits) == expected_miners_epoch_3

    print("-------- validation succeed --------")

    last_layer = layers_per_epoch * (curr_epoch + 2)
    print(f"wait 2 epochs for layer {last_layer}")
    api_handler.wait_for_layer(last_layer, timeout=layers_per_epoch * layer_duration * 2 + 10)

    new_pod_published_atx_epoch_4 = q.node_published_atx(init_session, new_pod_id, curr_epoch)

    # ========================== epoch 6 ==========================
    curr_epoch += 2
    print("\n\n-------- current epoch", curr_epoch, "--------")

    # assert each node has created layer_avg/number_of_nodes
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    prev_layer = last_layer - layers_per_epoch

    atx_epoch_4 = q.query_atx_per_epoch(init_session, curr_epoch - 2)
    print(f"found {len(atx_epoch_4)} ATXs in epoch {curr_epoch-2}")

    num_miners_epoch_6 = num_miners + 1 if new_pod_published_atx_epoch_4 else num_miners
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners_epoch_6)

