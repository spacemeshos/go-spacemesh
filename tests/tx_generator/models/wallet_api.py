from datetime import datetime
import random
import re
from requests.models import Response

from tests.tx_generator import config as conf
from tests.tx_generator.k8s_handler import api_call, aws_api_call


class WalletAPI:
    """
    WalletAPI communicates with a random miner pod for
    information such as a wallet nonce, balance and
    submitting transactions

    """

    ADDRESS_SIZE_HEX = 40
    balance_api = 'v1/balance'
    get_tx_api = 'v1/gettransaction'
    nonce_api = 'v1/nonce'
    submit_api = 'v1/submittransaction'
    a_ok = "'value': 'ok'"

    def __init__(self, namespace, clients_lst, fixed_node=None):
        """

        :param namespace: string, namespace
        :param clients_lst: [{"pod_ip": ..., "name": ...}, ...]
        """
        # TODO make fixed node boolean and create a @property to choose index once
        self.clients_lst = clients_lst
        self.namespace = namespace
        self.fixed_node = fixed_node
        self.tx_ids = []

    def submit_tx(self, to, src, gas_price, amount, tx_bytes):
        print(f"\n{datetime.now()}: submit transaction\nfrom {src}\nto {to}")
        pod_ip, pod_name = self.random_node()
        print(f"amount: {amount}, gas-price: {gas_price}, total: {amount+gas_price}")
        tx_field = '{"tx":' + str(list(tx_bytes)) + '}'
        out = self.send_api_call(pod_ip, tx_field, self.submit_api)
        print(f"{datetime.now()}: submit result: {out}")

        if self.a_ok in out:
            self.tx_ids.append(self.extract_tx_id(out))
            return True

        return False

    def get_tx_by_id(self, tx_id):
        print(f"get transaction with id {tx_id}")
        tx_id_lst = self.convert_hex_str_to_bytes(tx_id)
        pod_ip, pod_name = self.random_node()
        data = f'{{"id": {str(tx_id_lst)}}}'
        out = self.send_api_call(pod_ip, data, self.get_tx_api)
        print(f"get tx output={out}")
        return self.extract_tx_id(out)

    def get_nonce_value(self, acc):
        res = self._get_nonce(acc)
        print("#@!#@! get_nonce_value", res)
        print("#@!#@! get_nonce_value type", type(res))
        return WalletAPI.extract_nonce_from_resp(res)

    def get_balance_value(self, acc):
        res = self._get_balance(acc)
        return WalletAPI.extract_balance_from_resp(res)

    def _get_nonce(self, acc):
        return self._make_address_api_call(acc, "nonce", self.nonce_api)

    def _get_balance(self, acc):
        return self._make_address_api_call(acc, "balance", self.balance_api)

    def _make_address_api_call(self, acc, resource, api_res):
        # check balance/nonce
        print(f"\ngetting {resource} for", acc)
        pod_ip, pod_name = self.random_node()
        data = '{"address":"' + acc[-self.ADDRESS_SIZE_HEX:] + '"}'
        if acc == conf.acc_pub:
            data = '{"address":"' + acc + '"}'

        print(f"querying for the {resource} of {acc}")
        out = self.send_api_call(pod_ip, data, api_res)

        print(f"{resource} output={out}")
        return out

    def random_node(self):
        """
        gets a random node from nodes list
        if fixed is set then the node at the nodes[fixed]
        will be returned, this may be useful in stress tests

        :return: string string, chosen pod ip and chosen pod name
        """

        rnd = random.randint(0, len(self.clients_lst)-1) if not self.fixed_node else self.fixed_node
        pod_ip, pod_name = self.clients_lst[rnd]['pod_ip'], self.clients_lst[rnd]['name']
        if not self.fixed_node:
            print("randomly ", end="")

        print(f"selected pod: ip = {pod_ip}, name = {pod_name}")
        return pod_ip, pod_name

    def send_api_call(self, pod_ip, data, api_resource):
        if self.namespace:
            out = api_call(pod_ip, data, api_resource, self.namespace)
        else:
            out = aws_api_call(pod_ip, data, api_resource)
            print("#@!#@! send_api_call out", out)
            if out.status_code == 200:
                print("#@!#@! status is 200")
                out = out.text
            else:
                print("status code != 200, output =", out.text)
                out = None

        return out

    # ======================= utils =======================

    @staticmethod
    def extract_value_from_resp(val):
        res_pat = "value[\'\"]:\s?[\'\"]([0-9]+)"
        group_num = 1

        match = re.search(res_pat, val)
        if match:
            return match.group(group_num)

        return None

    @staticmethod
    def extract_nonce_from_resp(nonce_res):
        res = WalletAPI.extract_value_from_resp(nonce_res)
        if res:
            return int(res)

        return None

    @staticmethod
    def extract_balance_from_resp(balance_res):
        res = WalletAPI.extract_value_from_resp(balance_res)
        if res:
            return int(res)

        return None

    @staticmethod
    def extract_tx_id(tx_output):
        id_pat = r"'value': 'ok', 'id': '([0-9a-f]{64})'"
        group_num = 1

        match = re.search(id_pat, tx_output)
        if match:
            return match.group(group_num)

        return None

    @staticmethod
    def convert_hex_str_to_bytes(hex_str):
        """
        :param hex_str: string, 64 bytes (32 byte hex rep)
        :return: list, a list of 32 integers (32 bytes rep)
        """
        return list(bytearray.fromhex(hex_str))
