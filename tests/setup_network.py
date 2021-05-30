from datetime import datetime
import pytest
from pytest_testconfig import config as testconfig
import pytz
from tests.k8s_handler import get_external_ip
from tests.config import PROFILER_SERVER_PORT
from tests.conftest import NetworkDeploymentInfo, NetworkInfo
from tests.utils import wait_genesis, get_genesis_time_delta


@pytest.fixture(scope='module')
def setup_network(init_session, add_elk, add_pyroscope, add_node_pool, add_curl, setup_bootstrap, start_poet, setup_clients):
    # This fixture deploy a complete Spacemesh network and returns only after genesis time is over
    _session_id = init_session
    network_deployment = NetworkDeploymentInfo(dep_id=_session_id,
                                               bs_deployment_info=setup_bootstrap,
                                               cl_deployment_info=setup_clients)

    pyroscope = get_external_ip(testconfig['namespace'], "pyroscope")
    print("Pyroscope server URL : http://{0}:{1} ".format(pyroscope, PROFILER_SERVER_PORT))
# genesis time = when clients have been created + delta = time.now - time it took pods to come up + delta
    wait_genesis(get_genesis_time_delta(testconfig['genesis_delta']), testconfig['genesis_delta'])
    return network_deployment


@pytest.fixture(scope='module')
def setup_mul_network(init_session, add_elk, add_pyroscope, add_node_pool, add_curl, setup_bootstrap, start_poet, setup_mul_clients):
    # This fixture deploy a complete Spacemesh network and returns only after genesis time is over
    _session_id = init_session
    network_deployment = NetworkInfo(namespace=init_session,
                                     bs_deployment_info=setup_bootstrap,
                                     cl_deployment_info=setup_mul_clients)
    wait_genesis(get_genesis_time_delta(testconfig['genesis_delta']), testconfig['genesis_delta'])
    return network_deployment
