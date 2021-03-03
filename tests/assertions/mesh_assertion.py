from tests.api_handler import ApiHandler


def assert_layer_hash(api_handler: ApiHandler, last_layer, start_layer=None):
    """ last layer is included """
    # TODO: have api_handler to retrieve layers_per_epoch
    minimal_valid_layer = api_handler.layers_per_epoch * 2 - 1
    if start_layer is not None and start_layer < minimal_valid_layer:
        print(f"minimal valid layer for assertion is {minimal_valid_layer}, requested starting layer {start_layer}")
    # set starting layer to be at least minimal valid layer for querying
    start_layer = start_layer if start_layer and start_layer > minimal_valid_layer else minimal_valid_layer
    print(f"asserting layer hash is equal for all layers between {start_layer} and {last_layer}")
    # validate starting layer is smaller than last layer
    if start_layer > last_layer:
        raise ValueError(f"starting layer: {start_layer} is greater than last layer: {last_layer}")
    # add 1 to last layer to include the last layer
    for layer_num in range(start_layer, last_layer+1):
        print("asserting layer", layer_num)
        ass_err = f"found unequal layer hashes on layer {layer_num}"
        assert api_handler.is_layer_hash_equal(layer_num), ass_err
        print(f"layer {layer_num} is validated successfully")
