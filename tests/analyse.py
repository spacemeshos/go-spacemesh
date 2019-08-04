from tests import queries


def analyze_mining(deployment, last_layer, layers_per_epoch, layer_avg_size, total_pods):

    last_layer = int(last_layer)
    layers_per_epoch = int(layers_per_epoch)
    layer_avg_size = int(layer_avg_size)
    total_pods = int(total_pods)

    # need to filter out blocks that have come from last layer
    blockmap, layermap = queries.get_blocks_per_node_and_layer(deployment)
    queries.print_node_stats(blockmap)
    queries.print_layer_stat(layermap)

    xl = [len(layermap[str(x)]) for x in range(layers_per_epoch) if str(x) in layermap]
    print(str(xl))

    first_epoch_blocks = sum(xl)

    # count all blocks arrived in relevant layers
    total_blocks = sum([len(layermap[str(x)]) for x in range(last_layer) if str(x) in layermap])

    atxmap = queries.get_atx_per_node(deployment)
    newmap = {}
    for x in atxmap:
        lst = []
        for y in atxmap[x]:
            if y[1] != last_layer:  # check layer
                lst.append(y[0])  # append data
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
    assert int((total_blocks - first_epoch_blocks) / (last_layer - layers_per_epoch)) / layer_avg_size == 1
    # not all nodes produces atx in all epochs
    assert total_atxs == int((last_layer / layers_per_epoch)) * total_pods

    # assert that a node has created one atx per epoch
    for node in atxmap:
        mp = set()
        for blk in atxmap[node]:
            mp.add(blk[4])
        assert len(atxmap[node]) / int((last_layer / layers_per_epoch) + 0.5) == 1
        if len(mp) != int((last_layer / layers_per_epoch)):
            print("mp " + ','.join(mp) + " node " + node + " atxmap " + str(atxmap[node]))
        assert len(mp) == int((last_layer / layers_per_epoch))

    # assert that each node has created layer_avg/number_of_nodes
    for node in blockmap:
        blocks_in_relevant_layers = sum([len(blockmap[node]["layers"][str(x)]) for x in range(layers_per_epoch, last_layer) if str(x) in blockmap[node]["layers"]])
        # need to deduct blocks created in first genesis epoch since it does not follow general mining rules by design
        blocks_created_per_layer = blocks_in_relevant_layers / (last_layer - layers_per_epoch)
        wanted_avg_block_per_node = max(1, int(layer_avg_size / total_pods))
        assert blocks_created_per_layer / wanted_avg_block_per_node == 1
