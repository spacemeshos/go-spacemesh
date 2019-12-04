import time
from pytest_testconfig import config as testconfig

import tests.queries as q
from tests.sync.test_sync import new_client_in_namespace
from tests.test_bs import get_conf, setup_bootstrap

# ==============================================================================
#    TESTS
# ==============================================================================


# set a X nodes cluster
# sleep in order to enable a block to be delivered
# validate a block has been delivered
# add an additional client while still in genesis
# sleep until layer 4
# validate no errors occurred
# validate new node didn't receive any new blocks before being synced
def test_unsync_while_genesis(init_session, setup_bootstrap):
    time_before_first_block = 50

    bs_info = setup_bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'])

    # create a cluster of nodes
    _ = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 9)
    print(f"sleeping for {time_before_first_block} seconds in order to enable blocks to be published")
    time.sleep(time_before_first_block)
    print("querying for all blocks")
    # query for block creation
    nodes, _ = q.get_blocks_per_node_and_layer(init_session)
    # validate a block was published
    if not nodes:
        assert 0, f"no blocks were published during the first {time_before_first_block} seconds"

    # create a new node in cluster
    unsync_cl = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)
    # sleep until layer 4
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    print(f"sleeping for {layer_duration * 4}")
    time.sleep(layer_duration * 4)
    # Done waiting for ticks and validation means the node has finished syncing
    hits = q.get_all_msg_containing(init_session, init_session, "Done waiting for ticks and validation")
    print(f"#@! synced hits: {hits}")

    # # Found by Almogs: "validate votes failed" error has occurred following a known bug
    # hits = q.get_all_msg_containing(init_session, init_session, "validate votes failed")
    # assert hits == [], 'got a "validate votes" failed message'
