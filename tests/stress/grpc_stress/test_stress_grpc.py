from pytest_testconfig import config as testconfig
import multiprocessing as mp
import random

from tests.convenience import sleep_print_backwards
from tests.queries import find_error_log_msgs
from tests.test_bs import setup_network, add_curl, setup_bootstrap, start_poet, setup_clients, wait_genesis
from tests.tx_generator import actions
from tests.tx_generator import config as conf
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.tx_generator.models.accountant import Accountant
from tests.utils import print_hits_entry_count


def test_grpc_stress(init_session, setup_network):
    # use the same node to send all api calls (-1)
    wallet_api = WalletAPI(init_session, setup_network.clients.pods, -1)
    accountant = Accountant({conf.acc_pub: Accountant.set_tap_acc()})
    api_funcs_lst = [wallet_api.get_balance_value, wallet_api.get_nonce_value, actions.transfer]

    # create 100 accounts
    new_accounts = 50
    amount = 50
    print(f"\n\n----- create {new_accounts} new accounts with {amount} coins for each -----")
    actions.send_coins_to_new_accounts(wallet_api, new_accounts, amount, accountant)

    layer_duration = int(testconfig["client"]["args"]["layer-duration-sec"])
    tts = layer_duration * conf.num_layers_until_process
    sleep_print_backwards(tts)

    processes = []
    queue = mp.Queue()
    for x in range(new_accounts):
        # get acc pub to send from
        acc_pub = list(accountant.accounts.keys())[x]
        # select one of the api calls to run randomly
        func = random.choice(api_funcs_lst)
        args = (acc_pub, )
        if func == actions.transfer:
            dst = random.choice(list(accountant.accounts.keys()))
            amount = 1
            gas_price = 1
            args = (wallet_api, acc_pub, dst, amount, gas_price, None, None, accountant, None, queue)
        processes.append(mp.Process(target=func, args=args))

    # Run processes
    for p in processes:
        p.start()

    # Exit the completed processes
    for p in processes:
        p.join()

    accountant.set_accountant_from_queue(queue)

    tts = layer_duration * conf.num_layers_until_process
    sleep_print_backwards(tts)

    print(f"\n\n----- validate all accounts nonce and balance -----")
    for acc_pub in accountant.accounts:
        ass_err = f"account {acc_pub} did not have the matching balance"
        assert actions.validate_acc_amount(wallet_api, accountant, acc_pub), ass_err

    for acc_pub in accountant.accounts:
        ass_err = f"account {acc_pub} did not have the matching nonce"
        assert actions.validate_nonce(wallet_api, accountant, acc_pub), ass_err

    errs = find_error_log_msgs(init_session, init_session)
    if errs:
        print_hits_entry_count(errs, "message")
        assert 0, "found log errors"

    assert len(errs) == 0, "found errors in logs"
