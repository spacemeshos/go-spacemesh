import time
from datetime import datetime, timedelta

import pytz
from pytest_testconfig import config as testconfig

from tests import queries
from tests.misc import add_multi_clients, get_conf
from tests.pod import delete_pod
from tests.queries import current_index
from tests.test_bs import save_log_on_exit, setup_bootstrap


GENESIS_TIME = pytz.utc.localize(datetime.utcnow() + timedelta(seconds=testconfig['genesis_delta'])).isoformat('T', 'seconds')

def test_kill_node(init_session, setup_bootstrap, save_log_on_exit):
    bs_info = setup_bootstrap.pods[0]

    cspec = get_conf(GENESIS_TIME, bs_info, testconfig['client'])
    cspec2 = get_conf(GENESIS_TIME, bs_info, testconfig['clientv2'])
    bootstrap = get_conf(GENESIS_TIME, bs_info, testconfig['bootstrap'])

    #del cspec.args['remote-data']
    #cspec.args['data-folder'] = ""
    #num_clients = 3

    clients = [add_multi_clients(testconfig, init_session, bootstrap, 1, 'bootstrap'),
               add_multi_clients(testconfig, init_session, cspec, 48, 'client')]
    time.sleep(20)
    to_be_killed = add_multi_clients(testconfig, init_session, cspec2, 1, 'clientv2')[0]
    clients.append(to_be_killed)
    layer_avg_size = testconfig['client']['args']['layer-average-size']
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    # check only third epoch
    epochs = 3
    last_layer = epochs * layers_per_epoch

    queries.wait_for_latest_layer(testconfig["namespace"], last_layer, layers_per_epoch)

    print("take pod down ", clients[0])
    delete_pod(testconfig['namespace'], clients[0])

    queries.poll_query_message(current_index, testconfig['namespace'], to_be_killed, {"M": "sync done"})

    queries.assert_equal_layer_hashes(current_index, testconfig['namespace'])
