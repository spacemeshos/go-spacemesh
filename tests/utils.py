import os
import re
import time

from tests.queries import get_release_tick_msgs


LOG_ENTRIES = {"message": "M"}


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


def print_hits_entry_count(hits, log_entry):
    """
    prints number of seen values

    :param hits: list, all query results
    :param log_entry: string, what part of the log do we want to sum
    """

    if log_entry not in LOG_ENTRIES.keys():
        raise ValueError(f"unknown log entry {log_entry}")

    result = {}
    entry = LOG_ENTRIES[log_entry]

    for hit in hits:
        entry_val = getattr(hit, entry)
        if entry_val not in result:
            result[entry_val] = 1
            continue

        result[entry_val] += 1

    for key in result:
        print(f"found {result[key]} appearances of '{key}' in hits")


def wait_for_next_layer(namespace, cl_num, timeout):
    tts = 15
    old_release_ticks = get_release_tick_msgs(namespace, namespace)
    # if we started sampling while a new layer just started we will enter this while loop
    while len(old_release_ticks) % cl_num != 0 and timeout > 0:
        time.sleep(tts)
        old_release_ticks = get_release_tick_msgs(namespace, namespace)
        if len(old_release_ticks) % cl_num == 0:
            return

        timeout -= tts

    time.sleep(tts)
    new_release_ticks = get_release_tick_msgs(namespace, namespace)

    while len(old_release_ticks) + cl_num > len(new_release_ticks) and timeout > 0:
        time.sleep(tts)
        new_release_ticks = get_release_tick_msgs(namespace, namespace)
        timeout -= tts

    return


# TODO there might be a better place for a validation func than utils
def validate_blocks_per_nodes(block_map, last_layer, layers_per_epoch, layer_avg_size, num_miners):
    """

    :param block_map: dictionary, map between nodes to blocks per layer
    :param last_layer:
    :param layers_per_epoch:
    :param layer_avg_size:
    :param num_miners:
    :return:
    """
    for node in block_map:
        node_lays = block_map[node]["layers"]
        blocks_in_relevant_layers = sum([len(node_lays[str(x)]) for x in range(layers_per_epoch, last_layer)
                                         if str(x) in node_lays])
        # need to deduct blocks created in first genesis epoch since it does not follow general mining rules by design
        blocks_created_per_layer = blocks_in_relevant_layers / (last_layer - layers_per_epoch)
        wanted_avg_block_per_node = max(1, int(layer_avg_size / num_miners))
        ass_err = f"node {node} failed creating the avg block size"
        assert blocks_created_per_layer / wanted_avg_block_per_node == 1, ass_err


# TODO #@! rename this func
def validate_blocks_per_nodes_2(block_map, from_layer, to_layer, layers_per_epoch, layer_avg_size, num_miners):
    # layers count start from 0
    # epoch first layer and last % layers_per_epoch will be layers_per_epoch - 1
    layer_mod = layers_per_epoch - 1

    if from_layer == 0:
        print(f"refactoring starting layer from 0 to {layer_mod}, not validating first layer")
        from_layer = layer_mod

    if from_layer >= to_layer:
        print(f"starting layer ({from_layer}) must be bigger than ending layer ({to_layer})")
        return False

    # layers start from 0
    if from_layer % layers_per_epoch != layer_mod or to_layer % layers_per_epoch != layer_mod:
        print(f"layer to start from and layer to end at must be at the beginning and ending of an epoch respectively")
        print(f"from layer={from_layer}, to layer={to_layer}")
        return False

    for node in block_map:
        node_lays = block_map[node]["layers"]
        blocks_sum = sum([len(node_lays[str(x)]) for x in range(from_layer, to_layer) if str(x) in node_lays])
        # TODO #@! talk to Barak about these two steps
        blocks_per_layer = blocks_sum / (to_layer - from_layer)
        wanted_avg_block_per_node = max(1, int(layer_avg_size / num_miners))
        ass_err = f"node {node} failed creating the avg block size"
        assert blocks_per_layer / wanted_avg_block_per_node == 1, ass_err
