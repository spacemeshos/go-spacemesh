import pytest
from tests.test_bs import setup_network, setup_bootstrap, setup_clients, add_curl, wait_genesis
from tests.fixtures import init_session, load_config, set_namespace, session_id, set_docker_images


@pytest.mark.parametrize('set_namespace', ['doNotDeleteNameSpace'], indirect=True)
def test_mesh(setup_network):
    pass
