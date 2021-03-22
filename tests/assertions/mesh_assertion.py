from tests.api.api_handler import ApiHandler


def layer_assertion_setup(layers_per_epoch, last_layer, start_layer=None):
    """ last layer is included """
    minimal_valid_layer = layers_per_epoch * 2 - 1
    if start_layer is not None and start_layer < minimal_valid_layer:
        print(f"minimal valid layer for assertion is {minimal_valid_layer}, requested starting layer {start_layer}")
    # set starting layer to be at least minimal valid layer for querying
    start_layer = start_layer if start_layer and start_layer > minimal_valid_layer else minimal_valid_layer
    if start_layer > last_layer:
        raise ValueError(f"starting layer: {start_layer} is greater than last layer: {last_layer}")
    return start_layer


def assert_layer_hash(api_handler: ApiHandler, last_layer, start_layer=None):
    """ last layer is included """
    # TODO: have api_handler to retrieve layers_per_epoch
    start_layer = layer_assertion_setup(api_handler.sender.layers_per_epoch, last_layer, start_layer)
    print(f"asserting layer hash is equal for all layers between {start_layer} and {last_layer}")
    for layer_num in range(start_layer, last_layer+1):
        print("asserting layer", layer_num)
        ass_err = f"found unequal layer hashes on layer {layer_num}"
        assert api_handler.is_layer_hash_equal(layer_num), ass_err
        print(f"layer {layer_num} is validated successfully")


def assert_equal_state_roots(api_handler: ApiHandler, last_layer, start_layer=None):
    """ last layer is included """
    # TODO: have api_handler to retrieve layers_per_epoch
    start_layer = layer_assertion_setup(api_handler.sender.layers_per_epoch, last_layer, start_layer)
    print(f"asserting state root hash is equal for all layers between {start_layer} and {last_layer}")
    # add 1 to last layer to include the last layer
    for layer_num in range(start_layer, last_layer+1):
        print("asserting layer", layer_num)
        ass_err = f"found unequal state root hashes on layer {layer_num}"
        assert api_handler.is_state_root_hash_equal(layer_num), ass_err
        print(f"layer {layer_num} is validated successfully")


# TODO there might be a better place for a validation func than utils
def assert_blocks_num_in_epochs(block_map, from_layer, to_layer, layers_per_epoch, layer_avg_size, num_miners,
                                ignore_lst=None):
    """
    validate average block creation per node epoch wise,
    from_layer and to_layer must be a multiplication of layers_per_epoch.
    First epoch will be ignored

    :param block_map: dictionary, map between nodes to blocks each created
    :param from_layer: int, starting layer to check from
    :param to_layer: int, end layer to check up to (to_layer is not included)
    :param layers_per_epoch: int, number of layers per epoch
    :param layer_avg_size: int, average number of blocks per layer
    (layer_avg_size * layers_per_epoch will give us the number of blocks that should be created per epoch)
    :param num_miners: int, number of miners
    :param ignore_lst: list, a list of pod names to be ignored

    """
    # miners start creating blocks only from epoch 2
    min_layer_number = layers_per_epoch * 2
    if from_layer < min_layer_number:
        print(f"refactoring starting layer from 0 to {layers_per_epoch}, not validating first epoch")
        from_layer = min_layer_number
    if from_layer == to_layer:
        raise ValueError("no valid range was given, first layer equals the last, layers value:", from_layer)
    if from_layer > to_layer:
        raise ValueError(f"starting layer ({from_layer}) must be bigger than ending layer ({to_layer})")
    if from_layer % layers_per_epoch != 0 or to_layer % layers_per_epoch != 0:
        raise ValueError(f"start layer and end layer must be at the beginning and ending of an epoch respectively, "
                         f"start={from_layer}, end={to_layer}")

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
        ass_err += f"\nblocks created per layer {blocks_per_layer}, wanted average block per node {wanted_res}, " \
                   f"layer_avg_size {layer_avg_size}, layers_per_epoch {layers_per_epoch}, num_miners {num_miners}, " \
                   f"blocks_sum {blocks_sum}, to_layer {to_layer}, from_layer {from_layer}, blocks {node_lays}"
        assert blocks_per_layer == wanted_res, ass_err

        print(f"successfully validated {node}",
              f"\nblocks created per layer {blocks_per_layer}, wanted average block per node {wanted_res}, ",
              f"layer_avg_size {layer_avg_size}, layers_per_epoch {layers_per_epoch}, num_miners {num_miners}, ",
              f"blocks_sum {blocks_sum}, to_layer {to_layer}, from_layer {from_layer}")

    print("\nvalidation succeeded!\n")
