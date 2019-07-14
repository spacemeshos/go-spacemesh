import time

import pytest
from pytest_testconfig import config as testconfig

from tests.fixtures import DeploymentInfo
from tests.fixtures import init_session, load_config, set_namespace, session_id, set_docker_images
from tests.hare.assert_hare import expect_hare, assert_all, get_max_mem_usage
from tests.test_bs import current_index, setup_bootstrap_in_namespace, setup_clients_in_namespace, setup_server, create_configmap, wait_genesis

ORACLE_DEPLOYMENT_FILE = './k8s/oracle.yml'


# ==============================================================================
#    Fixtures
# ==============================================================================
@pytest.fixture(scope='module')
def setup_oracle(request):
    oracle_deployment_name = 'oracle'
    return setup_server(oracle_deployment_name, ORACLE_DEPLOYMENT_FILE, testconfig['namespace'])


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


EFK_LOG_PROPAGATION_DELAY = 20


# simple sanity test run for one layer
def test_hare_sanity(init_session, setup_bootstrap_for_hare, setup_clients_for_hare, wait_genesis):
    # Need to wait for 1 full iteration + the time it takes the logs to propagate to ES
    round_duration = int(testconfig['client']['args']['hare-round-duration-sec'])
    wakeup_delta = int(testconfig['client']['args']['hare-wakeup-delta'])
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_count = int(1)
    print("Number of layers is ", layers_count)
    delay = layer_duration * layers_count + EFK_LOG_PROPAGATION_DELAY + wakeup_delta * (layers_count - 1) + round_duration
    print("Going to sleep for {0}".format(delay))
    time.sleep(delay)

    assert_all(current_index, testconfig['namespace'])


EXPECTED_MAX_MEM = 300*1024*1024  # MB
# scale test run for 3 layers
def test_hare_scale(init_session, setup_bootstrap_for_hare, setup_clients_for_hare, wait_genesis):
    total = int(testconfig['client']['replicas']) + int(testconfig['bootstrap']['replicas'])

    # Need to wait for 1 full iteration + the time it takes the logs to propagate to ES
    round_duration = int(testconfig['client']['args']['hare-round-duration-sec'])
    wakeup_delta = int(testconfig['client']['args']['hare-wakeup-delta'])
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_count = int(10)
    print("Number of layers is ", layers_count)
    delay = layer_duration * layers_count + EFK_LOG_PROPAGATION_DELAY + wakeup_delta * (layers_count - 1) + round_duration
    print("Going to sleep for {0}".format(delay))
    time.sleep(delay)

    ns = testconfig['namespace']
    expect_hare(current_index, ns, 1, layers_count, total)
    max_mem = get_max_mem_usage(current_index, ns)
    print('Mem usage is {0} expected max is {1}'.format(max_mem, EXPECTED_MAX_MEM))
    assert max_mem < EXPECTED_MAX_MEM
