from pytest_testconfig import config as testconfig
from tests import queries as q

import time


def test_recorder(init_session, setup_network):
    # git to a certain point in time
    print()
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    num_miners = int(testconfig['client']['replicas']) + 1
    # wait for 2 epochs
    epochs_to_wait = 2
    last_layer = epochs_to_wait * layers_per_epoch
    print(f"wait until second epoch to layer {last_layer}")
    _ = q.wait_for_latest_layer(init_session, last_layer, layers_per_epoch, num_miners)

