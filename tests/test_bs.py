from pytest_testconfig import config as testconfig

from tests import analyse, queries
from tests.assertions.mesh_assertion import assert_layer_hash
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
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    layer_duration = int(testconfig['client']['args']['layer-duration-sec'])

    dep_info, api_handler = setup_network
    api_handler.wait_for_layer(tx_gen_conf.num_layers_until_process)
    wallet_api = WalletAPI(namespace, dep_info.clients.pods)
    tap_bal = wallet_api.get_balance_value(tx_gen_conf.acc_pub)
    tap_nonce = wallet_api.get_nonce_value(tx_gen_conf.acc_pub)
    tap_pub = tx_gen_conf.acc_pub
    acc = Accountant({tap_pub: Accountant.set_tap_acc(balance=tap_bal, nonce=tap_nonce)}, tap_init_amount=tap_bal)

    print("\n\n----- create new accounts ------")
    new_acc_num = 10
    amount = 50
    print("assert that we can send coin tx to new accounts")
    ass_err = "error sending coin transactions to new accounts"
    assert actions.send_coins_to_new_accounts(wallet_api, new_acc_num, amount, acc), ass_err

    print("assert tap's nonce and balance")
    ass_err = "tap did not have the matching nonce"
    assert actions.validate_nonce(wallet_api, acc, tx_gen_conf.acc_pub), ass_err
    ass_err = "tap did not have the matching balance"
    assert actions.validate_acc_amount(wallet_api, acc, tx_gen_conf.acc_pub), ass_err

    # wait for 2 genesis epochs that will not contain any blocks + one layer for tx execution
    tts = layer_duration * layers_per_epoch * 2 + 1
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
    dep_info, api_handler = setup_network
    current_index = get_curr_ind()
    ns = init_session
    layer_avg_size = testconfig['client']['args']['layer-average-size']
    layers_duration = int(testconfig['client']['args']['layer-duration-sec'])
    layers_per_epoch = int(testconfig['client']['args']['layers-per-epoch'])
    # check only third epoch
    epochs = 5
    last_layer = epochs * layers_per_epoch
    current_layer = api_handler.get_current_layer()
    total_pods = len(dep_info.clients.pods) + len(dep_info.bootstrap.pods)
    if current_layer < last_layer:
        # TODO: timeout margin variable in conf
        api_handler.wait_for_layer(last_layer, timeout=layers_duration * last_layer + 10)
    while current_layer % layers_per_epoch != 0:
        print("current layer is not the first layer of the epoch waiting another layer, curr:", current_layer)
        current_layer += 1
        api_handler.wait_for_layer(current_layer, timeout=layers_duration + 10)

    tts = 50
    sleep_print_backwards(tts)

    analyse.analyze_mining(testconfig['namespace'], current_layer, layers_per_epoch, layer_avg_size, total_pods)
    assert_layer_hash(api_handler, last_layer)
    queries.assert_equal_state_roots(current_index, ns)
    queries.assert_no_contextually_invalid_atxs(current_index, ns)
    validate_hare(current_index, ns)  # validate hare
