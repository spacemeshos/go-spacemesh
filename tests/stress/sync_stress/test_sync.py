from datetime import datetime
import time
from pytest_testconfig import config as testconfig

from tests.test_bs import add_multi_clients, get_conf, setup_bootstrap, save_log_on_exit
import tests.queries as q
from tests.convenience import convert_ts_to_datetime

# ==============================================================================
#    TESTS
# ==============================================================================

SYNC_DONE = "sync done"
START_SYNC = "start synchronize"


def test_sync_stress(init_session, setup_bootstrap, save_log_on_exit):
    clients_num = 10
    bs_info = setup_bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'])
    _ = add_multi_clients(init_session, cspec, clients_num)

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
    res_lst = add_multi_clients(init_session, cspec, 1, 'client')
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
    # longest run witnessed = 14:30 minutes, 2.5 days data, 700+ layers
    max_retries = 16
    interval_time = 60
    print("waiting for new client to be synced")
    while True:
        hits = q.get_all_msg_containing(init_session, new_client, SYNC_DONE, is_print=False)
        if hits:
            print(f"synced after {curr_try}/{max_retries} tries of {interval_time} seconds each\n")
            break

        print(f"not synced after {curr_try}/{max_retries} tries of {interval_time} secs each", end="\r")
        time.sleep(interval_time)

        assert curr_try <= max_retries, f"node failed syncing after waiting for {max_retries} minutes"
        curr_try += 1

    # there are several messages containing "start synchronize" according to Almog,
    # we would like the timestamp of the latest one.
    start_sync_hits = q.get_all_msg_containing(init_session, new_client, START_SYNC, is_print=False)
    last_sync_msg = start_sync_hits[-1]
    # parsing sync start time
    st = convert_ts_to_datetime(last_sync_msg["T"])
    et = convert_ts_to_datetime(hits[0]["T"])
    # total time since starting sync until finishing
    print(f"new client is synced after {str(et - st)}")
    assert 1
