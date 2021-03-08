from pytest_testconfig import config as testconfig

from tests import queries as q
from tests.assertions.mesh_assertion import assert_blocks_num_in_epochs
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
    epoch_2 = 2
    epochs_to_sleep = 2
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    layer_avg_size = int(testconfig['client']['args']['layer-average-size'])
    num_miners = int(testconfig['client']['replicas']) + 1  # add 1 for bs node

    print(f"\nlayer duration={layer_duration}, layers per epoch={layers_per_epoch}, layer avg size={layer_avg_size}")
    dep_info, api_handler = setup_network
    # wait for 2 epochs
    api_handler.wait_for_epoch(epoch_2)

    # ========================== epoch 2 ==========================
    curr_epoch += epochs_to_sleep
    print("\n\n-------- current epoch", curr_epoch, "--------")
    print("adding a new miner")
    bs_info = dep_info.bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'], testconfig['genesis_delta'])
    new_pod_name = add_multi_clients(testconfig, init_session, cspec, 1)[0]
    api_handler.wait_for_next_epoch()

    # ========================== epoch 3 ==========================
    curr_epoch += 1
    print("\n\n-------- current epoch", curr_epoch, "--------")
    # check if new client published ATX in epoch 2
    new_pod_id = get_pod_id(init_session, new_pod_name)
    new_pod_published_atx_epoch_2 = q.node_published_atx(init_session, new_pod_id, epoch_2)
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    # we're querying for block creation without epoch constrain, this will result
    # with epochs where new or deleted nodes will return 0 blocks in certain epochs
    # we should ignore those
    ignore_lst = [new_pod_id]
    atx_epoch_2 = q.query_atx_per_epoch(init_session, epoch_2)
    print(f"found {len(atx_epoch_2)} ATXs in epoch {epoch_2}")
    e2_first_layer, e2_last_layer = api_handler.get_epochs_layer_range(epoch_2, layers_per_epoch)
    print(f"-------- validating blocks per nodes in epoch {epoch_2} --------")
    assert_blocks_num_in_epochs(block_map, e2_first_layer, e2_last_layer + 1, layers_per_epoch, layer_avg_size,
                                num_miners, ignore_lst=ignore_lst)
    # wait an epoch
    api_handler.wait_for_next_epoch()

    # ========================== epoch 4 ==========================
    curr_epoch += 1
    print(f"\n\n-------- current epoch {curr_epoch} --------")
    epoch_3 = 3
    # TODO: why are we checking if new node created an ATX in epoch 3, we should assume it did,
    #  should this be an assertion?
    new_pod_published_atx_epoch_3 = q.node_published_atx(init_session, new_pod_id, epoch_3)
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    atx_epoch_3 = q.query_atx_per_epoch(init_session, epoch_3)
    print(f"found {len(atx_epoch_3)} ATXs in epoch {epoch_3}")
    # As new client was started in epoch 2, it may or may not published an ATX in that same epoch.
    # The amount of miners created blocks in epoch 3 depends on this condition.
    num_miners_epoch_4 = num_miners + 1 if new_pod_published_atx_epoch_2 else num_miners
    e3_first_layer, e3_last_layer = api_handler.get_epochs_layer_range(epoch_3, layers_per_epoch)
    print(f"-------- validating blocks per nodes in epoch {epoch_3} --------")
    # assert that each node has created layer_avg/number_of_nodes
    assert_blocks_num_in_epochs(block_map, e3_first_layer, e3_last_layer + 1, layers_per_epoch, layer_avg_size,
                                num_miners_epoch_4, ignore_lst=ignore_lst)
    # ATX validation in epoch 3
    atx_hits = q.query_atx_per_epoch(init_session, epoch_3)
    print(f"found {len(atx_hits)} ATXs in epoch {epoch_3}")
    # Sometimes, the new client doesn't create an ATX in the next epochs.
    expected_miners_epoch_3 = num_miners + 1 if new_pod_published_atx_epoch_3 else num_miners
    print("-------- validating all nodes ATX creation in last epoch --------")
    assert len(atx_hits) == expected_miners_epoch_3
    print("validation succeed")
    epoch_6 = 6
    api_handler.wait_for_epoch(epoch_6)

    # ========================== epoch 6 ==========================
    curr_epoch = epoch_6
    print("\n\n-------- current epoch", curr_epoch, "--------")
    epoch_4 = 4
    new_pod_published_atx_epoch_4 = q.node_published_atx(init_session, new_pod_id, epoch_4)
    # assert each node has created layer_avg/number_of_nodes
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    atx_epoch_4 = q.query_atx_per_epoch(init_session, epoch_4)
    print(f"found {len(atx_epoch_4)} ATXs in epoch {epoch_4}")
    num_miners_epoch_6 = num_miners + 1 if new_pod_published_atx_epoch_4 else num_miners
    print(f"-------- validating blocks per nodes in epoch {epoch_4} --------")
    e4_first_layer, e4_last_layer = api_handler.get_epochs_layer_range(epoch_4, layers_per_epoch)
    assert_blocks_num_in_epochs(block_map, e4_first_layer, e4_last_layer + 1, layers_per_epoch, layer_avg_size,
                                num_miners_epoch_6)
