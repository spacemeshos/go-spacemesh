import binascii
from pytest_testconfig import config as testconfig
import pprint
import random
import time

from tests.test_bs import setup_network, add_curl, setup_bootstrap, start_poet, setup_clients, wait_genesis
import tests.tx_generator.config as conf
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.tx_generator.models.tx_generator import TxGenerator
from tests.ed25519.eddsa import genkeypair

DEBUG = True
TX_COST = 3  # .Mul(trans.GasPrice, tp.gasCost.BasicTxCost)


def expected_balance(account, acc_pub):
    """
    calculate the account's balance

    :param account: dictionary, account details
    :param acc_pub: string, account's public key

    :return: int, the balance after sending and receiving txs
    """

    balance = sum([int(tx["amount"]) for tx in account["recv"]]) - \
              sum(int(tx["amount"]) + (int(tx["gasprice"]) * TX_COST) for tx in account["send"])

    print(f"balance calculated for {acc_pub}:\n{balance}\neverything:\n{pprint.pformat(account)}")
    return balance


def random_account(accounts):
    pk = random.choice(list(accounts.keys()))
    return pk


def new_account(accounts):
    """
    create a new account and adds it to the accounts data structure

    :param accounts: dictionary, all accounts

    :return: string, public key of the new account
    """

    priv, pub = genkeypair()
    str_pub = bytes.hex(pub)
    accounts[str_pub] = {"priv": bytes.hex(priv), "nonce": 0, "recv": [], "send": []}
    return str_pub


def transfer(wallet_api, accounts, frm, to, amount=None, gas_price=None, gas_limit=None):
    tx_gen = TxGenerator(pub=frm, pri=accounts[frm]['priv'])
    if not amount:
        amount = random.randint(1, expected_balance(accounts[frm], frm) - TX_COST)
    if not gas_price:
        gas_price = 1
    if not gas_limit:
        gas_limit = gas_price + 1

    # create transaction
    tx_bytes = tx_gen.generate(to, accounts[frm]["nonce"], gas_limit, gas_price, amount)
    # submit transaction
    success = wallet_api.submit_tx(to, frm, gas_price, amount, tx_bytes)
    accounts[frm]['nonce'] += 1
    if success:
        accounts[to]["recv"].append({"from": bytes.hex(tx_gen.publicK), "amount": amount, "gasprice": gas_price})
        accounts[frm]["send"].append({"to": to, "amount": amount, "gasprice": gas_price})
        return True
    return False


# def validate_account_nonce(accounts, acc, init_amount=0):
#     out = wallet_api.get_nonce(acc)
#     print(out)
#     if str(accounts[acc]['nonce']) in out:
#         balance = init_amount + expected_balance(acc)
#         print(f"expecting balance: {balance}")
#         if "{'value': '" + str(balance) + "'}" in out:
#             print("{0}, balance ok ({1})".format(str(acc), out))
#             return True
#         return False
#     return False


# account struct
#     {
#         "priv": "81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce5",
#         "nonce": 0,
#         "send": {to: ..., amount: ..., gasprice: .},
#         "recv": {from: , amount, gasprice}
#     }
def test_transactions(setup_network):
    wallet_api = WalletAPI(setup_network.bootstrap.deployment_id, setup_network.clients.pods)
    tap_init_amount = 10000
    acc = conf.acc_pub

    accounts = {
        acc: {
            "priv": conf.acc_priv,
            "nonce": 0,
            "send": {},
            "recv": {}
        }
    }

    # send tx to client via rpc
    test_txs = 10
    for i in range(test_txs):
        balance = tap_init_amount + expected_balance(accounts[acc], acc)
        if balance < 10:  # Stop sending if the tap is out of money
            break
        amount = random.randint(1, int(balance / 10))
        new_acc_pub = new_account(accounts)
        print("TAP NONCE {0}".format(accounts[acc]['nonce']))
        assert transfer(wallet_api, accounts, acc, new_acc_pub, amount=amount), "Transfer from tap failed"
        print("TAP NONCE {0}".format(accounts[acc]['nonce']))
        print("sleeping for 10 secs")
        time.sleep(10)

    print("sleeping for 180 secs")
    time.sleep(180)
    #
    # ready = 0
    # for x in range(int(layers_per_epoch) * 2):  # wait for two epochs (genesis)
    #     ready = 0
    #     print("...")
    #     time.sleep(float(layers_duration))
    #     print("checking tap nonce")
    #     if test_account(acc, tap_init_amount):
    #         print("nonce ok")
    #         for pk in accounts:
    #             if pk == acc:
    #                 continue
    #             print("checking account")
    #             print(pk)
    #             assert test_account(pk), "account {0} didn't have the expected nonce and balance".format(pk)
    #             ready += 1
    #         break
    # assert ready == len(accounts) - 1, "Not all accounts received sent txs"  # one for 0 counting and one for tap.
    #
    # def is_there_a_valid_acc(min_balance, excpect=[]):
    #     lst = []
    #     for acc in accounts:
    #         if expected_balance(acc) - 1 * TX_COST > min_balance and acc not in excpect:
    #             lst.append(acc)
    #     return lst
    #
    # # IF LONGEVITY THE CODE BELOW SHOULD RUN FOREVER
    #
    # TEST_TXS2 = 10
    # newaccounts = []
    # for i in range(TEST_TXS2):
    #     accounts = is_there_a_valid_acc(100, newaccounts)
    #     if len(accounts) == 0:
    #         break
    #
    #     src_acc = random.choice(accounts)
    #     if i % 2 == 0:
    #         # create new acc
    #         pub = new_account()
    #         newaccounts.append(pub)
    #         assert transfer(src_acc, pub), "Transfer from {0} to {1} (new account) failed".format(src_acc, pub)
    #     else:
    #         accfrom = src_acc
    #         accto = random_account()
    #         while accfrom == accto:
    #             accto = random_account()
    #         assert transfer(accfrom, accto), "Transfer from {0} to {1} failed".format(accfrom, accto)
    #
    # ready = 0
    #
    # for x in range(int(layers_per_epoch) * 3):
    #     time.sleep(float(layers_duration))
    #     print("...")
    #     ready = 0
    #     for pk in accounts:
    #         if test_account(pk, init_amount=tap_init_amount if pk is acc else 0):
    #             ready += 1
    #
    #     if ready == len(accounts):
    #         break
    #
    # assert ready == len(accounts), "Not all accounts got the sent txs got: {0}, want: {1}".format(ready,
    #                                                                                               len(accounts) - 1)

#
# if __name__ == "__main__":
#     # execute only if run as a script
#     gen = TxGenerator()
#     data = gen.generate("0000000000000000000000000000000000001111", 12345, 56789, 24680, 86420)
#     # data = gen.generate("0000000000000000000000000000000000002222", 0, 123, 321, 100)
#     # x = (str(list(data)))
#     # print('{"tx":'+ x + '}')
#
#     expected = "00000000000030390000000000000000000000000000000000001111000000000000ddd500000000000060680000000000" \
#                "01519417a80a21b815334b3e9afd1bde2b78ab1e3b17932babd2dab33890c2dbf731f87252c68f3490cce3ee69fd97d450" \
#                "d97d7fcf739b05104b63ddafa1c94dae0d0f"
#     assert (binascii.hexlify(data)).decode('utf-8') == str(expected)
