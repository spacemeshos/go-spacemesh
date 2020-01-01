import argparse
from pprint import pprint
import random

from tests.tx_generator import actions
from tests.tx_generator import config as conf
from tests.tx_generator import k8s_handler
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.tx_generator.models.accountant import Accountant


def set_parser():
    parser = argparse.ArgumentParser(description='This is a transaction generator program')
    parser.add_argument('--namespace', dest="namespace",
                        help='namespace to interact with', required=True)
    parser.add_argument('--tx_num', dest="tx_num", default=10, type=int,
                        help='number of transactions', required=False)
    parser.add_argument('--dst', dest="dst",
                        help='destination account to send coins to, a 64 character hex representation', required=False)
    parser.add_argument('--src', default=conf.acc_pub, dest="src",
                        help='source account to send coins from, a 64 character hex representation', required=False)
    parser.add_argument('--priv', dest='priv', default=conf.acc_priv,
                        help='the sending account\'s private key', required=False)

    return parser.parse_args()


if __name__ == "__main__":
    k8s_handler.load_config()
    # execute only if run as a script
    # parse cmd arguments
    args_parsed = set_parser()
    print(f"\n\nreceived args:\n{pprint(args_parsed)}")
    namespace = args_parsed.namespace
    tx_num = args_parsed.tx_num

    # get a list of active pods
    pods_lst = k8s_handler.get_clients_names_and_ips(namespace)
    print(f"\n\n#@! pods:\n{pods_lst}\n\n")

    wallet_api = WalletAPI(namespace, pods_lst)
    accountant = Accountant({conf.acc_pub: Accountant.set_tap_acc()})

    tap_acc = conf.acc_pub
    balance = accountant.get_balance(tap_acc)
    amount = random.randint(1, int(balance / 10))
    new_acc_pub = accountant.new_account()
    print(f"\ntap nonce before transferring {accountant.get_nonce(tap_acc)}")
    if actions.transfer(wallet_api, accountant, tap_acc, new_acc_pub, amount=amount):
        print("Transfer from tap failed!!")

    print("transfer succeed")
    print(f"tap nonce after transferring {accountant.get_nonce(tap_acc)}\n")
