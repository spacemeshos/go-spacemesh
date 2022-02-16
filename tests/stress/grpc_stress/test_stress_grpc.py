from itertools import cycle, islice
from pytest_testconfig import config as testconfig
import multiprocessing as mp
import random

from tests.convenience import sleep_print_backwards, print_hits_entry_count
from tests.queries import find_error_log_msgs
from tests.setup_network import setup_network
from tests.tx_generator import actions
from tests.tx_generator import config as conf
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.tx_generator.models.accountant import Accountant


def test_grpc_stress(init_session, setup_network):
    # use the same node to send all api calls (-1)
    wallet_api = WalletAPI(init_session, setup_network.clients.pods, -1)
    tap_balance = wallet_api.get_balance_value(conf.acc_pub)
    accountant = Accountant({conf.acc_pub: Accountant.set_tap_acc(balance=tap_balance)}, tap_init_amount=tap_balance)
    api_funcs_lst = [wallet_api.get_balance_value, wallet_api.get_nonce_value, wallet_api.get_tx_by_id,
                     actions.transfer]

    # create #new_accounts accounts, this will also be the number of
    # concurrent api calls
    new_accounts = 50
    amount = 50
    print(f"\n\n----- create {new_accounts} new accounts with {amount} coins for each -----")
    actions.send_coins_to_new_accounts(wallet_api, new_accounts, amount, accountant)

    layer_duration = int(testconfig["client"]["args"]["layer-duration-sec"])
    tts = layer_duration * conf.num_layers_until_process
    sleep_print_backwards(tts)

    processes = []
    queue = mp.Queue()
    proc_counter = 0
    for func in islice(cycle(api_funcs_lst), 0,  new_accounts):
        # get acc pub
        acc_pub = list(accountant.accounts.keys())[proc_counter]
        # this will be the args for wallet_api.get_balance_value and wallet_api.get_nonce_value
        args = (acc_pub, )
        if func == actions.transfer:
            dst = random.choice(list(accountant.accounts.keys()))
            balance = wallet_api.get_balance_value(acc_pub)
            amount = random.randint(1, balance - accountant.tx_cost)
            gas_price = 1
            args = (wallet_api, acc_pub, dst, amount, gas_price, None, None, accountant, None, queue)
        elif func == wallet_api.get_tx_by_id:
            args = (random.choice(wallet_api.tx_ids), )

        proc_counter += 1
        processes.append(mp.Process(target=func, args=args))

    actions.run_processes(processes, accountant, queue)

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
