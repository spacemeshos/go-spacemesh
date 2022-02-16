import datetime
import random
from collections import defaultdict
from typing import Dict, List

from tests import queries
from tests.queries import Atx, Block


class EpochSummary(object):
    node_weights: Dict[str, int]
    node_num_blocks: Dict[str, int]

    def __init__(self):
        self.node_weights = dict()
        self.node_num_blocks = defaultdict(int)

    def add_atx(self, atx: Atx):
        self.node_weights[atx.node_id] = atx.weight

    def add_block(self, block: Block):
        self.node_num_blocks[block.node_id] += 1

    def total_weight(self):
        return sum(self.node_weights.values())


def get_epoch_summaries(atxs: List[Atx], blocks: List[Block]) -> Dict[int, EpochSummary]:
    epoch_summaries: defaultdict[int, EpochSummary] = defaultdict(EpochSummary)
    for atx in atxs:
        target_epoch = atx.published_in_epoch + 1
        epoch_summaries[target_epoch].add_atx(atx)
    for block in blocks:
        epoch_summaries[block.epoch_id].add_block(block)
    return dict(epoch_summaries)


def analyze_mining(deployment: str, total_epochs: int, layers_per_epoch: int, blocks_per_layer: int, total_nodes: int):
    last_full_epoch = total_epochs - 1
    blocks_per_epoch = layers_per_epoch * blocks_per_layer
    epoch_summaries = get_epoch_summaries(queries.get_atxs(deployment), queries.get_blocks(deployment))
    epoch_summaries = {e: s for e, s in epoch_summaries.items() if e <= last_full_epoch}
    assert max(epoch_summaries.keys()) == last_full_epoch, \
        f"max(epoch_summaries.keys()): {max(epoch_summaries.keys())} last_epoch: {last_full_epoch}"
    print(f"\nValidating {total_epochs - 2} epochs (2-{last_full_epoch}), {layers_per_epoch} layers each. "
          f"Targeting {blocks_per_epoch} blocks per epoch (actual should be lower).\n")
    for epoch_id in range(2, last_full_epoch + 1):
        summary = epoch_summaries[epoch_id]
        assert len(summary.node_weights) == total_nodes
        for node_id, weight in summary.node_weights.items():
            expected_blocks = max(1, blocks_per_epoch * weight // summary.total_weight())
            assert summary.node_num_blocks[node_id] == expected_blocks, \
                f"summary.node_num_blocks[{node_id}]: {summary.node_num_blocks[node_id]}\n" \
                f"expected_blocks: {expected_blocks}\n" \
                f"epoch_id: {epoch_id}"
        print(f"✅  Validated epoch {epoch_id}: "
              f"{len(summary.node_weights)} nodes produced {sum(summary.node_num_blocks.values())} blocks "
              f"(min: {min(summary.node_num_blocks.values())}, max: {max(summary.node_num_blocks.values())}).")


def analyze_propagation(deployment, last_layer, max_block_propagation, max_atx_propagation):
    for i in range(last_layer):
        propagation, msg_time = queries.layer_block_max_propagation(deployment, random.randrange(3, last_layer))
        print(propagation, msg_time)
        assert msg_time < datetime.timedelta(seconds=max_block_propagation)
        propagation, msg_time = queries.all_atx_max_propagation(deployment)
        print(propagation, msg_time)
        assert msg_time < datetime.timedelta(seconds=max_atx_propagation)
