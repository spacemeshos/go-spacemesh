import binascii
from enum import Enum
from pytest_testconfig import config as testconfig
import pprint
import random
import time

from tests.test_bs import setup_network, add_curl, setup_bootstrap, start_poet, setup_clients, wait_genesis
import tests.tx_generator.config as conf
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.tx_generator.models.tx_generator import TxGenerator
from tests.ed25519.eddsa import genkeypair

TX_COST = 1  # .Mul(trans.GasPrice, tp.gasCost.BasicTxCost)

ACCOUNTS = {"priv": "", "nonce": 0, "recv": [], "send": []}
RECV = {"from_acc": "", "amount": 0, "gasprice": 0}
SEND = {"to": "", "amount": 0, "gasprice": 0}


def set_tap_acc():
    return dict(ACCOUNTS, priv=conf.acc_priv, recv=[], send=[])


def set_account(priv, nonce=0, recv=None, send=None):
    receive = [] if not recv else recv
    send_lst = [] if not send else send
    return dict(ACCOUNTS, priv=priv, nonce=nonce, recv=receive, send=send_lst)


def set_recv(_from, amount, gasprice):
    return dict(RECV, from_acc=_from, amount=amount, gasprice=gasprice)


def set_send(to, amount, gasprice):
    return dict(SEND, to=to, amount=amount, gasprice=gasprice)


def expected_balance(account, acc_pub, debug=False):
    """
    calculate the account's balance

    :param account: dictionary, account details
    :param acc_pub: string, account's public key
    :param debug: bool, print balance or not

    :return: int, the balance after sending and receiving txs
    """

    balance = sum([int(tx["amount"]) for tx in account["recv"]]) - \
              sum(int(tx["amount"]) + (int(tx["gasprice"]) * TX_COST) for tx in account["send"])

    if debug:
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
    accounts[str_pub] = set_account(bytes.hex(priv), 0, [], [])
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
    # append transactions into accounts data structure
    if success:
        accounts[to]["recv"].append(set_recv(bytes.hex(tx_gen.publicK), amount, gas_price))
        accounts[frm]["send"].append(set_send(to, amount, gas_price))
        return True
    return False


def validate_nonce(wallet_api, accounts, acc):
    print(f"checking nonce for {acc}")
    out = wallet_api.get_nonce(acc)
    if str(accounts[acc]['nonce']) in out:
        return True

    return False


def validate_acc_amount(wallet_api, accounts, acc, init_amount=0):
    val_fmt = "{{'value': '{}'}}"
    out = wallet_api.get_balance(acc)
    print(f"balance response={out}")
    balance = init_amount + expected_balance(accounts[acc], acc)
    if val_fmt.format(str(balance)) in out:
        print(f"{acc} balance ok ({out})")
        return True

    print(f"balance did not match: returned balance={out}, expected={balance}, init amount={init_amount}")
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
#         "send": [{to: ..., amount: ..., gasprice: .}, ...],
#         "recv": [{from: ..., amount: ..., gasprice: ...}, ...],
#     }
def test_transactions(setup_network):
    wallet_api = WalletAPI(setup_network.bootstrap.deployment_id, setup_network.clients.pods)
    tap_init_amount = 10000
    acc = conf.acc_pub
    accounts = {acc: set_tap_acc()}

    # send tx to client via rpc
    test_txs = 10
    for i in range(test_txs):
        balance = tap_init_amount + expected_balance(accounts[acc], acc)
        amount = random.randint(1, int(balance / 10))
        new_acc_pub = new_account(accounts)
        print(f"\ntap nonce before transferring {accounts[acc]['nonce']}")
        assert transfer(wallet_api, accounts, acc, new_acc_pub, amount=amount), "Transfer from tap failed"
        print(f"tap nonce after transferring {accounts[acc]['nonce']}\n")

    print(f"tap account {acc}:\neverything:\n{pprint.pformat(accounts[acc])}")

    ready_pods = 0
    layers_per_epoch = int(testconfig["client"]["args"]["layers-per-epoch"])
    layer_duration = int(testconfig["client"]["args"]["layer-duration-sec"])

    epochs_num = 2
    tts = layer_duration * layers_per_epoch * epochs_num
    tts = layer_duration
    print(f"\nsleeping for {tts} secs ({epochs_num} epochs)")
    time.sleep(tts)

    print("checking tap nonce")
    if validate_nonce(wallet_api, accounts, acc):
        print("nonce ok!")
        for pk in accounts:
            init_amount = 0
            if pk == acc:
                # TODO 1000 should be a const
                init_amount = 10000
                continue

            ass_err = f"account {pk} didn't have the expected balance"
            assert validate_acc_amount(wallet_api, accounts, pk, init_amount), ass_err
            ready_pods += 1
    else:
        assert 0, "tap account does not have the right nonce"

    assert ready_pods == len(accounts), "Not all accounts received sent txs"  # one for 0 counting and one for tap.
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
