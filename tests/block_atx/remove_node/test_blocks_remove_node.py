import time

from pytest_testconfig import config as testconfig

from tests import queries as q
from tests.deployment import delete_deployment
from tests.setup_network import setup_mul_network
from tests.utils import validate_blocks_per_nodes, get_pod_id


# epoch i:
# start with x miners
# add another single miner
# wait 3 epochs
#
# epoch i+3:
# validate total miners generated Tavg/x+1 (floored) in i+1 and i+2
# validate all miners created an ATX
# remove the last added miner
#
# epoch i+4:
# validate total miner generated Tavg/x+1 (floored) in i+3 except for the removed miner
#
# epoch i+5:
# validate total miner generated Tavg/x+1 (floored) in i+4 except for the removed miner
#
# epoch i+6
# validate total miner generated Tavg/x+1 (floored) in i+5 except for the removed miner
#
# epoch i+7
# node was killed after it created an ATX on the 3rd epoch (may be changed)
# other nodes notice no blocks created by the fallen node only
# 3 epochs after the epoch it was killed in (6th epoch)
#
# hence:
# validate total miner generated Tavg/x (floored) in i+6
def test_remove_node_validate_atx(init_session, setup_mul_network):
    curr_epoch = 0
    epochs_to_sleep = 2
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    layer_avg_size = int(testconfig['client']['args']['layer-average-size'])

    new_deployment_info = setup_mul_network.clients[1]
    print(f"\nlayer duration={layer_duration}, layers per epoch={layers_per_epoch}, layer avg size={layer_avg_size}")

    # add 1 for bs node and another 1 for the single dep client
    num_miners = int(testconfig['client']['replicas']) + 2

    last_layer = epochs_to_sleep * layers_per_epoch
    print(f"-------- wait until epoch number {epochs_to_sleep} to layer {last_layer} --------")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+2 ==========================
    curr_epoch += epochs_to_sleep
    print(f"\n\n-------- current epoch {curr_epoch} --------")

    tts = 15
    print(f"sleeping for {tts} in order to let all nodes enough time to publish ATXs")
    time.sleep(tts)

    print("remove deployment with single miner")
    # in conf yml file second client deployment (client_1) will show after the
    # first (client) in setup_mul_network.clients hence 1
    single_pod_dep_id = new_deployment_info.deployment_name
    _ = delete_deployment(single_pod_dep_id, init_session)

    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    validate_blocks_per_nodes(block_map, 0, last_layer, layers_per_epoch, layer_avg_size, num_miners)

    print("-------- validating all nodes ATX creation in last epoch --------")
    atx_hits = q.query_atx_per_epoch(init_session, curr_epoch)
    assert len(atx_hits) == num_miners
    print("-------- validation succeed --------")

    # wait for next epoch
    prev_layer = last_layer
    last_layer = layers_per_epoch * (curr_epoch + 1)
    print(f"-------- wait until next epoch to layer {last_layer} --------")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+3 ==========================
    curr_epoch += 1
    print(f"\n\n-------- current epoch {curr_epoch} --------")
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)

    single_pod_name = new_deployment_info.pods[0]["name"]
    deleted_pods_lst = [get_pod_id(init_session, single_pod_name)]
    # assert that each node has created layer_avg/number_of_nodes
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=deleted_pods_lst)

    # wait an epoch
    prev_layer = last_layer
    last_layer = layers_per_epoch * (curr_epoch + 1)
    print(f"-------- wait until next epoch to layer {last_layer} --------")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+4 ==========================
    curr_epoch += 1
    print(f"\n\n-------- current epoch {curr_epoch} --------")
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)

    # assert that each node has created layer_avg/number_of_nodes
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=deleted_pods_lst)

    # wait an epoch
    prev_layer = last_layer
    last_layer = layers_per_epoch * (curr_epoch + 1)
    print(f"wait until next epoch to layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+5 ==========================
    curr_epoch += 1
    print(f"\n\n-------- current epoch {curr_epoch} --------")
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)

    # assert that each node has created layer_avg/number_of_nodes
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=deleted_pods_lst)

    # wait an epoch
    prev_layer = last_layer
    last_layer = layers_per_epoch * (curr_epoch + 1)
    print(f"wait until next epoch to layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+6 ==========================
    curr_epoch += 1
    print(f"\n\n-------- current epoch {curr_epoch} --------")
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)

    # remove the removed node from nodes count
    num_miners -= 1
    # assert that each node has created layer_avg/number_of_nodes
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=deleted_pods_lst)
