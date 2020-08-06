from datetime import datetime
import re

from spacemesh.v1 import tx_pb2_grpc, tx_types_pb2, global_state_pb2_grpc, global_state_types_pb2, types_pb2
import grpc
from grpc_status import rpc_status


class WalletAPI:
    """
    WalletAPI communicates with a gateway node for
    information such as a wallet nonce, balance and
    submitting transactions

    """

    ADDRESS_SIZE_HEX = 40
    balance_api = 'v1/balance'
    get_tx_api = 'v1/gettransaction'
    nonce_api = 'v1/nonce'
    submit_api = 'v1/submittransaction'

    def __init__(self, host, port):
        """

        :param host: hostname/IP address of gateway node
        :param port: port of GRPC server of gateway node
        """
        # TODO make fixed node boolean and create a @property to choose index once
        self.host = host
        self.port = port
        self.tx_ids = []

        # GRPC stuff
        self._channel = None
        self._grpc_gs_stub = None
        self._grpc_tx_stub = None

    def _grpc_connect_channel(self):
        if not self._channel:
            self._channel = grpc.insecure_channel(self.host + ':' + self.port)

    def _grpc_connect_tx(self):
        self._grpc_connect_channel()
        if not self._grpc_tx_stub:
            self._grpc_tx_stub = tx_pb2_grpc.TransactionServiceStub(self._channel)

    def _grpc_connect_gs(self):
        self._grpc_connect_channel()
        if not self._grpc_gs_stub:
            self._grpc_gs_stub = global_state_pb2_grpc.GlobalStateServiceStub(self._channel)

    def _grpc_submit_tx(self, tx_bytes):
        self._grpc_connect_tx()

        # Submit it
        submit_tx_req = tx_types_pb2.SubmitTransactionRequest(transaction=tx_bytes)
        try:
            response = self._grpc_tx_stub.SubmitTransaction(submit_tx_req)
        except grpc.RpcError as e:
            print(f"SubmitTransaction GRPC call failed with error: {e}")
            print("Is the connected node fully synced?")
            return False

        # Check that it succeeded
        # see https://cloud.google.com/tasks/docs/reference/rpc/google.rpc#google.rpc.Status
        # https://cloud.google.com/apis/design/errors#handling_errors
        # https://grpc.github.io/grpc/python/grpc.html#grpc-status-code
        if rpc_status.to_status(response.status).code != grpc.StatusCode.OK:
            print(f"SubmitTransaction GRPC call returned failure status with message: {response.status.message}")
            return False
        return response.txstate

    def _grpc_get_account(self, address):
        self._grpc_connect_gs()
        return self._grpc_gs_stub.Account(
            global_state_types_pb2.AccountRequest(
                account_id=types_pb2.AccountId(address=bytes.fromhex(address)))).account_wrapper

    def submit_tx(self, to, src, gas_price, amount, tx_bytes):
        print(f"\n{datetime.now()}: submit transaction\nfrom {src}\nto {to}")
        print(f"amount: {amount}, gas-price: {gas_price}, total: {amount+gas_price}")
        out = self._grpc_submit_tx(tx_bytes)
        print(f"{datetime.now()}: submit result: state: {out.state} txid: {out.id.id.hex()}")
        self.tx_ids.append(out.id.id)
        return True

    def get_tx_by_id(self, tx_id):
        print(f"get transaction with id {tx_id}")
        tx_id_lst = self.convert_hex_str_to_bytes(tx_id)
        data = f'{{"id": {str(tx_id_lst)}}}'
        out = self.send_api_call(data, self.get_tx_api)
        print(f"get tx output={out}")
        return self.extract_tx_id(out)

    def get_nonce_value(self, acc):
        return self._get_nonce(acc)

    def get_balance_value(self, acc):
        return self._get_balance(acc)

    def _get_nonce(self, acc):
        return self._make_address_api_call(acc, "nonce", self.nonce_api)

    def _get_balance(self, acc):
        return self._make_address_api_call(acc, "balance", self.balance_api)

    def _make_address_api_call(self, acc, resource, api_res):
        # check balance/nonce
        print(f"\ngetting {resource} for", acc)
        account = self._grpc_get_account(acc)
        if resource == "nonce":
            print(f"\ngot {account.counter}")
            return account.counter
        elif resource == "balance":
            print(f"\ngot {account.balance.value}")
            return account.balance.value
        return None

    # ======================= utils =======================

    @staticmethod
    def extract_value_from_resp(val):
        if not val:
            print("cannot extract value, input is None")
            return None

        res_pat = "value[\'\"]:\s?[\'\"]([0-9]+)"
        group_num = 1

        match = re.search(res_pat, val)
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
