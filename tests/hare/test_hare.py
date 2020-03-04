from datetime import datetime, timedelta
import time
import pytest
import pytz
from pytest_testconfig import config as testconfig

from tests import deployment
from tests.conftest import DeploymentInfo
from tests.hare.assert_hare import expect_hare, assert_all, get_max_mem_usage
from tests.misc import CoreV1ApiClient
from tests.setup_utils import setup_bootstrap_in_namespace, setup_clients_in_namespace
from tests.utils import get_curr_ind, wait_genesis

ORACLE_DEPLOYMENT_FILE = './k8s/oracle.yml'
GENESIS_TIME = pytz.utc.localize(datetime.utcnow() + timedelta(seconds=testconfig['genesis_delta']))


def setup_server(deployment_name, deployment_file, namespace):
    deployment_name_prefix = deployment_name.split('-')[1]
    namespaced_pods = CoreV1ApiClient().list_namespaced_pod(namespace,
                                                            label_selector=(
                                                                "name={0}".format(deployment_name_prefix))).items
    if namespaced_pods:
        # if server already exist -> delete it
        deployment.delete_deployment(deployment_name, namespace)

    resp = deployment.create_deployment(deployment_file, namespace, time_out=testconfig['deployment_ready_time_out'])
    namespaced_pods = CoreV1ApiClient().list_namespaced_pod(namespace,
                                                            label_selector=(
                                                                "name={0}".format(deployment_name_prefix))).items
    if not namespaced_pods:
        raise Exception('Could not setup Server: {0}'.format(deployment_name))

    ip = namespaced_pods[0].status.pod_ip
    if ip is None:
        print("{0} IP was None, trying again..".format(deployment_name_prefix))
        time.sleep(3)
        # retry
        namespaced_pods = CoreV1ApiClient().list_namespaced_pod(namespace,
                                                                label_selector=(
                                                                    "name={0}".format(deployment_name_prefix))).items
        ip = namespaced_pods[0].status.pod_ip
        if ip is None:
            raise Exception("Failed to retrieve {0} ip address".format(deployment_name_prefix))

    return ip

# ==============================================================================
#    Fixtures
# ==============================================================================
@pytest.fixture(scope='module')
def setup_oracle(request):
    oracle_deployment_name = 'sm-oracle'
    return setup_server(oracle_deployment_name, ORACLE_DEPLOYMENT_FILE, testconfig['namespace'])


@pytest.fixture(scope='module')
def setup_bootstrap_for_hare(request, init_session, setup_oracle):
    bootstrap_deployment_info = DeploymentInfo(dep_id=init_session)
    return setup_bootstrap_in_namespace(testconfig['namespace'],
                                        bootstrap_deployment_info,
                                        testconfig['bootstrap'],
                                        testconfig['genesis_delta'],
                                        oracle=setup_oracle,
                                        dep_time_out=testconfig['deployment_ready_time_out'])


@pytest.fixture(scope='module')
def setup_clients_for_hare(request, init_session, setup_oracle, setup_bootstrap_for_hare):
    client_info = DeploymentInfo(dep_id=setup_bootstrap_for_hare.deployment_id)
    return setup_clients_in_namespace(testconfig['namespace'],
                                      setup_bootstrap_for_hare.pods[0],
                                      client_info,
                                      testconfig['client'],
                                      testconfig['genesis_delta'],
                                      oracle=setup_oracle,
                                      dep_time_out=testconfig['deployment_ready_time_out'])


# ==============================================================================
#    TESTS
# ==============================================================================


EFK_LOG_PROPAGATION_DELAY = 20


# simple sanity test run for one layer
def test_hare_sanity(init_session, setup_bootstrap_for_hare, setup_clients_for_hare):
    # NOTICE the following line should be present in the first test of the suite
    wait_genesis(GENESIS_TIME, testconfig['genesis_delta'])
    current_index = get_curr_ind()
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
def test_hare_scale(init_session, setup_bootstrap_for_hare, setup_clients_for_hare):
    current_index = get_curr_ind()
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
    f = int(testconfig['client']['args']['hare-max-adversaries'])
    expect_hare(current_index, ns, 1, layers_count, total, f)
    max_mem = get_max_mem_usage(current_index, ns)
    print('Mem usage is {0} expected max is {1}'.format(max_mem, EXPECTED_MAX_MEM))
    assert max_mem < EXPECTED_MAX_MEM
