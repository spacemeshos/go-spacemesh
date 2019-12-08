import time
from pytest_testconfig import config as testconfig

import tests.queries as q
from tests.sync.test_sync import new_client_in_namespace
from tests.test_bs import get_conf, setup_bootstrap, start_poet

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
def test_unsync_while_genesis(init_session, setup_bootstrap, start_poet):
    time_before_first_block = 55
    layers_to_wait = 4
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])

    bs_info = setup_bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'], None, setup_bootstrap.pods[0]['pod_ip'])

    # create a cluster of nodes
    _ = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 9)
    print(f"sleeping for {time_before_first_block} seconds in order to enable blocks to be published")
    time.sleep(time_before_first_block)
    print("querying for all blocks")
    nodes_published_block, _ = q.get_blocks_per_node_and_layer(init_session)
    # validate a block was published
    if not nodes_published_block:
        assert 0, f"no blocks were published during the first {time_before_first_block} seconds"

    # create a new node in cluster
    unsynced_cl = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)

    # sleep until layer num layers_to_wait
    print(f"sleeping for {layer_duration * layers_to_wait} seconds")
    time.sleep(layer_duration * layers_to_wait)
    # Done waiting for ticks and validation means the node has finished syncing

    # # Found by Almogs: "validate votes failed" error has occurred following a known bug
    # hits = q.get_all_msg_containing(init_session, init_session, "validate votes failed")
    # assert hits == [], 'got a "validate votes" failed message'
    print("querying for 'Done waiting for ticks' message")
    hits = q.get_all_msg_containing(init_session, init_session, "Done waiting for ticks and validation")

    if not hits:
        assert 0, f"New node did not sync after {layers_to_wait} layers"
    # since we only created one late node we can assume there will be only one result for done waiting query
    if len(hits) > 1:
        assert 0, f"found more than one node who performed synchronization.\nnodes names{[n.name for n in hits]}"

    print("query has positively returned a single match, validating results")
    # validate that the matched node with "Done waiting for ticks and validation" is the same
    # as the one we added late
    unsync_pod_name = unsynced_cl.pods[0]["name"]
    done_waiting_pod_name = hits[0].kubernetes.pod_name
    ass_err = f"unsynced pod name, {unsync_pod_name} and newly synced node name, {done_waiting_pod_name} does not match"
    assert unsync_pod_name == done_waiting_pod_name, ass_err

