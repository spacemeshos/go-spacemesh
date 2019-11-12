from pytest_testconfig import config as testconfig
import time

from tests.conftest import DeploymentInfo
from tests import pod
from tests.test_bs import setup_bootstrap_in_namespace, add_curl, setup_bootstrap
from tests.misc import CoreV1ApiClient


# reboot bootstrap
# add a new stateful bootstrap
# kill bootstrap
# create a new bootstrap with the former key
# validate state is the same as the old bootstrap node
def test_reboot_bootstrap(init_session):
    print("test_reboot_bootstrap")
    # test params
    sleep_time = 5
    session_id = init_session
    bootstrap_key = 'bootstrap'
    bootstrap_group_id = 'bootstrap_key'
    ss_file_path = "/Users/amit/workspace/go-spacemesh/tests/k8s/bootstrap-w-conf-ss.yml"
    key_regex = r"Local node identity >> (?P<{bootstrap_group_id}>\w+)".format(bootstrap_group_id=bootstrap_group_id)
    # using the same logic as setup_bootstrap fixture but with an additional
    # file_path argument to load a none default yaml spec file
    bootstrap_deployment_info = DeploymentInfo(dep_id=session_id)

    bootstrap_deployment_info = setup_bootstrap_in_namespace(testconfig['namespace'],
                                                             bootstrap_deployment_info,
                                                             testconfig['bootstrap'],
                                                             dep_time_out=testconfig['deployment_ready_time_out'],
                                                             file_path=ss_file_path)

    print(f"sleeping for {sleep_time} seconds before bootstrap deletion node")
    time.sleep(sleep_time)
    # DELETE bootstrap node
    pod.delete_pod(bootstrap_deployment_info.deployment_name, session_id)
    print(f"sleeping for {sleep_time} seconds after bootstrap deletion node")
    time.sleep(sleep_time)

    pods = CoreV1ApiClient().list_namespaced_pod(session_id,
                                                 label_selector=(
                                                     "name={0}".format(bootstrap_key))).items
    pod_name = pods[0].spec.hostname
    new_key = pod.search_phrase_in_pod_log(pod_name, session_id, bootstrap_key, key_regex, group=bootstrap_group_id)
    print(f"new key is: {new_key}")
    original_bs_key = bootstrap_deployment_info.pods[0]["key"]
    print(f"old key is: {original_bs_key}")
    assert original_bs_key == new_key


# Kill original bootstraps
# Deploy X peers with X bootstrap node
# Wait for (network) bootstrap
# Kill original bootstrap nodes
# Reboot X clients (using saved routing table)
# wait for and validate bootstrap in X rebooted clients
def test_kill_bs():
    pass
