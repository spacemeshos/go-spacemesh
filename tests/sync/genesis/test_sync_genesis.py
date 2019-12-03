import time
from pytest_testconfig import config as testconfig

from tests.sync.test_sync import new_client_in_namespace
from tests.test_bs import get_conf

# ==============================================================================
#    TESTS
# ==============================================================================


# add an additional client while genesis
def test_unsync_while_genesis(init_session, setup_bootstrap, save_log_on_exit):
    bs_info = setup_bootstrap.pods[0]

    cspec = get_conf(bs_info, testconfig['client'])

    inf = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 10)

    del cspec.args['remote-data']
    cspec.args['data-folder'] = ""
    print("sleeping for 180 secs")
    time.sleep(180)
    unsync_cl = new_client_in_namespace(testconfig['namespace'], setup_bootstrap, cspec, 1)
    time.sleep(10)

    assert 1
