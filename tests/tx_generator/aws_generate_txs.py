import argparse
import os
import sys
import time
# this hack is for importing packages located above
# this file and it's imports files location
dir_path = os.getcwd()
print(f"adding {dir_path} to sys.path")
sys.path.insert(0, dir_path)

from tests.convenience import str2bool
from tests.tx_generator import actions
from tests.tx_generator import config as conf
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.tx_generator.models.accountant import Accountant


def set_parser():
    parser = argparse.ArgumentParser(description='This is a transactions generator program',
                                     usage='%(prog)s [-h]',
                                     formatter_class=argparse.ArgumentDefaultsHelpFormatter)
    parser.add_argument('-p', '--pod_ip', dest="pod_ip", metavar='',
                        help='miner ip to interact with', required=True)
    parser.add_argument('-n', '--new_accounts', dest="new_accounts", default=100, type=int, metavar='',
                        help='number of new accounts to be created by sending coins from tap', required=False)
    parser.add_argument('-t', '--tx_count', dest="tx_count", default=0, type=int, metavar='',
                        help='number of txs sent between newly created accounts', required=False)
    parser.add_argument('-gp', '--gas_price', dest='gas_price', default=1, type=int, metavar='',
                        help='the sending account\'s private key', required=False)
    parser.add_argument('-ld', '--layer_duration', dest='layer_duration', default=300, type=int, metavar='',
                        help='duration of each layer', required=False)
    parser.add_argument('-w', '--wait_layers', dest='layer_wait', default=6, type=int, metavar='',
                        help='layers to wait until new state is processed', required=False)
    parser.add_argument('-c', '--concurrent', dest='is_concurrent', type=str2bool, nargs='?', default=False, metavar='',
                        help='send transactions concurrently', required=False)

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

    # Parse arguments
    parsed_args = set_parser()
    print(f"\narguments received:\n{parsed_args}\n")

    new_acc_num = parsed_args.new_accounts
    tx_count = parsed_args.tx_count
    layer_duration = parsed_args.layer_duration
    layer_wait = parsed_args.layer_wait
    is_concurrent = parsed_args.is_concurrent
    amount = 100

    pod_ip = parsed_args.pod_ip
    pod_lst = [{"pod_ip": pod_ip, "name": "AWS_GATEWAY"}]

    my_wallet = WalletAPI(None, pod_lst)

    # Get TAP initial values
    tap_nonce = my_wallet.get_nonce_value(conf.acc_pub)
    tap_balance = my_wallet.get_balance_value(conf.acc_pub)
    if not tap_nonce or not tap_balance:
        print(f"could not resolve nonce/balance, nonce={tap_nonce}, balance={tap_balance}")

    # Create an accountant to follow state
    tap_acc = Accountant.set_tap_acc(balance=tap_balance, nonce=tap_nonce)
    acc = Accountant({conf.acc_pub: tap_acc}, tap_init_amount=tap_balance)
    acc.tx_cost = parsed_args.gas_price

    # Create new accounts by sending them coins
    actions.send_coins_to_new_accounts(my_wallet, new_acc_num, amount, acc, parsed_args.gas_price)

    if tx_count:
        tts = layer_wait * layer_duration
        print(f"sleeping for {tts} to enable new state to be processed")
        time.sleep(tts)
        actions.send_tx_from_each_account(my_wallet, acc, tx_count, is_concurrent=is_concurrent)
