from datetime import datetime
import time
from pytest_testconfig import config as testconfig

from tests.convenience import convert_ts_to_datetime
import tests.queries as q
from tests.setup_utils import add_multi_clients
from tests.utils import get_conf

# ==============================================================================
#    TESTS
# ==============================================================================

SYNC_DONE = "sync done"
START_SYNC = "start synchronize"


def test_sync_stress(init_session, setup_bootstrap, save_log_on_exit):
    # currently the only data we have is for 2.5 days, ~700+ layers
    max_time_in_mins = 20
    max_time_for_sync_mins = max_time_in_mins

    clients_num = testconfig["client"]["replicas"]
    bs_info = setup_bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'], testconfig['genesis_delta'])
    _ = add_multi_clients(testconfig, init_session, cspec, clients_num)

    hits = []
    number_of_pods = clients_num + 1  # add 1 for bootstrap pod
    tts = 70
    while len(hits) != number_of_pods:
        print(f"waiting for all clients to finish downloading all files, sleeping for {tts} seconds")
        time.sleep(tts)
        hits = q.get_all_msg_containing(init_session, init_session, "Done downloading")

    del cspec.args['remote-data']
    cspec.args['data-folder'] = ""

    # Adding a single new client
    res_lst = add_multi_clients(testconfig, init_session, cspec, 1, 'client')
    new_client = res_lst[0]

    # wait for the new node to start syncing
    while True:
        start_sync_hits = q.get_all_msg_containing(init_session, new_client, START_SYNC, is_print=False)
        if start_sync_hits:
            print(f"new client started syncing\n")
            break

        tts = 60
        print(f"new client did not start syncing yet sleeping for {tts} secs")
        time.sleep(tts)

    curr_try = 0
    # longest run witnessed ~18:00 minutes (12:00 minutes is the shortest), 2.5 days data, 700+ layers
    max_retries = max_time_in_mins
    interval_time = 60
    print("waiting for new client to be synced")
    while True:
        hits = q.get_all_msg_containing(init_session, new_client, SYNC_DONE, is_print=False)
        if hits:
            print(f"synced after {curr_try}/{max_retries} tries of {interval_time} seconds each\n")
            break

        print(f"not synced after {curr_try}/{max_retries} tries of {interval_time} secs each", end="\r")
        time.sleep(interval_time)

        curr_try += 1
        assert curr_try <= max_retries, f"node failed syncing after waiting for {max_retries} minutes"

    # There are several messages containing "start synchronize" according to Almog,
    # this is due to a bug in the sync test binary.
    # We would like the timestamp of the latest one.
    start_sync_hits = q.get_all_msg_containing(init_session, new_client, START_SYNC, is_print=False)
    last_sync_msg = start_sync_hits[-1]
    # parsing sync start time
    st = convert_ts_to_datetime(last_sync_msg["T"])
    et = convert_ts_to_datetime(hits[0]["T"])

    ass_err = f"it took too long for syncing: {str(et - st)}, max {max_retries} minutes"
    passed_minutes = (et-st).seconds / 60
    assert passed_minutes < max_time_for_sync_mins, ass_err

    # total time since starting sync until finishing
    print(f"new client is synced after {str(et - st)}")
    assert 1
