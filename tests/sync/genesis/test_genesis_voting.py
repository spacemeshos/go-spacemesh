import time
import pytest
from pytest_testconfig import config as testconfig

from tests.convenience import print_hits_entry_count
import tests.queries as q
from tests.sync.test_sync import new_client_in_namespace
from tests.utils import get_conf

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
def test_unsync_while_genesis(init_session, setup_bootstrap, start_poet, add_curl):
    time_to_create_block_since_startup = 10
    time_before_first_block = testconfig["genesis_delta"] + time_to_create_block_since_startup
    layers_to_wait = 4

    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    bs_info = setup_bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'], testconfig['genesis_delta'], setup_oracle=None,
                     setup_poet=setup_bootstrap.pods[0]['pod_ip'])

    # Create a cluster of nodes
    _ = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 9)

    # Sleep to enable block creation
    print(f"sleeping for {time_before_first_block} seconds in order to enable blocks to be published\n")
    time.sleep(time_before_first_block)

    # Validate a block was published
    nodes_published_block, _ = q.get_blocks_per_node_and_layer(init_session)
    assert nodes_published_block, f"no blocks were published during the first {time_before_first_block} seconds"

    # Create a new node in cluster
    unsynced_cl = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)

    # Sleep until layers_to_wait layer, default is 4
    print(f"sleeping for {layer_duration * layers_to_wait} seconds\n")
    time.sleep(layer_duration * layers_to_wait)

    # Found by Almogs: "validate votes failed" error has occurred following a known bug
    print("validating no 'validate votes failed' messages has arrived")
    hits_val_failed = q.get_all_msg_containing(init_session, init_session, "validate votes failed")
    assert hits_val_failed == [], 'got a "validate votes" failed message'
    print("validation succeeded")

    # Get the msg when app started on the late node
    app_started_hits = q.get_app_started_msgs(init_session, unsynced_cl.pods[0]["name"])
    assert app_started_hits, f"app did not start for new node after {layers_to_wait} layers"

    # Check if the new node has finished syncing
    hits_synced = q.get_done_syncing_msgs(init_session, unsynced_cl.pods[0]["name"])
    assert hits_synced, f"New node did not sync, waited for {layers_to_wait} layers"

    print(f"{hits_synced[0].kubernetes.pod_name} has performed sync")

    # validate no new blocks were received before being synced
    sync_ts = hits_synced[0].T
    app_started_ts = app_started_hits[0].T

    hits_msg_block = q.get_block_creation_msgs(init_session, unsynced_cl.pods[0]["name"], from_ts=app_started_ts,
                                               to_ts=sync_ts)

    if hits_msg_block:
        print("\n\n############ WARNING: node created blocks before syncing!!!! ############\n\n")

    hits_errors = q.find_error_log_msgs(init_session, init_session)
    if hits_errors:
        print_hits_entry_count(hits_errors, "message")
        # assert 0, "found log errors"

    print("successfully finished")
    assert 1
