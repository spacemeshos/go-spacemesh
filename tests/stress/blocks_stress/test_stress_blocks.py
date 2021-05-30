from pytest_testconfig import config as testconfig

import tests.analyse as analyse
from tests.queries import wait_for_latest_layer
from tests.setup_network import setup_network


def test_blocks_stress(init_session, setup_network):
    epochs_to_wait = 4
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    layer_avg_size = int(testconfig['client']['args']['layer-average-size'])

    number_of_cl = int(testconfig['client']['replicas'])
    number_of_cl += 1  # add bootstrap node

    last_layer = layers_per_epoch * epochs_to_wait
    wait_for_latest_layer(init_session, last_layer, layers_per_epoch, number_of_cl)
    analyse.analyze_mining(init_session, epochs_to_wait, layers_per_epoch, layer_avg_size, number_of_cl)
