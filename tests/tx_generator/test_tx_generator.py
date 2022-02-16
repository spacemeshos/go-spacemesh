from pytest_testconfig import config as testconfig
import pprint
import random
import time

from tests.setup_network import setup_network
from tests.tx_generator import actions
from tests.tx_generator.models.accountant import Accountant
import tests.tx_generator.config as conf
from tests.tx_generator.models.wallet_api import WalletAPI


def test_transactions(setup_network):
    debug = False
    wallet_api = WalletAPI(setup_network.bootstrap.deployment_id, setup_network.clients.pods)
    tap_pub = conf.acc_pub
    tap_balance = wallet_api.get_balance_value(tap_pub)
    accountant = Accountant({tap_pub: Accountant.set_tap_acc(balance=tap_balance)}, tap_init_amount=tap_balance)

    # send txs via miners
    test_txs = 10
    for i in range(test_txs):
        balance = accountant.get_balance(tap_pub)
        amount = random.randint(1, int(balance / 10))
        new_acc_pub = accountant.new_account()
        print(f"\ntap nonce before transferring {accountant.get_nonce(tap_pub)}")
        ass_err = "Transfer from tap failed"
        assert actions.transfer(wallet_api, tap_pub, new_acc_pub, amount, accountant=accountant), ass_err
        print(f"tap nonce after transferring {accountant.get_nonce(tap_pub)}\n")

    print(f"TAP account {tap_pub}:\n\n{pprint.pformat(accountant.get_acc(tap_pub))}")

    layers_per_epoch = int(testconfig["client"]["args"]["layers-per-epoch"])
    layer_duration = int(testconfig["client"]["args"]["layer-duration-sec"])

    print("checking tap nonce")
    is_valid = actions.validate_nonce(wallet_api, accountant, tap_pub)
    assert is_valid, "tap account does not have the right nonce"
    print("nonce ok!")

    # validate that each account has the expected amount
    ready_pods = 0
    epochs_sleep_limit = 20
    for x in range(layers_per_epoch * epochs_sleep_limit):
        ready_pods = 0
        print(f"\n\nsleeping for a layer ({layer_duration} seconds)... {x+1}\n\n")
        time.sleep(layer_duration)

        for pk in accountant.accounts:
            if actions.validate_acc_amount(wallet_api, accountant, pk):
                ready_pods += 1
                continue

            print(f"account with {pk} is not ready yet")
            break

        if ready_pods == len(accountant.accounts):
            print("\nall accounts got the expected amount\n")
            break

    if debug:
        print(f"tap account {tap_pub}:\n\n{pprint.pformat(accountant.accounts)}\n")

    assert ready_pods == len(accountant.accounts), "Expected state does not match the current state"
