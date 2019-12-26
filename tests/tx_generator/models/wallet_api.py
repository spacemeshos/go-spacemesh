from random import random

from tests.test_bs import api_call


class WalletAPI:
    """
    TODO add docstring
    """

    DEBUG = False
    nonce_api = 'v1/nonce'
    submit_api = 'v1/submittransaction'
    balance_api = 'v1/balance'

    def __init__(self, namespace, clients_lst):
        self.clients_lst = clients_lst
        self.namespace = namespace

    def submit_tx(self, to, src, gas_limit, gas_price, amount, txgen):
        pod_ip, pod_name = self.random_node()
        tx_bytes = txgen.generate(to, src, gas_limit, gas_price, amount)
        print(f"submit transaction from {src} to {to} of {amount} with gas price {gas_price}")
        tx_field = '{"tx":' + str(list(tx_bytes)) + '}'
        out = api_call(pod_ip, tx_field, self.submit_api, self.namespace)
        if self.DEBUG:
            print(tx_field)
            print(out)

        return "{'value': 'ok'}" in out

    def get_nonce(self, acc):
        # check nonce
        print(f"getting {acc} nonce")
        pod_ip, pod_name = self.random_node()
        data = '{{"address":"' + acc + '"}'
        return api_call(pod_ip, data, self.nonce_api, self.namespace)

    def get_balance(self, acc):
        # check balance
        pod_ip, pod_name = self.random_node()
        data = '{"address":"' + str(acc) + '"}'
        if self.DEBUG:
            print(data)
        return api_call(pod_ip, data, self.balance_api, self.namespace)

    def random_node(self):
        rnd = random.randint(0, len(self.clients_lst)-1)
        # self.setup_network.clients.pods
        pod_ip, pod_name = self.clients_lst[rnd]['pod_ip'], self.clients_lst[rnd]['name']
        print(f"chosen pod ip = {pod_ip}, name = {pod_name}")
        return pod_ip, pod_name
