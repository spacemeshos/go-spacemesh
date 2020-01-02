import argparse
import os
import random
import sys
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
    parser.add_argument('--priv', dest='priv', default=conf.acc_priv,
                        help='the sending account\'s private key', required=False)
    parser.add_argument('--amount', dest='amount', type=int,
                        help='the sending account\'s private key', required=False)
    parser.add_argument('--gas_price', dest='gas_price', default=GAS_PRICE, type=int,
                        help='the sending account\'s private key', required=False)

    return parser.parse_args()


def transfer(wallet_api, all_args):
    # get the nonce of the sending account
    curr_nonce = wallet_api.get_nonce_value(all_args.src)

    if not curr_nonce:
        print("could not retrieve nonce:", curr_nonce)
        exit(1)

    print(f"current nonce={curr_nonce}")
    # get the balance of the src account
    balance = wallet_api.get_balance_value(all_args.src)
    # if an amount wasn't given rand one in the allowed range
    amount = all_args.amount if all_args.amount else random.randint(1, int(int(balance) / all_args.tx_num))
    if amount + parsed_args.gas_price > int(balance):
        print(f"amount is too high: balance={balance}, amount={amount}, gas-price={parsed_args.gas_price}")
        exit(1)

    # if destination wasn't supplied create a new account to send to
    # all_args.priv has a default value
    if not all_args.dst or not all_args.priv:
        all_args.dst, new_account_priv = Accountant.new_untracked_account()

    if actions.transfer(wallet_api, all_args.src, all_args.dst, amount,
                        curr_nonce=int(curr_nonce), priv=all_args.priv):
        print("\ntransfer succeed")

    return amount + parsed_args.gas_price


if __name__ == "__main__":
    k8s_handler.load_config()
    # execute only if run as a script
    # parse cmd arguments
    parsed_args = set_parser()

    print(f"\nreceived args:\n{parsed_args}\n")

    # get a list of active pods
    pods_lst = k8s_handler.get_clients_names_and_ips(parsed_args.namespace)
    my_wallet = WalletAPI(parsed_args.namespace, pods_lst)
    total = []
    for tx in range(parsed_args.tx_num):
        total.append(transfer(my_wallet, parsed_args))
