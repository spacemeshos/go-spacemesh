from pytest_testconfig import config as testconfig

from tests import queries as q
from tests.setup_utils import add_multi_clients
from tests.setup_network import setup_network
from tests.utils import validate_beacons, get_pod_id, get_conf


def test_tortoise_beacon(init_session, setup_network):
    print(f"tortoise beacon system test started")

    curr_epoch = 0
    epochs_to_sleep = 2
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    layer_avg_size = int(testconfig['client']['args']['layer-average-size'])
    num_miners = int(testconfig['client']['replicas']) + 1  # add 1 for bs node

    print(
        f"\nlayer duration={layer_duration}, layers per epoch={layers_per_epoch}, layer avg size={layer_avg_size}")
    # wait for 2 epochs
    last_layer = epochs_to_sleep * layers_per_epoch
    print(f"wait until second epoch to layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch, num_miners)

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
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch, num_miners + 1)

    # ========================== epoch i+3 ==========================
    curr_epoch += 1
    print("\n\n-------- current epoch", curr_epoch, "--------")

    # wait an epoch
    last_layer = layers_per_epoch * (curr_epoch + 1)
    print(f"wait until next epoch to layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch, num_miners + 1)

    # ========================== epoch i+4 ==========================
    curr_epoch += 1
    print("\n\n-------- current epoch", curr_epoch, "--------")

    last_layer = layers_per_epoch * (curr_epoch + 2)
    print(f"wait 2 epochs for layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch, num_miners + 1)

    # ========================== epoch i+6 ==========================
    curr_epoch += 2
    print("\n\n-------- current epoch", curr_epoch, "--------")

    print(f"-------- validating tortoise beacon --------")
    beacon_messages = q.get_beacon_msgs(init_session, init_session)

    validate_beacons(beacon_messages)
    print("-------- tortoise beacon validation succeed --------")