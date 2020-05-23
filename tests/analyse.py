import datetime
import random

from tests import queries


def analyze_mining(deployment, last_layer, layers_per_epoch, layer_avg_size, total_pods):
    """
    Analyze mining assures some vital assertions such as:
    none of the pods restarted (unintentionally) or wasn't deployed,
    all nodes created blocks,
    number of average blocks created match layer_avg_size,
    number of total ATX match the number of pods * number of epochs until last layer,
    average block creation per user

    :param deployment: string, namespace id
    :param last_layer: int, layer number to validate from
    :param layers_per_epoch: int, number of layers per epoch
    :param layer_avg_size: int, average number of blocks per layer
    :param total_pods: int, total number of active pods

    """

    last_layer = int(last_layer)
    layers_per_epoch = int(layers_per_epoch)
    layer_avg_size = int(layer_avg_size)
    total_pods = int(total_pods)

    # need to filter out blocks that have come from last layer
    blockmap, layermap = queries.get_blocks_per_node_and_layer(deployment)
    queries.print_node_stats(blockmap)
    queries.print_layer_stat(layermap)

    xl = [len(layermap[layer]) for layer in range(layers_per_epoch)]
    print(xl)

    first_epoch_blocks = sum(xl)

    # count all blocks arrived in relevant layers
    total_blocks = sum([len(layermap[layer]) for layer in range(last_layer)])

    atxmap = queries.get_atx_per_node(deployment)
    newmap = {}
    for x in atxmap:
        lst = []
        for y in atxmap[x]:
            if y.layer_id != last_layer:  # check layer
                lst.append(y)  # append data
        newmap[x] = lst  # remove last layer
    atxmap = newmap
    total_atxs = sum([len(atxmap[x]) for x in atxmap])

    print("atx created " + str(total_atxs))
    print("blocks created " + str(total_blocks) + " first epoch blocks " + str(first_epoch_blocks))

    # not enough pods deployed or pods restarted
    assert queries.get_nodes_up(deployment) == total_pods
    # not all nodes created blocks
    assert total_pods == len(blockmap)
    # remove blocks created in first epoch since first epoch starts with layer 1
    print("total and first", total_blocks, first_epoch_blocks)
    ass_err = f"all blocks but first epoch={int(total_blocks - first_epoch_blocks)}\n" \
              f"total num of layers={last_layer - layers_per_epoch} layers avg size={layer_avg_size}"
    assert int((total_blocks - first_epoch_blocks) / (last_layer - layers_per_epoch)) / layer_avg_size == 1, ass_err
    # not all nodes produces atx in all epochs
    assert total_atxs == int((last_layer / layers_per_epoch)) * total_pods

    # assert that a node has created one atx per epoch
    for node in atxmap:
        mp = set()
        for blk in atxmap[node]:
            mp.add(blk.published_in_epoch)
        assert len(atxmap[node]) / int((last_layer / layers_per_epoch) + 0.5) == 1
        if len(mp) != int((last_layer / layers_per_epoch)):
            print("mp " + ','.join(mp) + " node " + node + " atxmap " + str(atxmap[node]))
        assert len(mp) == int((last_layer / layers_per_epoch))

    # assert that each node has created layer_avg/number_of_nodes
    for node in blockmap.values():
        blocks_in_relevant_layers = sum([len(node.layers[layer]) for layer in range(layers_per_epoch, last_layer)])
        # need to deduct blocks created in first genesis epoch since it does not follow general mining rules by design
        blocks_created_per_layer = blocks_in_relevant_layers / (last_layer - layers_per_epoch)
        wanted_avg_block_per_node = max(1, int(layer_avg_size / total_pods))
        assert blocks_created_per_layer / wanted_avg_block_per_node == 1


def analyze_propagation(deployment, last_layer, max_block_propagation, max_atx_propagation):
    for i in range(last_layer):
        propagation, msg_time = queries.layer_block_max_propagation(deployment, random.randrange(3, last_layer))
        print(propagation, msg_time)
        assert msg_time < datetime.timedelta(seconds=max_block_propagation)
        propagation, msg_time = queries.all_atx_max_propagation(deployment)
        print(propagation, msg_time)
        assert msg_time < datetime.timedelta(seconds=max_atx_propagation)
