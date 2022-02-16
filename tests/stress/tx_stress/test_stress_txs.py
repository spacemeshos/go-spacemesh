from pytest_testconfig import config as testconfig
import multiprocessing as mp

from tests.convenience import sleep_print_backwards
from tests.setup_network import setup_network
from tests.tx_generator import actions
from tests.tx_generator import config as conf
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.tx_generator.models.accountant import Accountant
from tests.utils import wait_for_next_layer


# send 100 txs in one layer
def test_tx_stress(init_session, setup_network):
    wallet_api = WalletAPI(init_session, setup_network.clients.pods, fixed_node=-1)
    tap_balance = wallet_api.get_balance_value(conf.acc_pub)
    tap_acc = Accountant.set_tap_acc(balance=tap_balance)
    accountant = Accountant({conf.acc_pub: tap_acc}, tap_init_amount=tap_balance)

    # create 100 accounts
    new_accounts = 100
    amount = 50
    print(f"\n\n----- create {new_accounts} new accounts with {amount} coins for each -----")
    actions.send_coins_to_new_accounts(wallet_api, new_accounts, amount, accountant)

    layer_duration = int(testconfig["client"]["args"]["layer-duration-sec"])
    tts = layer_duration * conf.num_layers_until_process
    sleep_print_backwards(tts)

    print("\n\n----- wait for next layer to start -----")
    cl_num = len(setup_network.clients.pods) + 1  # adding 1 for bootstrap
    wait_for_next_layer(init_session, cl_num, layer_duration)

    tx_num = new_accounts
    print(f"\n\n----- send {tx_num} new transactions during the current layer -----")
    actions.send_tx_from_each_account(wallet_api, accountant, tx_num, is_concurrent=True)

    tts = layer_duration * conf.num_layers_until_process
    sleep_print_backwards(tts)

    print(f"\n\n----- validate all accounts nonce and balance -----")
    for acc_pub in accountant.accounts:
        ass_err = f"account {acc_pub} did not have the matching balance"
        assert actions.validate_acc_amount(wallet_api, accountant, acc_pub), ass_err

    for acc_pub in accountant.accounts:
        ass_err = f"account {acc_pub} did not have the matching nonce"
        assert actions.validate_nonce(wallet_api, accountant, acc_pub), ass_err
