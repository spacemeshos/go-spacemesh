import argparse
import os
import sys
# this hack is for importing packages located above
# this file and it's imports files location
dir_path = os.getcwd()
print(f"adding {dir_path} to sys.path")
sys.path.insert(0, dir_path)

from tests.tx_generator import actions
from tests.tx_generator import config as conf
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.tx_generator.models.accountant import Accountant


def set_parser():
    parser = argparse.ArgumentParser(description='This is a transactions generator program',
                                     usage='%(prog)s [-h]',
                                     formatter_class=argparse.ArgumentDefaultsHelpFormatter)
    parser.add_argument('-p', '--pod_ip', dest="pod_ip", metavar='',
                        help='namespace to interact with', required=True)
    parser.add_argument('-t', '--tx_count', dest="tx_count", default=100, type=int, metavar='',
                        help='number of new accounts to create by sending coins from tap', required=False)
    parser.add_argument('-gp', '--gas_price', dest='gas_price', default=1, type=int, metavar='',
                        help=f'the sending account\'s private key', required=False)

    return parser.parse_args()


if __name__ == "__main__":
    """
    This script relays on the fact that we have a tap in our cluster
    with the public and private keys that are mentioned in the conf.py
    file.
    
    initially #new_accounts number of transactions will be sent and create
    #new_accounts new accounts
    
    Sleep for 4 layers, until the state is updated and new accounts created
    
    send #tx_num transactions using different account for each.
    in case of concurrency, after every full iteration where all accounts were involved, 
    run all processes at once using multiprocessing and continue accumulating 
    more txs for the next run.
    
    """

    # Parse cmd arguments
    parsed_args = set_parser()
    print(f"\nreceived args:\n{parsed_args}\n")
    tx_count = parsed_args.tx_count
    amount = 1

    pod_ip = parsed_args.pod_ip
    pod_lst = [{"pod_ip": pod_ip, "name": "AWS_GATEWAY"}]

    my_wallet = WalletAPI(None, pod_lst)
    # Get TAP initial values
    tap_nonce = my_wallet.get_nonce_value(conf.acc_pub)
    tap_balance = my_wallet.get_balance_value(conf.acc_pub)

    if not tap_nonce or not tap_balance:
        print(f"could not resolve nonce/balance, nonce={tap_nonce}, balance={tap_balance}")

    # Create an accountant to follow state
    print("#@!#@!#@#@!#!@")
    print(f"nonce={tap_nonce}")
    print(f"nonce type={type(tap_nonce)}")
    print(f"balance={tap_balance}")
    print(f"balance type={type(tap_balance)}")
    tap_acc = Accountant.set_tap_acc(balance=tap_balance, nonce=tap_nonce)
    print(f"tap_acc={tap_acc}")
    print("#@!#@!#@#@!#!@")
    acc = Accountant({conf.acc_pub: tap_acc}, tap_init_amount=tap_balance)
    acc.tx_cost = parsed_args.gas_price
    # Create new accounts by sending them coins
    actions.send_coins_to_new_accounts(my_wallet, tx_count, amount, acc, parsed_args.gas_price)
