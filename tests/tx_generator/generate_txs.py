import argparse
import multiprocessing as mp
import os
import pprint
import random
import sys
# this hack is for importing packages located above
# this file and it's imports files location
dir_path = os.getcwd()
dir_path = '/'.join(dir_path.split('/')[0:-2])
print(f"adding {dir_path} to sys.path")
sys.path.insert(0, dir_path)

from tests.convenience import sleep_print_backwards
from tests.tx_generator import actions
from tests.tx_generator import config as conf
from tests.tx_generator import k8s_handler
from tests.tx_generator.models.wallet_api import WalletAPI
from tests.tx_generator.models.accountant import Accountant

GAS_PRICE = 1
NEW_ACCOUNTS = 20
TX_NUM = NEW_ACCOUNTS


def str2bool(v):
    if isinstance(v, bool):
        return v
    if v.lower() in ('yes', 'true', 't', 'y', '1'):
        return True
    elif v.lower() in ('no', 'false', 'f', 'n', '0'):
        return False
    else:
        raise argparse.ArgumentTypeError('Boolean value expected.')


def set_parser():
    parser = argparse.ArgumentParser(description='This is a transactions generator program',
                                     usage='%(prog)s [-h]',
                                     formatter_class=argparse.ArgumentDefaultsHelpFormatter)
    parser.add_argument('-ns', '--namespace', dest="namespace", metavar='',
                        help='namespace to interact with', required=True)
    parser.add_argument('-na', '--new_accounts', dest="new_accounts", default=NEW_ACCOUNTS, type=int, metavar='',
                        help='number of new accounts to create by sending coins from tap', required=False)
    parser.add_argument('-t', '--tx_num', dest="tx_num", default=TX_NUM, type=int, metavar='',
                        help='number of transactions for each node to make', required=False)
    parser.add_argument('-src', '--source', default=conf.acc_pub, dest="src", metavar='',
                        help='source account to send coins from, a 64 character hex representation', required=False)
    parser.add_argument('-dst', '--destination', dest="dst", metavar='',
                        help='destination account to send coins to, a 64 character hex', required=False)
    parser.add_argument('-a', '--amount', dest='amount', type=int, metavar='',
                        help='the amount to send to each new account', required=True)
    parser.add_argument('-p', '--priv', dest='priv', default=conf.acc_priv, metavar='',
                        help='the sending account\'s private key', required=False)
    parser.add_argument('-gp', '--gas_price', dest='gas_price', default=GAS_PRICE, type=int, metavar='',
                        help=f'the sending account\'s private key', required=False)
    parser.add_argument('-gl', '--gas_limit', dest='gas_limit', default=None, type=int, metavar='',
                        help='gas limit', required=False)
    parser.add_argument('-n', '--nonce', dest='nonce', type=int, metavar='',
                        help='tap nonce to add to the first tx', required=False)
    parser.add_argument('-sp', '--same_pod', dest='same_pod', default=False, type=str2bool, nargs='?', metavar='',
                        help='use the same pod for all api calls', required=False)
    parser.add_argument('-c', '--concurrent', dest='is_concurrent', type=str2bool, nargs='?', default=False, metavar='',
                        help='send transactions concurrently', required=False)
    parser.add_argument('-ld', '--layer_duration', dest='layer_duration', default=70, type=int, metavar='',
                        help='duration of each layer', required=False)
    parser.add_argument('-lp', '--layers_per_epoch', dest='layers_per_epoch', default=4, type=int,
                        metavar='',
                        help='how many layers are there in one epoch', required=False)

    return parser.parse_args()


def run():
    menu = "sp - fixed node\n" \
           "nsp - random node\n" \
           "c - concurrent\n" \
           "nc - cancel concurrency\n" \
           "t - tx number\n" \
           "txs - print all txs ids\n" \
           "tbi - tx by id\n" \
           "p - print state\n" \
           "go to start, q to quit: "
    print(menu)
    inp = 0
    while inp != 'q':
        inp = input()
        if inp == 'q':
            break

        if inp == 'p':
            print(f"\n\n{pprint.pformat(acc.accounts)}\n\n")
            print(menu)
        if inp == "sp":
            print("using same pod for all GRPC api calls")
            my_wallet.fixed_node = -1
        elif inp == "nsp":
            print("using random pods GRPC api calls")
            my_wallet.fixed_node = None
        elif inp == "c":
            print("multiprocessing mode")
            parsed_args.is_concurrent = True
        elif inp == "nc":
            print("canceled multiprocessing mode")
            parsed_args.is_concurrent = False
        elif inp == "t":
            parsed_args.tx_num = int(input("how many txs: "))
        elif inp == "txs":
            print(my_wallet.tx_ids)
        elif inp == "tbi":
            inp = input("enter id: ")
            print(my_wallet.get_tx_by_id(inp))
        elif inp == "go":
            actions.send_tx_from_each_account(my_wallet, acc, parsed_args.tx_num,
                                              is_concurrent=parsed_args.is_concurrent)
            print(menu, end="")
        else:
            print("unknown input")


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
    acc.tx_cost = parsed_args.gas_price
    # Create new accounts by sending them coins
    actions.send_coins_to_new_accounts(my_wallet, parsed_args.new_accounts, parsed_args.amount, acc,
                                       parsed_args.gas_price)

    print(f"\n\n{pprint.pformat(acc.accounts)}\n\n")

    # Sleep for 4 layers to let new txs enter the state and new accounts to be valid
    tts = parsed_args.layer_duration * conf.num_layers_until_process
    sleep_print_backwards(tts)

    # Use same pod for every wallet api call
    if parsed_args.same_pod:
        print("Using the same pod for all txs (and nonce querying)")
        my_wallet.fixed_node = -1

    run()
    print("bye bye!")
