import time
from pytest_testconfig import config as testconfig

from tests.test_bs import add_multi_clients, get_conf, setup_bootstrap, save_log_on_exit
import tests.queries as q

# ==============================================================================
#    TESTS
# ==============================================================================

SYNC_DONE = "sync done"


def test_sync_stress(init_session, setup_bootstrap, save_log_on_exit):
    bs_info = setup_bootstrap.pods[0]
    cspec = get_conf(bs_info, testconfig['client'])

    clients = add_multi_clients(init_session, cspec, 10)

    hits = []
    number_of_pods = len(clients) + 1  # add 1 for bootstrap pod
    tts = 60
    while len(hits) != number_of_pods:
        print(f"not all clients finished downloading all files, sleeping for {tts} seconds")
        time.sleep(tts)
        hits = q.get_all_msg_containing(init_session, init_session, "finished downloading")

    del cspec.args['remote-data']
    cspec.args['data-folder'] = ""

    # Adding a new client
    res_lst = add_multi_clients(init_session, cspec, 1, 'client')
    new_client = res_lst[0]

    curr_try = 0
    max_retries = 2880  # two days in minutes
    interval_time = 60
    wait_fmt = "not synced after {} tries of {} secs each, total of: {} secs"
    print("waiting for new pod to be synced")
    while True:
        print(wait_fmt.format(curr_try, interval_time, interval_time*curr_try), end="\r")
        time.sleep(interval_time)

        hits = q.get_all_msg_containing(init_session, new_client, SYNC_DONE, is_print=False)
        if hits:
            break

        if curr_try >= max_retries:
            assert 0, "node failed syncing!"

        curr_try += 1

    print(f"new client is synced after {interval_time*curr_try} seconds, success!")
    assert 1
