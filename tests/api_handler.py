import json
import multiprocessing as mp
from random import choice
import time

import tests.utils as ut

layers_query_request = '{{"start_layer": {{"number": {start_layer}}}, "end_layer": {{"number": {end_layer}}}}}'


class ApiHandler:
    layer_query = "v1/mesh/layersquery"
    current_layer = "v1/mesh/currentlayer"

    def __init__(self, ips, namespace):
        self.ips = ips
        self.namespace = namespace

    def send(self, data, api, ip=None):
        ip = choice(self.ips) if not ip else ip
        api_entry = self.resolve_api(api)
        return ut.api_call(ip, data, api_entry, self.namespace)

    def send_concurrent(self, data, api, return_list, ip=None):
        ip = choice(self.ips) if not ip else ip
        api_entry = self.resolve_api(api)
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

    def get_blocks_created(self, start_layer, ip=None, end_layer=None):
        # select a random ip in case no ip was supplied
        ip = choice(self.ips) if not ip else ip
        # set end_layer as start_layer in case no end_layer was supplied
        end_layer = start_layer if not end_layer else end_layer
        data = layers_query_request.format(start_layer=start_layer, end_layer=end_layer)
        return self.send(data, "layer_query", ip)

    def wait_for_layer(self, layer_num, timeout=None, interval=10):
        """
        wait for layer_num
        raises ValueError exception if layer has already passed

        :param layer_num: int, wanted layer to wait for
        :param timeout: int, seconds until timeout
        :return:
        """
        if timeout is not None and timeout <= 0:
            raise TimeoutError(f"got a timeout while waiting for layer {layer_num}")
        current_layer, elapsed_time = self.get_current_layer(timeout)
        if current_layer > layer_num:
            # raise a value error in case wanted layer has passed
            raise ValueError(f"layer {layer_num} has already passed")
        elif current_layer < layer_num:
            print(f"waiting for layer {layer_num}")
            print(f"sleeping for {interval} before querying for current layer again\n")
            time.sleep(interval)
            timeout -= interval
            self.wait_for_layer(layer_num, timeout - elapsed_time)

    @ut.timing
    def get_current_layer(self, timeout=None, interval=10):
        res_numeric = None
        while res_numeric is None or not all(elem == res_numeric[0] for elem in res_numeric):
            if timeout is not None and timeout < 0:
                raise TimeoutError(f"could not resolve current layer")
            res = self.send_all('{}', 'current_layer')
            res_jsons = [self.set_response_to_json_format(layer_res) for layer_res in res]
            res_numeric = [res_json['layernum']['number'] for res_json in res_jsons]
            if all(elem == res_numeric[0] for elem in res_numeric):
                break
            # TODO: print min and max layers
            print('not all miners are synced on current layer number')
            print(f"sleeping for {interval}")
            time.sleep(interval)
            timeout -= interval
        print(f"current layer is {res_numeric[0]}")
        return res_numeric[0]

    @staticmethod
    def set_response_to_json_format(res):
        res = res.replace("'", "\"")
        return json.loads(res)

    @staticmethod
    def resolve_api(api):
        api_entry = getattr(ApiHandler, api)
        if not api_entry:
            raise ValueError(f"{api} is not a valid api")
        return api_entry
