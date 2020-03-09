import pytest
from tests.setup_network import setup_network
from tests.conftest import init_session, load_config, set_namespace, session_id, set_docker_images


@pytest.mark.parametrize('set_namespace', ['doNotDeleteNameSpace'], indirect=True)
def test_mesh(setup_network):
    pass
