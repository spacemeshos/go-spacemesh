import time

from pytest_testconfig import config as test_config

from tests import queries
from tests import config as conf
from tests.conftest import DeploymentInfo
from tests.deployment import create_deployment
from tests.hare.assert_hare import expect_hare
from tests.misc import CoreV1ApiClient
from tests.queries import query_atx_published
from tests.utils import get_conf, get_curr_ind, wait_genesis, get_genesis_time_delta


def new_client_in_namespace(name_space, setup_bootstrap, cspec, num):
    resp = create_deployment(conf.CLIENT_DEPLOYMENT_FILE, name_space,
                             deployment_id=setup_bootstrap.deployment_id,
                             replica_size=num,
                             container_specs=cspec,
                             time_out=test_config['deployment_ready_time_out'])
    client_info = DeploymentInfo(dep_id=setup_bootstrap.deployment_id)
    client_info.deployment_name = resp.metadata._name
    namespaced_pods = CoreV1ApiClient().list_namespaced_pod(namespace=name_space, include_uninitialized=True).items
    client_pods = list(filter(lambda i: i.metadata.name.startswith(client_info.deployment_name), namespaced_pods))

    client_info.pods = [{'name': c.metadata.name, 'pod_ip': c.status.pod_ip} for c in client_pods]
    print("Number of client pods: {0}".format(len(client_info.pods)))

    for c in client_info.pods:
        while True:
            resp = CoreV1ApiClient().read_namespaced_pod(name=c['name'], namespace=name_space)
            if resp.status.phase != 'Pending':
                break
            time.sleep(1)
    return client_info


# this is a path for 10m timeout limit in CI
# we reached the timeout because epochDuration happened to be greater than 10m
def sleep_and_print(total_seconds):
    print("Going to sleep total of %s seconds" % total_seconds)

    interval = 60  # seconds
    remaining = total_seconds
    while remaining > 0:
        if remaining != total_seconds:
            print("%s seconds remaining..." % remaining)
        time.sleep(min(interval, remaining))
        remaining -= interval

    if total_seconds <= interval:
        time.sleep(total_seconds)
        return

    print("Done")


# ==============================================================================
#    TESTS
# ==============================================================================

# add nodes continuously during the test (4 epochs) and validate hare consensus process,
# layer hashes (match between all nodes)
def test_add_delayed_nodes(init_session, add_elk, add_node_pool, add_curl, setup_bootstrap, start_poet, save_log_on_exit):
    current_index = get_curr_ind()
    bs_info = setup_bootstrap.pods[0]
    cspec = get_conf(bs_info, test_config['client'], test_config['genesis_delta'], setup_oracle=None,
                     setup_poet=setup_bootstrap.pods[0]['pod_ip'])
    ns = test_config['namespace']

    layer_duration = int(test_config['client']['args']['layer-duration-sec'])
    layers_per_epoch = int(test_config['client']['args']['layers-per-epoch'])
    epoch_duration = layer_duration * layers_per_epoch

    # start with 20 miners
    start_count = 20
    new_client_in_namespace(ns, setup_bootstrap, cspec, start_count)
    wait_genesis(get_genesis_time_delta(test_config["genesis_delta"]), test_config["genesis_delta"])
    sleep_and_print(epoch_duration)  # wait epoch duration

    # add 10 each epoch
    num_to_add = 10
    num_epochs_to_add_clients = 4
    clients = []
    for i in range(num_epochs_to_add_clients):
        clients.append(new_client_in_namespace(ns, setup_bootstrap, cspec, num_to_add))
        print("Added client batch ", i, clients[i].pods[i]['name'])
        sleep_and_print(epoch_duration)

    print("Done adding clients. Going to wait for two epochs")
    # wait two more epochs
    wait_epochs = 3
    sleep_and_print(wait_epochs * epoch_duration)

    # total = bootstrap + first clients + added clients
    total = 1 + start_count + num_epochs_to_add_clients * num_to_add
    total_epochs = 1 + num_epochs_to_add_clients + wait_epochs  # add 1 for first epoch
    total_layers = layers_per_epoch * total_epochs
    first_layer_of_last_epoch = total_layers - layers_per_epoch
    f = int(test_config['client']['args']['hare-max-adversaries'])

    # validate
    print("Waiting 2 minutes for logs to propagate")
    sleep_and_print(120)

    print("Running validation")
    expect_hare(current_index, ns, first_layer_of_last_epoch, total_layers - 1, total, f)  # validate hare
    atx_last_epoch = list()
    for layer in range(first_layer_of_last_epoch, first_layer_of_last_epoch+layers_per_epoch-1):
        atx_last_epoch += query_atx_published(current_index, ns, layer)

    queries.assert_equal_layer_hashes(current_index, ns)
    assert len(atx_last_epoch) == total  # validate num of atxs in last epoch
