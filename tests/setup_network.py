from datetime import datetime, timedelta
import pytest
from pytest_testconfig import config as testconfig
import pytz

from tests.conftest import NetworkDeploymentInfo, NetworkInfo
from tests.utils import wait_genesis


GENESIS_TIME = pytz.utc.localize(datetime.utcnow() + timedelta(seconds=testconfig['genesis_delta']))


@pytest.fixture(scope='module')
def setup_network(init_session, add_curl, setup_bootstrap, start_poet, setup_clients):
    # This fixture deploy a complete Spacemesh network and returns only after genesis time is over
    _session_id = init_session
    network_deployment = NetworkDeploymentInfo(dep_id=_session_id,
                                               bs_deployment_info=setup_bootstrap,
                                               cl_deployment_info=setup_clients)

    wait_genesis(GENESIS_TIME, testconfig['genesis_delta'])
    return network_deployment


@pytest.fixture(scope='module')
def setup_mul_network(init_session, add_curl, setup_bootstrap, start_poet, setup_mul_clients):
    # This fixture deploy a complete Spacemesh network and returns only after genesis time is over
    _session_id = init_session
    network_deployment = NetworkInfo(namespace=init_session,
                                     bs_deployment_info=setup_bootstrap,
                                     cl_deployment_info=setup_mul_clients)

    wait_genesis(GENESIS_TIME, testconfig['genesis_delta'])
    return network_deployment
