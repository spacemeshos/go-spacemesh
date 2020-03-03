from pytest_testconfig import config as testconfig

from tests import analyse, queries
from tests.convenience import sleep_print_backwards
from tests.hare.assert_hare import validate_hare
from tests.setup_network import setup_network
from tests.tx_generator import config as tx_gen_conf
import tests.tx_generator.actions as actions
from tests.tx_generator.models.accountant import Accountant
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.utils import get_curr_ind


def test_transactions(init_session, setup_network):
    # create #new_acc_num new accounts by sending them coins from tap
    # check tap balance/nonce
    # sleep until new state is processed
    # send txs from new accounts and create new accounts
    # sleep until new state is processes
    # validate all accounts balance/nonce
    # send txs from all accounts between themselves
    # validate all accounts balance/nonce

    namespace = init_session
    wallet_api = WalletAPI(namespace, setup_network.clients.pods)

    tap_bal = wallet_api.get_balance_value(tx_gen_conf.acc_pub)
    tap_nonce = wallet_api.get_nonce_value(tx_gen_conf.acc_pub)
    tap_pub = tx_gen_conf.acc_pub
    acc = Accountant({tap_pub: Accountant.set_tap_acc(balance=tap_bal, nonce=tap_nonce)}, tap_init_amount=tap_bal)

    print("\n\n----- create new accounts ------")
    new_acc_num = 10
    amount = 50
    actions.send_coins_to_new_accounts(wallet_api, new_acc_num, amount, acc)

    print("assert tap's nonce and balance")
    ass_err = "tap did not have the matching nonce"
    assert actions.validate_nonce(wallet_api, acc, tx_gen_conf.acc_pub), ass_err
    ass_err = "tap did not have the matching balance"
    assert actions.validate_acc_amount(wallet_api, acc, tx_gen_conf.acc_pub), ass_err

    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])
    tts = layer_duration * tx_gen_conf.num_layers_until_process
    sleep_print_backwards(tts)

    print("\n\n------ create new accounts using the accounts created by tap ------")
    # add 1 because we have #new_acc_num new accounts and one tap
    tx_num = new_acc_num + 1
    amount = 5
    actions.send_tx_from_each_account(wallet_api, acc, tx_num, is_new_acc=True, amount=amount)

    tts = layer_duration * tx_gen_conf.num_layers_until_process
    sleep_print_backwards(tts)

    for acc_pub in acc.accounts:
        ass_err = f"account {acc_pub} did not have the matching balance"
        assert actions.validate_acc_amount(wallet_api, acc, acc_pub), ass_err

    for acc_pub in acc.accounts:
        ass_err = f"account {acc_pub} did not have the matching nonce"
        assert actions.validate_nonce(wallet_api, acc, acc_pub), ass_err

    print("\n\n------ send txs between all accounts ------")
    # send coins from all accounts between themselves (add 1 for tap)
    tx_num = new_acc_num * 2 + 1
    actions.send_tx_from_each_account(wallet_api, acc, tx_num)

    tts = layer_duration * tx_gen_conf.num_layers_until_process
    sleep_print_backwards(tts)

    for acc_pub in acc.accounts:
        ass_err = f"account {acc_pub} did not have the matching balance"
        assert actions.validate_acc_amount(wallet_api, acc, acc_pub), ass_err

    for acc_pub in acc.accounts:
        ass_err = f"account {acc_pub} did not have the matching nonce"
        assert actions.validate_nonce(wallet_api, acc, acc_pub), ass_err


def test_mining(init_session, setup_network):
    current_index = get_curr_ind()
    ns = init_session
    layer_avg_size = testconfig['client']['args']['layer-average-size']
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    # check only third epoch
    epochs = 5
    last_layer = epochs * layers_per_epoch

    layer_reached = queries.wait_for_latest_layer(testconfig["namespace"], last_layer, layers_per_epoch)

    total_pods = len(setup_network.clients.pods) + len(setup_network.bootstrap.pods)

    tts = 50
    sleep_print_backwards(tts)

    analyse.analyze_mining(testconfig['namespace'], layer_reached, layers_per_epoch, layer_avg_size, total_pods)

    queries.assert_equal_layer_hashes(current_index, ns)
    queries.assert_equal_state_roots(current_index, ns)
    queries.assert_no_contextually_invalid_atxs(current_index, ns)
    validate_hare(current_index, ns)  # validate hare
