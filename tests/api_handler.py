import json
import multiprocessing as mp
from random import choice
import time

import tests.utils as ut
import tests.convenience as conv

layers_query_request = '{{"start_layer": {{"number": {start_layer}}}, "end_layer": {{"number": {end_layer}}}}}'


class ApiHandler:
    # mesh endpoints
    layer_query = "v1/mesh/layersquery"
    current_layer = "v1/mesh/currentlayer"
    epoch_num_layers = "v1/mesh/epochnumlayers"

    def __init__(self, ips, namespace):
        self.ips = ips
        self.namespace = namespace
        self.layers_per_epoch = self.get_layers_per_epoch()

    def extend_ips(self, ips):
        self.ips.extend(ips)

    def send(self, data, api, ip=None, retry=3):
        ip = choice(self.ips) if not ip else ip
        api_entry = self.resolve_api_entry(api)
        return ut.api_call(ip, data, api_entry, self.namespace, retry=retry)

    def send_concurrent(self, data, api, return_list, ip=None):
        ip = choice(self.ips) if not ip else ip
        api_entry = self.resolve_api_entry(api)
        return_list.append(ut.api_call(ip, data, api_entry, self.namespace))

    def send_all(self, data, api):
        print(f'sending all miners api request to "{api}" endpoint with data "{data}"')
        processes = []
        return_list = mp.Manager().list()
        for ip in self.ips:
            processes.append(mp.Process(target=self.send_concurrent, args=(data, api, return_list, ip)))
        # Run processes
        for p in processes:
            p.start()
        # Exit the completed processes
        for p in processes:
            p.join()
        return return_list

    # layer_query = "v1/mesh/layersquery" endpoint
    def send_all_layer_request(self, start_layer, end_layer=None):
        return self.send_layer_request(start_layer, end_layer=end_layer, is_all=True)

    # layer_query = "v1/mesh/layersquery" endpoint
    def send_layer_request(self, start_layer, ip=None, end_layer=None, is_all=False):
        # no layer details before second epoch
        minimal_layer = self.layers_per_epoch * 2 - 1
        if start_layer < minimal_layer:
            print(f"minimal layer to query is {minimal_layer}, minimal requested layer: {start_layer}")
            return
        # select a random ip in case no ip was supplied and is_all is False
        ip = choice(self.ips) if not ip and not is_all else ip
        # set end_layer as start_layer in case no end_layer was supplied
        end_layer = start_layer if not end_layer else end_layer
        data = layers_query_request.format(start_layer=start_layer, end_layer=end_layer)
        if is_all:
            return self.send_all(data, "layer_query")
        return [self.send(data, "layer_query", ip)]

    def request_layer_hash(self, layer_num):
        layer_hashes = []
        layers_info = self.send_all_layer_request(layer_num)
        for layer_info in layers_info:
            layer_info_dict = self.set_response_to_json_format(layer_info)
            layer_hashes.append(self.parse_layer_hash_from_layer_query_res(layer_info_dict))
        return layer_hashes

    def is_layer_hash_equal(self, layer_num):
        layer_hashes = self.request_layer_hash(layer_num)
        if not conv.all_list_items_equal(layer_hashes):
            print(f"not all layer hashes equal for layer {layer_num}\nlayer_hashes: {layer_hashes}")
            return False
        return True

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
        current_layer, elapsed_time = self.get_current_layer(timeout)
        if current_layer > layer_num:
            # raise a value error in case wanted layer has passed
            raise ValueError(f"layer {layer_num} has already passed")
        while True:
            if current_layer == layer_num:
                break
            if is_timeout and timeout <= 0:
                raise TimeoutError("wait for layer reached timeout")
            print(f"waiting for layer {layer_num}")
            print(f"sleeping for {interval} before querying for current layer again\n")
            time.sleep(interval)
            timeout = timeout - interval if is_timeout else timeout
            current_layer, elapsed_time = self.get_current_layer(timeout)
            timeout = timeout - elapsed_time if is_timeout else timeout
        return current_layer

    @ut.timing
    def get_current_layer(self, timeout=None, interval=10):
        res_numeric = None
        is_timeout = timeout is not None
        while res_numeric is None or not all(elem == res_numeric[0] for elem in res_numeric):
            if is_timeout and timeout < 0:
                raise TimeoutError(f"could not resolve current layer")
            res = self.send_all('{}', 'current_layer')
            # set response result of current layer query from string to json format
            res_jsons = [self.set_response_to_json_format(layer_res) for layer_res in res]
            res_numeric = [res_json['layernum']['number'] for res_json in res_jsons]
            if all(elem == res_numeric[0] for elem in res_numeric):
                break
            res_numeric = res_numeric.sort()
            print(f"miners are not synced about current layer, lowest: {res_numeric[0]} highest: {res_numeric[-1]}")
            print(f"sleeping for {interval}")
            time.sleep(interval)
            timeout = timeout - interval if is_timeout else timeout
        print(f"current layer is {res_numeric[0]}")
        return res_numeric[0]

    def get_layers_per_epoch(self):
        res = self.send("{}", "epoch_num_layers")
        res = self.set_response_to_json_format(res)
        return int(self.parse_layer_per_epoch_msg(res))

    @staticmethod
    def set_response_to_json_format(res):
        res = res.replace("'", "\"")
        return json.loads(res)

    @staticmethod
    def resolve_api_entry(api):
        api_entry = getattr(ApiHandler, api)
        if not api_entry:
            raise ValueError(f"{api} is not a valid api")
        return api_entry

    @staticmethod
    def parse_layer_hash_from_layer_query_res(layer_query):
        # {'layer': ['hash': '[\d\w=]+']}
        return layer_query['layer'][0]['hash']

    @staticmethod
    def parse_layer_per_epoch_msg(msg):
        # {'numlayers': {'value': '\d+'}}
        return msg['numlayers']['value']
