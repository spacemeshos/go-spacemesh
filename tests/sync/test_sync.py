import time
from pytest_testconfig import config as testconfig

from tests.fixtures import set_namespace, load_config, init_session, set_docker_images, session_id
from tests.test_bs import setup_clients, save_log_on_exit, setup_oracle, setup_bootstrap, create_configmap
from tests.test_bs import get_elastic_search_api
from tests.test_bs import current_index, wait_genesis, query_message


# ==============================================================================
#    TESTS
# ==============================================================================


def test_sync_sanity(set_namespace, setup_clients, setup_bootstrap, save_log_on_exit):
    time.sleep(2*60)
    fields2 = {"M": "Validate layer 99"}
    res = query_message(current_index, testconfig['namespace'], setup_bootstrap.pods[0]['name'], fields2, False)
    assert res

    print("done!!")



