import time

import pytest
from pytest_testconfig import config as testconfig

from tests.fixtures import DeploymentInfo
from tests.hare.assert_hare import assert_all
from tests.test_bs import current_index, setup_bootstrap_in_namespace, setup_clients_in_namespace


# ==============================================================================
#    Fixtures
# ==============================================================================
@pytest.fixture(scope='module')
def setup_bootstrap_for_hare(request, init_session, setup_oracle, create_configmap):
    bootstrap_deployment_info = DeploymentInfo(dep_id=init_session)
    return setup_bootstrap_in_namespace(testconfig['namespace'],
                                        bootstrap_deployment_info,
                                        testconfig['bootstrap'],
                                        oracle=setup_oracle,
                                        dep_time_out=testconfig['deployment_ready_time_out'])


@pytest.fixture(scope='module')
def setup_clients_for_hare(request, init_session, setup_oracle, setup_bootstrap_for_hare):
    client_info = DeploymentInfo(dep_id=setup_bootstrap_for_hare.deployment_id)
    return setup_clients_in_namespace(testconfig['namespace'],
                                      setup_bootstrap_for_hare.pods[0],
                                      client_info,
                                      testconfig['client'],
                                      oracle=setup_oracle,
                                      dep_time_out=testconfig['deployment_ready_time_out'])


# ==============================================================================
#    TESTS
# ==============================================================================


NUM_OF_EXPECTED_ROUNDS = 5
EFK_LOG_PROPAGATION_DELAY = 10


def test_hare_sanity(init_session, setup_bootstrap_for_hare, setup_clients_for_hare, wait_genesis):
    # Need to wait for 1 full iteration + the time it takes the logs to propagate to ES
    round_duration = int(testconfig['client']['args']['hare-round-duration-sec'])
    layers_count = int(testconfig['client']['args']['cmd-hare-layers-count'])
    delay = round_duration * NUM_OF_EXPECTED_ROUNDS * layers_count + \
            EFK_LOG_PROPAGATION_DELAY + int(testconfig['client']['args']['hare-wakeup-delta'])
    print("Going to sleep for {0}".format(delay))
    time.sleep(delay)

    assert_all(current_index, testconfig['namespace'])
