import time

import tests.convenience as conv
import tests.api.parser as parser
from tests.api.sender import ApiSender as Sender
from tests import utils as ut


layers_query_request = '{{"start_layer": {{"number": {start_layer}}}, "end_layer": {{"number": {end_layer}}}}}'


class ApiHandler:
    def __init__(self, ips, namespace):
        self.sender = Sender(ips, namespace)

    def extend_ips(self, ips):
        self.sender.extend_ips(ips)

    def remove_ips(self, ips):
        self.sender.remove_ips(ips)

    # TODO: add timeout decorator
    def wait_for_layer(self, layer_num, timeout=None, interval=10):
        """
        wait for layer_num
        raises ValueError exception if layer has already passed

        :param layer_num: int, wanted layer to wait for
        :param timeout: int, seconds until timeout
        :param interval: int, seconds between iterations
        :return:
        """
        is_timeout = timeout is not None
        if is_timeout and timeout <= 0:
            raise TimeoutError(f"got a timeout while waiting for layer {layer_num}")
        current_layer, elapsed_time = self.sender.send_current_layer_timed(timeout)
        if current_layer > layer_num:
            # raise a value error in case wanted layer has passed
            # raise ValueError(f"layer {layer_num} has already passed")
            return current_layer
        while True:
            if current_layer == layer_num:
                break
            if is_timeout and timeout <= 0:
                raise TimeoutError("wait for layer reached timeout")
            print(f"waiting for layer {layer_num}")
            print(f"sleeping for {interval} before querying for current layer again\n")
            time.sleep(interval)
            timeout = timeout - interval if is_timeout else timeout
            current_layer, elapsed_time = self.sender.send_current_layer_timed(timeout)
            timeout = timeout - elapsed_time if is_timeout else timeout
        return current_layer

    # TODO: add timeout
    def wait_for_epoch(self, epoch_num):
        curr_epoch = self.sender.send_current_epoch()
        if curr_epoch > epoch_num:
            raise ValueError(f"epoch requested is {epoch_num} has already passed, current epoch {curr_epoch}")
        # TODO: add layers per epoch in ApiHandler?
        first_layer, last_layer = self.get_epoch_layer_range(epoch_num)
        print(f"wait for epoch {epoch_num}, epoch first layer {first_layer}")
        return self.wait_for_layer(first_layer)

    # TODO: add timeout
    def wait_for_next_epoch(self):
        curr_epoch = self.sender.send_current_epoch()
        next_epoch_ind = curr_epoch + 1
        # TODO: add layers per epoch in ApiHandler?
        first_layer, last_layer = self.get_epoch_layer_range(next_epoch_ind)
        print(f"wait for next epoch - {next_epoch_ind}, epoch first layer {first_layer}")
        return self.wait_for_layer(first_layer)

    def get_from_all_miners_layer_query(self, layer_num, parsing_func):
        results = []
        layer_query_responses = self.sender.send_all_layer_request(layer_num)
        for layer_info in layer_query_responses:
            results.append(parsing_func(layer_info))
        return results

    def request_layer_hash(self, layer_num):
        return self.get_from_all_miners_layer_query(layer_num, parser.parse_layer_hash_from_layer_query_res)

    def request_layer_state_root_hash(self, layer_num):
        return self.get_from_all_miners_layer_query(layer_num, parser.parse_state_root_hash_from_layer_query_res)

    def is_layer_hash_equal(self, layer_num):
        layer_hashes = self.request_layer_hash(layer_num)
        if not conv.all_list_items_equal(layer_hashes):
            print(f"not all layer hashes equal for layer {layer_num}\nlayer_hashes: {layer_hashes}")
            return False
        return True

    def is_state_root_hash_equal(self, layer_num):
        sr_hash = self.request_layer_state_root_hash(layer_num)
        if not conv.all_list_items_equal(sr_hash):
            print(f"not all layer state root hashes are equal on layer {layer_num}\nstate_root_hashes: {sr_hash}")
            return False
        return True

    def get_current_layer(self):
        return self.sender.send_current_layer()

    def get_epoch_layer_range(self, epoch_num):
        return ut.get_epoch_layer_range(epoch_num, self.sender.layers_per_epoch)

    @staticmethod
    def resolve_api_entry(api):
        api_entry = getattr(ApiHandler, api)
        if not api_entry:
            raise ValueError(f"{api} is not a valid api")
        return api_entry
