import pytest
from pytest_testconfig import config as testconfig

from tests.api_handler import ApiHandler
from tests.conftest import NetworkDeploymentInfo, NetworkInfo
from tests.conftest import DeploymentInfo as DepInfo
from tests.utils import wait_genesis, get_genesis_time_delta


@pytest.fixture(scope='module')
def setup_network(init_session, add_elk, add_node_pool, add_curl, setup_bootstrap, start_poet, setup_clients):
    # This fixture deploy a complete Spacemesh network and returns only after genesis time is over
    _session_id = init_session
    network_deployment = NetworkDeploymentInfo(dep_id=_session_id,
                                               bs_deployment_info=setup_bootstrap,
                                               cl_deployment_info=setup_clients)
    # TODO: not adding bootstrap info here since bootstrap port is different than the miners and that causes troubles
    api_handler = ApiHandler(DepInfo.get_all_ips([setup_clients]), init_session)
    # genesis time = when clients have been created + delta = time.now - time it took pods to come up + delta
    wait_genesis(get_genesis_time_delta(testconfig['genesis_delta']), testconfig['genesis_delta'])
    return network_deployment, api_handler


@pytest.fixture(scope='module')
def setup_mul_network(init_session, add_elk, add_node_pool, add_curl, setup_bootstrap, start_poet, setup_mul_clients):
    # This fixture deploy a complete Spacemesh network and returns only after genesis time is over
    _session_id = init_session
    network_deployment = NetworkInfo(namespace=init_session,
                                     bs_deployment_info=setup_bootstrap,
                                     cl_deployment_info=setup_mul_clients)
    api_handler = ApiHandler(DepInfo.get_all_ips(setup_mul_clients.append(setup_bootstrap)), init_session)
    wait_genesis(get_genesis_time_delta(testconfig['genesis_delta']), testconfig['genesis_delta'])
    return network_deployment, api_handler
