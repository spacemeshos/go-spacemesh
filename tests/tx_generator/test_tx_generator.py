from pytest_testconfig import config as testconfig
import pprint
import random
import time

from tests.tx_generator.actions import validate_nonce, transfer, validate_acc_amount
from tests.tx_generator.models.accountant import Accountant
from tests.test_bs import setup_network, add_curl, setup_bootstrap, start_poet, setup_clients, wait_genesis
import tests.tx_generator.config as conf
from tests.tx_generator.models.wallet_api import WalletAPI


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

    # validate that each account has the expected amount
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
