import random
from tests.tx_generator import config as conf

from tests.test_bs import api_call


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
        self.clients_lst = clients_lst
        self.namespace = namespace

    def submit_tx(self, to, src, gas_price, amount, tx_bytes):
        pod_ip, pod_name = self.random_node()
        print(f"submit transaction from {src} to {to}\nusing {pod_name}")
        print(f"amount:{amount} gas price: {gas_price}")
        tx_field = '{"tx":' + str(list(tx_bytes)) + '}'
        out = api_call(pod_ip, tx_field, self.submit_api, self.namespace)
        print(f"submit result: {out}")

        return self.a_ok in out

    def get_nonce(self, acc):
        # check nonce
        print(f"querying for nonce of {acc}")
        pod_ip, pod_name = self.random_node()
        data = '{"address":"' + acc + '"}'
        out = api_call(pod_ip, data, self.nonce_api, self.namespace)
        print(f"nonce output={out}")
        return out

    def get_balance(self, acc):
        # check balance
        pod_ip, pod_name = self.random_node()
        data = '{"address":"' + acc[-self.ADDRESS_SIZE_HEX:] + '"}'
        if acc == conf.acc_pub:
            data = '{"address":"' + acc + '"}'

        print(f"querying for balance of {acc} using {pod_name}")
        out = api_call(pod_ip, data, self.balance_api, self.namespace)
        print(f"balance output={out}")
        return out

    def random_node(self):
        rnd = random.randint(0, len(self.clients_lst)-1)
        pod_ip, pod_name = self.clients_lst[rnd]['pod_ip'], self.clients_lst[rnd]['name']
        print(f"random selected pod: ip = {pod_ip}, name = {pod_name}")
        return pod_ip, pod_name
