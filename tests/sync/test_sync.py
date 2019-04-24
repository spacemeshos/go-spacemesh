import time

from pytest_testconfig import config as testconfig

from tests.fixtures import set_namespace, load_config, bootstrap_deployment_info, client_deployment_info
from tests.test_bs import setup_clients, save_log_on_exit, setup_oracle, setup_bootstrap, create_configmap
from tests.test_bs import get_elastic_search_api
from tests.test_bs import current_index, wait_genesis, query_message

# ==============================================================================
#    TESTS
# ==============================================================================


def test_sync_sanity(set_namespace, setup_clients, setup_bootstrap, save_log_on_exit):
    time.sleep(5*60)

    fields = {"M": "loaded 100 layers from disk error getting layer 101 from database"}
    res = query_message(current_index, testconfig['namespace'], setup_clients.pods[0]['name'], fields, False)
    assert res

    fields2 = {"M": "Validate layer 99"}
    res = query_message(current_index, testconfig['namespace'], setup_bootstrap.pods[0]['name'], fields2, False)
    assert res

    print("synced2 !!!!")




