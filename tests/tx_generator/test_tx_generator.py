from pytest_testconfig import config as testconfig
import pprint
import random
import time

from tests.tx_generator.models.accountant import Accountant
from tests.test_bs import setup_network, add_curl, setup_bootstrap, start_poet, setup_clients, wait_genesis
import tests.tx_generator.config as conf
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.tx_generator.models.tx_generator import TxGenerator


def transfer(wallet_api, accountant, frm, to, amount=None, gas_price=None, gas_limit=None):
    tx_gen = TxGenerator(pub=frm, pri=accountant.get_acc_priv(frm))
    if not amount:
        amount = random.randint(1, accountant.get_balance(frm) - accountant.tx_cost)
    if not gas_price:
        gas_price = 1
    if not gas_limit:
        gas_limit = gas_price + 1

    # create transaction
    tx_bytes = tx_gen.generate(to, accountant.get_nonce(frm), gas_limit, gas_price, amount)
    # submit transaction
    success = wallet_api.submit_tx(to, frm, gas_price, amount, tx_bytes)
    accountant.set_nonce(frm)
    # append transactions into accounts data structure
    if success:
        accountant.set_acc_recv(to, Accountant.set_recv(bytes.hex(tx_gen.publicK), amount, gas_price))
        accountant.set_acc_send(frm, Accountant.set_send(to, amount, gas_price))
        return True

    return False


def validate_nonce(wallet_api, acc, nonce):
    print(f"\nchecking nonce for {acc}")
    out = wallet_api.get_nonce(acc)
    if str(nonce) in out:
        return True

    return False


def validate_acc_amount(wallet_api, accountant, acc):
    print(f"\nvalidate balance for {acc}")
    if acc == conf.acc_pub:
        print("TAP")

    res_fmt = "{{'value': '{}'}}"
    out = wallet_api.get_balance(acc)
    balance = accountant.get_balance(acc)

    if res_fmt.format(str(balance)) in out:
        print(f"balance ok (origin={balance})")
        return True

    print(f"balance did not match: returned balance={out}, expected={balance}")
    return False


def test_transactions(setup_network):
    debug = False
    wallet_api = WalletAPI(setup_network.bootstrap.deployment_id, setup_network.clients.pods)
    acc = conf.acc_pub
    accountant = Accountant({acc: Accountant.set_tap_acc()})

    # send txs via miners
    test_txs = 10
    for i in range(test_txs):
        balance = accountant.get_balance(acc)
        amount = random.randint(1, int(balance / 10))
        new_acc_pub = accountant.new_account()
        print(f"\ntap nonce before transferring {accountant.get_nonce(acc)}")
        assert transfer(wallet_api, accountant, acc, new_acc_pub, amount=amount), "Transfer from tap failed"
        print(f"tap nonce after transferring {accountant.get_nonce(acc)}\n")

    print(f"TAP account {acc}:\n\n{pprint.pformat(accountant.get_acc(acc))}")

    layers_per_epoch = int(testconfig["client"]["args"]["layers-per-epoch"])
    layer_duration = int(testconfig["client"]["args"]["layer-duration-sec"])

    print("checking tap nonce")
    is_valid = validate_nonce(wallet_api, acc, accountant.get_nonce(acc))
    assert is_valid, "tap account does not have the right nonce"
    print("nonce ok!")

    ready_pods = 0
    epochs_sleep_limit = 2
    for x in range(layers_per_epoch * epochs_sleep_limit):
        ready_pods = 0
        print(f"\n\nsleeping for a layer ({layer_duration} seconds)... {x+1}\n\n")
        time.sleep(layer_duration)

        for pk in accountant.accounts:
            if validate_acc_amount(wallet_api, accountant, pk):
                ready_pods += 1
                continue

            print(f"account with {pk} is not ready yet")
            break

        if ready_pods == len(accountant.accounts):
            print("\nall accounts got the expected amount\n")
            break

    if debug:
        print(f"tap account {acc}:\n\n{pprint.pformat(accountant.accounts)}\n")

    assert ready_pods == len(accountant.accounts), "Not all accounts received sent txs"

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
