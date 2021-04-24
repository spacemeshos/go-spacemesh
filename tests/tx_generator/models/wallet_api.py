import base64
from datetime import datetime
import json
import random
import re

from tests.tx_generator.k8s_handler import api_call, aws_api_call


class WalletAPI:
    """
    WalletAPI communicates with a [random] miner pod for
    information such as a wallet nonce, balance and
    submitting transactions

    """

    ADDRESS_SIZE_BYTES = 20
    account_api = 'v1/globalstate/account'
    get_tx_api = 'v1/transaction/transactionsstate'
    submit_api = 'v1/transaction/submittransaction'

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

    def submit_tx(self, to, src, gas_price, amount, nonce, tx_bytes):
        print(f"\n{datetime.now()}: submit transaction\nfrom {src}\nto {to}")
        pod_ip, pod_name = self.random_node()
        print(f"nonce: {nonce}, amount: {amount}, gas-price: {gas_price}, total: {amount+gas_price}")
        tx_str = base64.b64encode(tx_bytes).decode('utf-8')
        print(f"txbytes in base64: {tx_str}")
        tx_field = '{"transaction": "' + tx_str + '"}'
        out = self.send_api_call(pod_ip, tx_field, self.submit_api)
        print(f"{datetime.now()}: submit result: {out}")
        if not out:
            print("cannot parse submission result, result is none")
            return False

        res = self.decode_response(out)
        try:
            if res['txstate']['state'] == 'TRANSACTION_STATE_MEMPOOL':
                print("tx submission successful")
                self.tx_ids.append(self.extract_tx_id(out))
                return True
        except KeyError:
            print(f"failed to parse transaction!\n\n{res}")
            return False

        print("tx submission failed, bad status or txstate")
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
        return self._get_nonce(acc)

    def get_balance_value(self, acc):
        return self._get_balance(acc)

    def _get_nonce(self, acc):
        return self._make_address_api_call(acc, "counter")

    def _get_balance(self, acc):
        return self._make_address_api_call(acc, "balance")

    def _make_address_api_call(self, acc, resource):
        # get account state to check balance/nonce
        print(f"\ngetting {resource} of {acc}")
        pod_ip, pod_name = self.random_node()

        # API expects binary address in base64 format, must be converted to string to pass into curl
        address = base64.b64encode(bytes.fromhex(acc)[-self.ADDRESS_SIZE_BYTES:]).decode('utf-8')
        data = '{"account_id": {"address":"' + address + '"}}'
        print(f"api input: {data}")
        out = self.send_api_call(pod_ip, data, self.account_api)
        print(f"api output: {out}")

        # Try to decode the response. If we fail that probably just means the data isn't there.
        try:
            out = self.decode_response(out)['account_wrapper']['state_projected']
        except json.decoder.JSONDecodeError:
            raise Exception(f"missing or malformed account data for address {acc}")

        # GRPC doesn't include zero values so use intelligent defaults here
        if resource == 'balance':
            return int(out.get('balance', {}).get('value', 0))
        elif resource == 'counter':
            return int(out.get('counter', 0))

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
            if out.status_code == 200:
                out = out.text
            else:
                print("status code != 200, output =", out.text)
                out = None

        return out

    # ======================= utils =======================

    @staticmethod
    def extract_tx_id(tx_output):
        if not tx_output:
            print("cannot extract id from output, input is None")
            return None

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

    @staticmethod
    def decode_response(res):
        p = re.compile('(?<!\\\\)\'')
        res = p.sub('\"', res)
        return json.loads(res)
