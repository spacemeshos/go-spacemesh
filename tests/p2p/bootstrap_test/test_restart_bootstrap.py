# from tests.conftest import init_session
from tests.test_bs import setup_bootstrap_in_namespace


# Kill original bootstraps
# Deploy X peers with X bootstrap node
# Wait for (network) bootstrap
# Kill original bootstrap nodes
# Reboot X clients (using saved routing table)
# wait for and validate bootstrap in X rebooted clients
def test_reboot_bootstrap(init_session):
    pass
