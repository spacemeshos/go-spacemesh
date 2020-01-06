import argparse
import math
import multiprocessing as mp
import os
import pprint
import random
import sys
import time
# this hack is for importing packages located above
# this file and it's imports files location
dir_path = os.getcwd()
dir_path = '/'.join(dir_path.split('/')[0:-2])
print(f"adding {dir_path} to sys.path")
sys.path.insert(0, dir_path)

from tests.tx_generator import actions
from tests.tx_generator import config as conf
from tests.tx_generator import k8s_handler
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.tx_generator.models.accountant import Accountant

TX_NUM = 1
GAS_PRICE = 1


def set_parser():
    parser = argparse.ArgumentParser(description='This is a transaction generator program')
    parser.add_argument('--namespace', dest="namespace",
                        help='namespace to interact with', required=True)
    parser.add_argument('--tx_num', dest="tx_num", default=TX_NUM, type=int,
                        help='number of transactions', required=False)
    parser.add_argument('--dst', dest="dst",
                        help='destination account to send coins to, a 64 character hex representation', required=False)
    parser.add_argument('--src', default=conf.acc_pub, dest="src",
                        help='source account to send coins from, a 64 character hex representation', required=False)
    parser.add_argument('--amount', dest='amount', type=int,
                        help='the sending account\'s private key', required=True)
    parser.add_argument('--priv', dest='priv', default=conf.acc_priv,
                        help='the sending account\'s private key', required=False)
    parser.add_argument('--gas_price', dest='gas_price', default=GAS_PRICE, type=int,
                        help='the sending account\'s private key', required=False)
    parser.add_argument('--gas_limit', dest='gas_limit', default=None, type=int,
                        help='gas limit', required=False)
    parser.add_argument('--nonce', dest='nonce', type=int,
                        help='nonce to add to the tx', required=False)
    parser.add_argument('--layer_duration', dest='layer_duration', default=70, type=int,
                        help='duration of each layer', required=False)
    parser.add_argument('--layers_per_epoch', dest='layers_per_epoch', default=4, type=int,
                        help='how many layers are there in one epoch', required=False)

    return parser.parse_args()


def untracked_transfer(wallet_api, args):
    # get the nonce of the sending account
    curr_nonce = args.nonce if args.nonce else wallet_api.get_nonce_value(args.src)

    if not curr_nonce:
        print("could not retrieve nonce:", curr_nonce)
        exit(1)

    print(f"current nonce={curr_nonce}")
    # get the balance of the src account
    balance = wallet_api.get_balance_value(args.src)
    # if an amount wasn't given rand one in the allowed range
    amount = args.amount if args.amount else random.randint(1, int(int(balance) / args.tx_num))
    if amount + parsed_args.gas_price > int(balance):
        print(f"amount is too high: balance={balance}, amount={amount}, gas-price={parsed_args.gas_price}")
        exit(1)

    # if destination wasn't supplied create a new account to send to
    if not args.dst:
        args.dst, new_account_priv = Accountant.new_untracked_account()

    if actions.transfer(wallet_api, args.src, args.dst, amount,
                        curr_nonce=int(curr_nonce), priv=args.priv):
        print("\ntransfer succeed")

    return amount + parsed_args.gas_price


def send_money_to_new_accounts(args, accountant):
    # create #tx_num new accounts by sending them new txs
    for tx in range(args.tx_num):
        # TODO set a random amount
        dst = args.dst if args.dst else accountant.new_account()

        if actions.transfer(my_wallet, args.src, dst, args.amount, accountant=accountant,
                            gas_price=args.gas_price):
            print("transaction succeeded!\n")
            continue

        print("transaction FAILED!\n")
    

def send_random_txs(accountant, is_new_acc=False):
    """
    send random transactions

    :param accountant: Accountant, a manager for current state
    :param is_new_acc: bool, create new accounts and send money (True) to or use existing (False)
    :return:
    """
    accounts_copy = accountant.accounts.copy()
    for sending_acc_pub, account_val in accounts_copy.items():
        # we might send here tx from and to the same account
        # if we're using the random choice
        dst = accountant.new_account() if is_new_acc else random.choice(list(accountant.accounts.keys()))
        balance = accountant.get_balance(sending_acc_pub)
        if balance < 1:
            print(f"account {sending_acc_pub} does not have sufficient funds to make a tx, balance: {balance}")
            continue

        # amount = random.randint(1, math.ceil(balance / 5))
        amount = 1

        if actions.transfer(my_wallet, sending_acc_pub, dst, amount, accountant=accountant,
                            gas_price=accountant.tx_cost, priv=account_val["priv"]):
            print("transaction succeeded!")
            continue

        print("transaction failed!")


def send_random_txs_using_same_pod(accountant, is_new_acc=False):
    my_wallet.fixed_node = -1
    send_random_txs(accountant, is_new_acc)
    my_wallet.fixed_node = None


if __name__ == "__main__":
    """
    This script relays on the fact we have a tap in our cluster
    with the public and private keys that are mentioned in the conf.py
    file.
    
    initially #tx_num number of transactions will be sent and create
    #tx_num new accounts
     
    """

    k8s_handler.load_config()
    # Parse cmd arguments
    parsed_args = set_parser()
    print(f"\nreceived args:\n{parsed_args}\n")

    # Get a list of active pods
    pods_lst = k8s_handler.get_clients_names_and_ips(parsed_args.namespace)
    my_wallet = WalletAPI(parsed_args.namespace, pods_lst)
    # Get TAP initial values
    tap_nonce = my_wallet.get_nonce_value(conf.acc_pub)
    tap_balance = my_wallet.get_balance_value(conf.acc_pub)
    # Create an accountant to follow state
    acc = Accountant({conf.acc_pub: Accountant.set_tap_acc(balance=tap_balance, nonce=tap_nonce)})

    send_money_to_new_accounts(parsed_args, acc)
    print("\n\n", pprint.pformat(acc.accounts), "\n\n")

    tts = parsed_args.layer_duration * parsed_args.layers_per_epoch
    print("sleeping for an epoch to accept new state")
    time.sleep(tts)

    send_random_txs(acc)
    print("\n\n", pprint.pformat(acc.accounts), "\n\n")

    print("\n\n#@! SENDING using THE SAME CLIENT\n\n")
    send_random_txs_using_same_pod(acc)
    print("\n\n", pprint.pformat(acc.accounts), "\n\n")


# ======================================================================
    # # Setup a list of processes that we want to run
    # processes = []
    # for x in range(14, 14 + parsed_args.tx_num):
    #     args_copy = argparse.Namespace(**vars(parsed_args))
    #     args_copy.nonce = x
    #     processes.append(mp.Process(target=transfer, args=(my_wallet, args_copy)))
    #
    # # processes = [mp.Process(target=transfer, args=(my_wallet, parsed_args)) for x in range(parsed_args.tx_num)]
    #
    # # Run processes
    # for p in processes:
    #     p.start()
    #
    # # Exit the completed processes
    # for p in processes:
    #     p.join()
    # ======================================================================
    # pool = mp.Pool(processes=1)
    #
    # results = []
    # for x in range(10, 10 + parsed_args.tx_num):
    #     args_copy = argparse.Namespace(**vars(parsed_args))
    #     args_copy.nonce = x
    #     pool.apply_async(transfer, args=(my_wallet, args_copy))
    #
    # time.sleep(180)
    # print(results)
