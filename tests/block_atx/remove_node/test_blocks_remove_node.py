import time

from pytest_testconfig import config as testconfig

from tests import queries as q
from tests.deployment import delete_deployment
from tests.test_bs import setup_network, add_curl, setup_bootstrap, start_poet, setup_clients, wait_genesis, get_conf
from tests.test_bs import add_multi_clients
from tests.utils import validate_blocks_per_nodes, get_pod_id


# epoch i:
# start with x miners
# add another single miner
# wait 3 epochs
#
# epoch i+3:
# validate total miners generated Tavg/x+1 (floored) in i+2
# check if miners created an ATX
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
# validate total miner generated Tavg/x (floored) in i+6
def test_remove_node_validate_atx(init_session, setup_network):
    curr_epoch = 0
    epochs_to_sleep = 3
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    layer_avg_size = int(testconfig['client']['args']['layer-average-size'])

    print("\n-------- adding a new single miner deployment --------")
    bs_info = setup_network.bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client_1'], setup_poet=bs_info["pod_ip"])
    new_pods, dep_name = add_multi_clients(init_session, cspec, 1, client_title="client", ret_dep=True)
    pod_name = new_pods[0]

    print(f"\nlayer duration={layer_duration}, layers per epoch={layers_per_epoch}, layer avg size={layer_avg_size}")

    # add 1 for bs node and another 1 for the single dep client
    num_miners = int(testconfig['client']['replicas']) + 2

    last_layer = epochs_to_sleep * layers_per_epoch
    print(f"-------- wait until epoch number {epochs_to_sleep} to layer {last_layer} --------")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+3 ==========================
    curr_epoch += epochs_to_sleep
    print(f"\n\n-------- current epoch {curr_epoch} --------")

    tts = 10
    print(f"sleeping for {tts} in order to let all nodes enough time to publish ATXs")
    time.sleep(tts)

    print("remove a miner")
    _ = delete_deployment(dep_name, init_session)

    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    # prev_layer = last_layer - layers_per_epoch
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

    # ========================== epoch i+4 ==========================
    curr_epoch += 1
    print(f"\n\n-------- current epoch {curr_epoch} --------")
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)

    ignore_lst = [get_pod_id(init_session, pod_name)]
    # assert that each node has created layer_avg/number_of_nodes
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=ignore_lst)

    # wait an epoch
    prev_layer = last_layer
    last_layer = layers_per_epoch * (curr_epoch + 1)
    print(f"-------- wait until next epoch to layer {last_layer} --------")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+5 ==========================
    curr_epoch += 1
    print(f"\n\n-------- current epoch {curr_epoch} --------")
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)

    # assert that each node has created layer_avg/number_of_nodes
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=ignore_lst)

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

    # assert that each node has created layer_avg/number_of_nodes
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=ignore_lst)

    # wait an epoch
    prev_layer = last_layer
    last_layer = layers_per_epoch * (curr_epoch + 1)
    print(f"wait until next epoch to layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch)

    # ========================== epoch i+7 ==========================
    curr_epoch += 1
    print(f"\n\n-------- current epoch {curr_epoch} --------")
    print(f"-------- validating blocks per nodes up to layer {last_layer} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)

    # remove the removed node from nodes count
    # node was killed after it created an ATX on the 4th epoch (may be changed)
    # other nodes notice no blocks created by the fallen node only
    # 2 epochs after the epoch it was killed in (6th epoch)
    num_miners -= 1
    # assert that each node has created layer_avg/number_of_nodes
    validate_blocks_per_nodes(block_map, prev_layer, last_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=ignore_lst)
