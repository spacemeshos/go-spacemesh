import time

from pytest_testconfig import config as testconfig

from tests.hare.assert_hare import assert_all
from tests.test_bs import current_index, setup_clients, setup_oracle, setup_poet, setup_bootstrap, create_configmap, \
    wait_genesis, save_log_on_exit
from tests.fixtures import init_session, load_config, set_namespace, session_id, set_docker_images


# ==============================================================================
#    TESTS
# ==============================================================================


NUM_OF_EXPECTED_ROUNDS = 5
EFK_LOG_PROPAGATION_DELAY = 10


def test_hare_scale(setup_bootstrap, setup_clients, wait_genesis):
    # Need to wait for 1 full iteration + the time it takes the logs to propagate to ES
    delay = int(testconfig['client']['args']['hare-round-duration-sec']) * NUM_OF_EXPECTED_ROUNDS + \
            EFK_LOG_PROPAGATION_DELAY + int(testconfig['client']['args']['hare-wakeup-delta'])
    print("Going to sleep for {0}".format(delay))
    time.sleep(delay)

    assert_all(current_index, testconfig['namespace'])
