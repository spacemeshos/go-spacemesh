import json
import multiprocessing as mp
from random import choice
import time

import tests.utils as ut
import tests.api.parser as parser


layers_query_request = '{{"start_layer": {{"number": {start_layer}}}, "end_layer": {{"number": {end_layer}}}}}'


class ApiSender:
    # mesh endpoints
    layer_query = "v1/mesh/layersquery"
    current_layer = "v1/mesh/currentlayer"
    epoch_num_layers = "v1/mesh/epochnumlayers"
    current_epoch = "v1/mesh/currentepoch"
    # smesher
    smesher_id = "v1/smesher/smesherid"

    def __init__(self, ips, namespace):
        self.ips = ips
        self.namespace = namespace
        self.layers_per_epoch = self.send_layers_per_epoch()
        self.node_ip_to_id = {}

    def extend_ips(self, ips):
        self.ips.extend(ips)

    def remove_ips(self, ips):
        self.ips = [ip for ip in self.ips if ip not in ips]

    def send(self, data, api, ip=None, retry=3):
        ip = choice(self.ips) if not ip else ip
        api_entry = self.resolve_api_entry(api)
        res = ut.api_call(ip, data, api_entry, self.namespace, retry=retry)
        return self.set_response_to_json_format(res)

    def send_concurrent(self, data, api, return_list, ip=None):
        ip = choice(self.ips) if not ip else ip
        api_entry = self.resolve_api_entry(api)
        res = ut.api_call(ip, data, api_entry, self.namespace)
        return_list.append(self.set_response_to_json_format(res))

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

    # epoch_num_layers = "v1/mesh/epochnumlayers"
    def send_layers_per_epoch(self):
        res = self.send("{}", "epoch_num_layers")
        return int(parser.parse_layer_per_epoch_msg(res))

    # epoch_num_layers = "v1/mesh/epochnumlayers"
    def send_current_epoch(self):
        res = self.send("{}", "current_epoch")
        return int(parser.parse_current_epoch(res))

    # layer_query = "v1/mesh/layersquery" endpoint
    def send_layer_query(self, start_layer, ip=None, end_layer=None, is_all=False):
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

    # layer_query = "v1/mesh/layersquery"
    def send_all_layer_request(self, start_layer, end_layer=None):
        return self.send_layer_query(start_layer, end_layer=end_layer, is_all=True)

    # TODO: remove timing decorator after adding timeout decorator
    @ut.timing
    def send_current_layer(self, timeout=None, ip=None, interval=10):
        res_layer_idx = None
        is_timeout = timeout is not None
        while res_layer_idx is None or not all(elem == res_layer_idx[0] for elem in res_layer_idx):
            if is_timeout and timeout < 0:
                raise TimeoutError(f"could not resolve current layer")
            # if ip is supplied then query only this miners ip else query all
            results = [self.send('{}', 'current_layer', ip)] if ip else self.send_all('{}', 'current_layer')
            res_layer_idx = [parser.parse_layer_number(res) for res in results]
            if all(elem == res_layer_idx[0] for elem in res_layer_idx):
                break
            res_layer_idx.sort()
            print(f"miners are not synced about current layer, lowest: {res_layer_idx[0]} highest: {res_layer_idx[-1]}")
            print(f"sleeping for {interval}")
            time.sleep(interval)
            timeout = timeout - interval if is_timeout else timeout
        print(f"current layer is {res_layer_idx[0]}")
        return res_layer_idx[0]

    # TODO: habitat self.node_ip_to_id using this func in the constructor
    # smesher_id = "v1/smesher/smesherid"
    def send_smesher_id(self, node_ip):
        # v1/smesher/smesherid
        res = self.send('{}', "smesher_id", node_ip)
        smesher_id_base64 = parser.parse_smesher_id(res)
        self.node_ip_to_id[node_ip] = smesher_id_base64
        return self.node_ip_to_id[node_ip]

    # smesher_id = /v1/smesher/smesherid
    def get_node_id_by_node_ip(self, node_ip):
        if node_ip in self.node_ip_to_id:
            return self.node_ip_to_id[node_ip]
        return self.send_smesher_id(node_ip)

    @staticmethod
    def resolve_api_entry(api):
        api_entry = getattr(ApiSender, api)
        if not api_entry:
            raise ValueError(f"{api} is not a valid api")
        return api_entry

    @staticmethod
    def set_response_to_json_format(res):
        res = res.replace("'", "\"")
        return json.loads(res)
