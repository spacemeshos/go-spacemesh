from datetime import datetime, timedelta
from kubernetes.stream import stream
import functools
import ntpath
import os
import pytz
import re
from shutil import copyfile
import subprocess
import time

import tests_elk.config as conf
import tests_elk.deployment as deployment
from tests_elk.misc import ContainerSpec, CoreV1ApiClient
from tests_elk.statefulset import create_statefulset
import tests_elk.queries as q


def api_call(client_ip, data, api, namespace, port="9093"):
    # todo: this won't work with long payloads - ( `Argument list too long` ). try port-forward ?
    res = stream(CoreV1ApiClient().connect_post_namespaced_pod_exec, name="curl", namespace=namespace,
                 command=["curl", "-s", "--request", "POST", "--data", data, f"http://{client_ip}:{port}/{api}"],
                 stderr=True, stdin=False, stdout=True, tty=False, _request_timeout=90)
    return res


def get_curr_ind():
    dt = datetime.now()
    today_date = dt.strftime("%Y.%m.%d")
    return 'kubernetes_cluster-' + today_date


def get_spec_file_path(file_name):
    """
    resolve yml file path according to current working directory
    default file path for yml files is ./tests/k8s/

    :param file_name: string, file name

    :return: string, full path to file
    """
    main_tests_directory = "tests"
    k8s_dir = "k8s"

    curr_dir = os.path.dirname(os.path.realpath(__file__))
    tests_dir_end_ind = re.search(main_tests_directory, curr_dir).end()
    if not tests_dir_end_ind:
        raise Exception("must be ran from 'tests' dir or any sub directory to tests for yml path resolution")

    curr_dir = curr_dir[:tests_dir_end_ind]
    full_path = os.path.join(curr_dir, k8s_dir, file_name)
    print(f"get_spec_file_path return val: {full_path}")
    return full_path


def wait_for_next_layer(namespace, cl_num, timeout):
    tts = 15
    old_release_ticks = q.get_release_tick_msgs(namespace, namespace)
    # if we started sampling while a new layer just started we will enter this while loop
    while len(old_release_ticks) % cl_num != 0 and timeout > 0:
        time.sleep(tts)
        old_release_ticks = q.get_release_tick_msgs(namespace, namespace)
        if len(old_release_ticks) % cl_num == 0:
            return

        timeout -= tts

    time.sleep(tts)
    new_release_ticks = q.get_release_tick_msgs(namespace, namespace)

    while len(old_release_ticks) + cl_num > len(new_release_ticks) and timeout > 0:
        time.sleep(tts)
        new_release_ticks = q.get_release_tick_msgs(namespace, namespace)
        timeout -= tts

    return


# TODO there might be a better place for a validation func than utils
def validate_blocks_per_nodes(block_map, from_layer, to_layer, layers_per_epoch, layer_avg_size, num_miners,
                              ignore_lst=None):
    """
    validate average block creation per node epoch wise,
    from_layer and to_layer must be a multiplication of layers_per_epoch.
    First epoch will be ignored

    :param block_map: dictionary, map between nodes to blocks each created
    :param from_layer: int, starting layer to check from
    :param to_layer: int, end layer to check up to
    :param layers_per_epoch: int, number of layers per epoch
    :param layer_avg_size: int, average number of blocks per layer
    (layer_avg_size * layers_per_epoch will give us the number of blocks that should be created per epoch)
    :param num_miners: int, number of miners
    :param ignore_lst: list, a list of pod names to be ignored

    """
    # layers count start from 0
    if from_layer == 0:
        print(f"refactoring starting layer from 0 to {layers_per_epoch}, not validating first epoch")
        from_layer = layers_per_epoch * 2
        if from_layer == to_layer:
            return

    assert from_layer <= to_layer, f"starting layer ({from_layer}) must be bigger than ending layer ({to_layer})"

    if from_layer % layers_per_epoch != 0 or to_layer % layers_per_epoch != 0:
        print(f"layer to start from and layer to end at must be at the beginning and ending of an epoch respectively")
        print(f"from layer={from_layer}, to layer={to_layer}")
        assert 0

    print("validating node")
    for node in block_map:
        if ignore_lst and node in ignore_lst:
            print(f"SKIPPING NODE {node}, ", end="")
            continue

        print(f"{node}, ", end="")
        node_lays = block_map[node].layers
        blocks_sum = sum([len(node_lays[x]) for x in range(from_layer, to_layer)])
        blocks_per_layer = blocks_sum / (to_layer - from_layer)
        wanted_res = int((layer_avg_size * layers_per_epoch) / num_miners) / layers_per_epoch
        ass_err = f"node {node} failed creating the avg block size"
        ass_err += f"\nblocks created per layer {blocks_per_layer}, wanted average block per node {wanted_res}"
        assert blocks_per_layer == wanted_res, ass_err

    print("\nvalidation succeeded!\n")


def get_pod_id(ns, pod_name):
    hits = q.get_all_msg_containing(ns, pod_name, "Starting HARE_PROTOCOL")
    if not hits:
        return None

    res = hits[0]
    return res["node_id"]


# ====================== tests_bs RIP ======================

def node_string(key, ip, port, discport):
    return "spacemesh://{0}@{1}:{2}?disc={3}".format(key, ip, port, discport)


@functools.lru_cache(maxsize=1)
def get_genesis_time_delta(genesis_time):
    return pytz.utc.localize(datetime.utcnow() + timedelta(seconds=genesis_time))


def get_conf(bs_info, client_config, genesis_time, setup_oracle=None, setup_poet=None, args=None):
    """
    get_conf gather specification information into one ContainerSpec object

    :param bs_info: DeploymentInfo, bootstrap info
    :param client_config: DeploymentInfo, client info
    :param genesis_time: string, genesis time as set in suite specification file
    :param setup_oracle: string, oracle ip
    :param setup_poet: string, poet ip
    :param args: list of strings, arguments for appendage in specification
    :return: ContainerSpec
    """
    genesis_time_delta = get_genesis_time_delta(genesis_time)
    client_args = {} if 'args' not in client_config else client_config['args']
    # append client arguments
    if args is not None:
        for arg in args:
            client_args[arg] = args[arg]

    # create a new container spec with client configuration
    cspec = ContainerSpec(cname='client', specs=client_config)

    # append oracle configuration
    if setup_oracle:
        client_args['oracle_server'] = 'http://{0}:{1}'.format(setup_oracle, conf.ORACLE_SERVER_PORT)

    # append poet configuration
    if setup_poet:
        client_args['poet_server'] = '{0}:{1}'.format(setup_poet, conf.POET_SERVER_PORT)

    bootnodes = node_string(bs_info['key'], bs_info['pod_ip'], conf.BOOTSTRAP_PORT, conf.BOOTSTRAP_PORT)
    cspec.append_args(bootnodes=bootnodes, genesis_time=genesis_time_delta.isoformat('T', 'seconds'))
    # append client config to ContainerSpec
    if len(client_args) > 0:
        cspec.append_args(**client_args)
    return cspec


def choose_k8s_object_create(config, deployment_file, statefulset_file):
    dep_type = 'deployment' if 'deployment_type' not in config else config['deployment_type']
    if dep_type == 'deployment':
        return deployment_file, deployment.create_deployment
    elif dep_type == 'statefulset':
        # StatefulSets are intended to be used with stateful applications and distributed systems.
        # Pods in a StatefulSet have a unique ordinal index and a stable network identity.
        return statefulset_file, create_statefulset
    else:
        raise Exception("Unknown deployment type in configuration. Please check your config.yaml")


def wait_genesis(genesis_time, genesis_delta):
    # Make sure genesis time has not passed yet and sleep for the rest
    time_now = pytz.utc.localize(datetime.utcnow())
    delta_from_genesis = (genesis_time - time_now).total_seconds()
    if delta_from_genesis < 0:
        raise Exception("genesis_delta time={0}sec, is too short for this deployment. "
                        "delta_from_genesis={1}".format(genesis_delta, delta_from_genesis))
    else:
        print('sleep for {0} sec until genesis time'.format(delta_from_genesis))
        time.sleep(delta_from_genesis)


def exec_wait(cmd, retry=1, interval=1):
    print(f"\nrunning: {cmd}")
    ret_code = 0
    try:
        process = subprocess.Popen(cmd, shell=True, stdout=subprocess.PIPE)
        while True:
            line = process.stdout.readline()
            if not line and process.poll() is not None:
                break
            print(line.strip())
            time.sleep(0.5)
        ret_code = process.poll()
    except Exception as e:
        raise Exception(f"failed running: \"{cmd}\",\nreturn code: {ret_code},\nexception: {e}")

    # if return code != 0 and retry !=0 run the same command again
    if ret_code and ret_code != "0" and retry:
        print(f"failed with return code: {ret_code}, retrying (retries left: {retry})")
        time.sleep(interval)
        exec_wait(cmd, retry-1)
    elif ret_code is not None:
        print(f"return code: {ret_code}")


def duplicate_file_and_replace_phrase(path, filename, new_name, look, replace):
    if is_pattern_in_file(os.path.join(path, filename), look):
        backup_path = create_backup_file(path, filename, new_name)
        replace_phrase_in_file(backup_path, look, replace)
        return backup_path, True
    else:
        return os.path.join(path, filename), False


def is_pattern_in_file(path, look):
    # search phrase in file return True if found else return False
    with open(path) as f:
        text = f.read()
        match = re.search(look, text)
        if match:
            return True
    return False


def create_backup_file(path, filename, new_name):
    src_path = os.path.join(path, filename)
    backup_path = os.path.join(path, new_name)
    try:
        print(f"copying {src_path} to {backup_path}")
        copyfile(src_path, backup_path)
    except Exception as e:
        print(f"failed backing up file: {src_path}, err: {e}")
        return None

    return backup_path


def replace_phrase_in_file(filepath, look, replace):
    with open(filepath, 'r+') as f:
        text = f.read()
        text = re.sub(look, replace, text)
        f.seek(0)
        f.write(text)
        f.truncate()


def delete_file(filepath):
    try:
        while os.path.isfile(filepath):
            print(f"deleting file: {filepath}")
            os.remove(filepath)
            time.sleep(0.2)
    except OSError as e:
        print(f"failed removing file: {filepath}, err: {e}")


def get_filename_and_path(path):
    head, tail = ntpath.split(path)
    if not tail:
        head, tail = ntpath.split(head)

    return head, tail
