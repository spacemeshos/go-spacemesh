import random
import re

from tests.tx_generator import config as conf
from tests.tx_generator.k8s_handler import api_call


class WalletAPI:
    """
    WalletAPI communicates with a random miner pod for
    information such as a wallet nonce, balance and
    submitting transactions

    """

    ADDRESS_SIZE_HEX = 40
    nonce_api = 'v1/nonce'
    submit_api = 'v1/submittransaction'
    balance_api = 'v1/balance'
    a_ok = "'value': 'ok'"

    def __init__(self, namespace, clients_lst):
        """

        :param namespace: string, namespace
        :param clients_lst: [{"pod_ip": ..., "name": ...}, ...]
        """
        self.clients_lst = clients_lst
        self.namespace = namespace

    def submit_tx(self, to, src, gas_price, amount, tx_bytes):
        print(f"\nsubmit transaction\nfrom {src}\nto {to}")
        pod_ip, pod_name = self.random_node()
        print(f"amount: {amount}, gas-price: {gas_price}, total: {amount+gas_price}")
        tx_field = '{"tx":' + str(list(tx_bytes)) + '}'
        out = api_call(pod_ip, tx_field, self.submit_api, self.namespace)
        print(f"submit result: {out}")

        return self.a_ok in out

    def get_nonce_value(self, acc):
        res = self._get_nonce(acc)
        return WalletAPI.extract_nonce_from_resp(res)

    def get_balance_value(self, acc):
        res = self._get_balance(acc)
        return WalletAPI.extract_balance_from_resp(res)

    def _get_nonce(self, acc):
        # check nonce
        print(f"\ngetting the nonce of {acc}")
        pod_ip, pod_name = self.random_node()
        data = '{"address":"' + acc[-self.ADDRESS_SIZE_HEX:] + '"}'
        if acc == conf.acc_pub:
            data = '{"address":"' + acc + '"}'

        print(f"querying for {data}")
        out = api_call(pod_ip, data, self.nonce_api, self.namespace)
        print(f"nonce output={out}")
        return out

    def _get_balance(self, acc):
        # check balance
        print("\ngetting balance for", acc)
        pod_ip, pod_name = self.random_node()
        data = '{"address":"' + acc[-self.ADDRESS_SIZE_HEX:] + '"}'
        if acc == conf.acc_pub:
            data = '{"address":"' + acc + '"}'

        print(f"querying for the balance of {acc}")
        out = api_call(pod_ip, data, self.balance_api, self.namespace)
        print(f"balance output={out}")
        return out

    def random_node(self):
        rnd = random.randint(0, len(self.clients_lst)-1)
        pod_ip, pod_name = self.clients_lst[rnd]['pod_ip'], self.clients_lst[rnd]['name']
        print(f"random selected pod: ip = {pod_ip}, name = {pod_name}")
        return pod_ip, pod_name

    # ======================= utils =======================

    @staticmethod
    def extract_from_resp(val):
        res_pat = "value\': \'([0-9]+)"
        group_num = 1

        match = re.search(res_pat, val)
        if match:
            return match.group(group_num)

        return None

    @staticmethod
    def extract_nonce_from_resp(nonce_res):
        res = WalletAPI.extract_from_resp(nonce_res)
        if res:
            return int(res)

        return None

    @staticmethod
    def extract_balance_from_resp(balance_res):
        res = WalletAPI.extract_from_resp(balance_res)
        if res:
            return int(res)

        return None
