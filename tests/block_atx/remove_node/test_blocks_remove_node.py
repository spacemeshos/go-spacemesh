import time

from pytest_testconfig import config as testconfig

from tests import queries as q
from tests.convenience import sleep_print_backwards
from tests.deployment import delete_deployment
from tests.setup_network import setup_mul_network
from tests.utils import validate_blocks_per_nodes, get_pod_id


# epoch 0:
# start with x miners
# wait 2 epochs
#
# epoch 2:
# validate all miners created an ATX
# remove a miner
#
# epoch 3:
# validate total miner generated Tavg/x+1 (floored) in i+3 except for the removed miner
#
# epoch 4:
# validate total miner generated Tavg/x+1 (floored) in i+4 except for the removed miner
#
# epoch 5
# validate total miner generated Tavg/x+1 (floored) in i+5 except for the removed miner
#
# epoch 6
# node was killed after it created an ATX on the 2nd epoch
# other nodes notice no blocks created by the fallen node only
# 3 epochs after the epoch it was killed in (5th epoch)
#
# hence:
# validate total miner generated Tavg/x (floored) in i+5
def test_remove_node_validate_atx(init_session, setup_mul_network):
    epoch_2 = 2
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    layer_avg_size = int(testconfig['client']['args']['layer-average-size'])
    dep_info, api_handler = setup_mul_network
    # in conf yml file second client deployment (clientv2) will show after the
    # first (client) in setup_mul_network.clients hence 1
    new_deployment_info = dep_info.clients[1]
    print(f"\nlayer duration={layer_duration}, layers per epoch={layers_per_epoch}, layer avg size={layer_avg_size}")
    # add 1 for bs node and another 1 for the single dep client
    num_miners = int(testconfig['client']['replicas']) + 2
    api_handler.wait_for_epoch(epoch_2)

    # ========================== epoch 2 ==========================
    print(f"\n\n-------- current epoch {epoch_2} --------")
    sleep_print_backwards(tts=45, msg="sleeping in order to let all nodes enough time to publish ATXs")
    print("-------- validating ATXs creation in current epoch --------")
    atx_hits = q.query_atx_per_epoch(init_session, epoch_2)
    assert len(atx_hits) == num_miners, f"atx hits = {len(atx_hits)}, number of miners = {num_miners}"
    print("remove deployment with single miner")
    single_pod_dep_id = new_deployment_info.deployment_name
    _ = delete_deployment(single_pod_dep_id, init_session)
    ips_for_removal = new_deployment_info.get_ips()
    # TODO: create this func in api handler
    api_handler.remove_ips(ips_for_removal)
    # wait for next epoch
    api_handler.wait_for_next_epoch()

    # ========================== epoch 3 ==========================
    epoch_3 = 3
    print(f"\n\n-------- current epoch {epoch_3} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    single_pod_name = new_deployment_info.pods[0]["name"]
    deleted_pods_lst = [get_pod_id(init_session, single_pod_name)]
    # assert that each node has created layer_avg/number_of_nodes
    print(f"-------- validating blocks per nodes in epoch {epoch_2} --------")
    e2_first_layer, e2_last_layer = api_handler.get_epoch_layer_range(epoch_2, layers_per_epoch)
    validate_blocks_per_nodes(block_map, e2_first_layer, e2_last_layer+1, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=deleted_pods_lst)
    # wait an epoch
    api_handler.wait_for_next_epoch()

    # ========================== epoch 4 ==========================
    epoch_4 = 4
    print(f"\n\n-------- current epoch {epoch_4} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)

    print(f"-------- validating blocks per nodes in epoch {epoch_3} --------")
    # assert that each node has created layer_avg/number_of_nodes
    e3_first_layer, e3_last_layer = api_handler.get_epoch_layer_range(epoch_3, layers_per_epoch)
    validate_blocks_per_nodes(block_map, e3_first_layer, e3_last_layer+1, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=deleted_pods_lst)
    # wait an epoch
    api_handler.wait_for_next_epoch()

    # ========================== epoch 5 ==========================
    epoch_5 = 5
    print(f"\n\n-------- current epoch {epoch_5} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    print(f"-------- validating blocks per nodes in epoch {epoch_4} --------")
    # assert that each node has created layer_avg/number_of_nodes
    e4_first_layer, e4_last_layer = api_handler.get_epoch_layer_range(epoch_4, layers_per_epoch)
    validate_blocks_per_nodes(block_map, e4_first_layer, e4_last_layer+1, layers_per_epoch, layer_avg_size, num_miners-1,
                              ignore_lst=deleted_pods_lst)
    # wait an epoch
    api_handler.wait_for_next_epoch()

    # ========================== epoch 6 ==========================
    curr_epoch = 6
    print(f"\n\n-------- current epoch {curr_epoch} --------")
    block_map, _ = q.get_blocks_per_node_and_layer(init_session)
    # remove the removed node from nodes count
    num_miners -= 1
    e5_first_layer, e5_last_layer = api_handler.get_epoch_layer_range(epoch_5, layers_per_epoch)
    # assert that each node has created layer_avg/number_of_nodes
    print(f"-------- validating blocks per nodes in epoch {epoch_5} --------")
    validate_blocks_per_nodes(block_map, e5_first_layer, e5_last_layer+1, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=deleted_pods_lst)
