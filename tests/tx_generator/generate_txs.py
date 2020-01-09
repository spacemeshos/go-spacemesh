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

GAS_PRICE = 1
NEW_ACCOUNTS = 20
TX_NUM = 1


def set_parser():
    parser = argparse.ArgumentParser(description='This is a transaction generator program',
                                     usage='%(prog)s [-h] [-i INI]',
                                     formatter_class=argparse.ArgumentDefaultsHelpFormatter)
    parser.add_argument('-ns', '--namespace', dest="namespace", metavar='',
                        help='namespace to interact with', required=True)
    parser.add_argument('-na', '--new_accounts', dest="new_accounts", default=NEW_ACCOUNTS, type=int, metavar='',
                        help='number of new accounts to create by sending coins from tap', required=False)
    # parser.add_argument('-t', '--tx_num', dest="tx_num", default=TX_NUM, type=int, metavar='',
    #                     help='number of transactions for each node to make', required=False)
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
    parser.add_argument('-sp', '--same_pod', dest='same_pod', default=False, type=bool, metavar='',
                        help='use the same pod for all api calls', required=False)
    parser.add_argument('-c', '--concurrent', dest='is_concurrent', default=True, type=bool, metavar='',
                        help='send transactions concurrently', required=False)
    parser.add_argument('-ld', '--layer_duration', dest='layer_duration', default=70, type=int, metavar='',
                        help='duration of each layer', required=False)
    parser.add_argument('-lp', '--layers_per_epoch', dest='layers_per_epoch', default=4, type=int,
                        metavar='',
                        help='how many layers are there in one epoch', required=False)

    return parser.parse_args()


def untracked_transfer(wallet_api, args):
    # get the nonce of the sending account
    curr_nonce = wallet_api.get_nonce_value(args.src)

    if not curr_nonce:
        print("could not retrieve nonce:", curr_nonce)
        exit(1)

    print(f"current nonce={curr_nonce}")
    # get the balance of the src account
    balance = wallet_api.get_balance_value(args.src)
    # if an amount wasn't given rand one in the allowed range
    amount = args.amount if args.amount else random.randint(1, int(int(balance) / args.new_accounts))
    if amount + parsed_args.gas_price > int(balance):
        print(f"amount is too high: balance={balance}, amount={amount}, gas-price={parsed_args.gas_price}")
        exit(1)

    # if destination wasn't supplied create a new account to send to
    if not args.dst:
        args.dst, new_account_priv = Accountant.new_untracked_account()

    is_succeed = actions.transfer(wallet_api, args.src, args.dst, amount, curr_nonce=int(curr_nonce), priv=args.priv)
    if is_succeed:
        print("\ntransfer succeed")

    return amount + parsed_args.gas_price, is_succeed


def send_coins_to_new_accounts(args, accountant):
    # create #new_accounts new accounts by sending them new txs
    for tx in range(args.new_accounts):
        # TODO set a random amount
        dst = args.dst if args.dst else accountant.new_account()

        if actions.transfer(my_wallet, args.src, dst, args.amount, accountant=accountant,
                            gas_price=args.gas_price):
            print("transaction succeeded!\n")
            continue

        print("transaction FAILED!\n")


def send_tx_from_each_account(wallet, accountant, is_new_acc=False, is_concurrent=False, queue=None):
    """
    send random transactions iterating over the accounts list

    :param wallet: WalletAPI, a manager for GRPC communication
    :param accountant: Accountant, a manager for current state
    :param is_new_acc: bool, create new accounts and send money to (if True) or use existing (False)
    :param is_concurrent: bool, send transactions concurrently
    :param queue: multiprocess.Queue, a queue for collecting the accountant result

    :return:
    """

    # a list for all transfers to be made concurrently
    processes = []
    # TODO use cycle here #@!
    accounts_copy = accountant.accounts.copy()
    for sending_acc_pub, account_det in accounts_copy.items():
        # we might send here tx from and to the same account
        # if we're using the random choice
        dst = accountant.new_account() if is_new_acc else random.choice(list(accountant.accounts.keys()))
        balance = accountant.get_balance(sending_acc_pub)
        if balance < 1:
            print(f"account {sending_acc_pub} does not have sufficient funds to make a tx, balance: {balance}")
            continue

        # amount = random.randint(1, math.ceil(balance / 5))
        # send the minimum amount so we can flood with valid txs
        amount = 1
        if is_concurrent:
            processes.append(mp.Process(target=actions.transfer, args=(wallet, sending_acc_pub, dst, amount,
                                        accountant.tx_cost, parsed_args.gas_limit, parsed_args.nonce, accountant,
                                        account_det["priv"], queue)))
            continue
        elif actions.transfer(wallet, sending_acc_pub, dst, amount, accountant=accountant, gas_price=accountant.tx_cost,
                              priv=account_det["priv"]):
            print("transaction succeeded!")
            continue

        print("transaction failed!")

    # Run processes
    for p in processes:
        p.start()

    # Exit the completed processes
    for p in processes:
        p.join()

    # update accountant after finishing a round of txs (for each account by order)
    # if more transaction will be sent after this round without saving accountant
    # accountant will lose track because of the concurrency nature
    if queue:
        accountant.set_accountant_from_queue(queue)


if __name__ == "__main__":
    """
    This script relays on the fact we have a tap in our cluster
    with the public and private keys that are mentioned in the conf.py
    file.
    
    initially #new_accounts number of transactions will be sent and create
    #new_accounts new accounts
    
    Sleep for an epoch where the state is updated and new accounts created
     
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
    send_coins_to_new_accounts(parsed_args, acc)
    print(f"\n\n{pprint.pformat(acc.accounts)}\n\n")

    # Sleep for 4 layers to let new txs enter the state and new accounts to be valid
    # TODO remove layers per epoch and sleep for 4 layers #@!
    tts = parsed_args.layer_duration * parsed_args.layers_per_epoch
    print(f"sleeping for {tts} seconds to accept new state")
    time.sleep(tts)

    # Use same pod for every wallet api call
    if parsed_args.same_pod:
        print("Using the same pod for all txs (and nonce querying)")
        my_wallet.fixed_node = -1

    # Set a queue for collecting all the transactions output,
    # this will help with updating the "accountant" when multiprocessing
    q = mp.Queue() if parsed_args.is_concurrent else None
    # TODO remove this hack #@!
    inp = 0
    while inp != 'q':
        inp = input("\npress any key to send txs from each account (enter 'q' to quite): ")
        if inp == 'q':
            break

        if inp == "sp":
            my_wallet.fixed_node = -1
        else:
            my_wallet.fixed_node = None

        send_tx_from_each_account(my_wallet, acc, is_concurrent=parsed_args.is_concurrent, queue=q)

        # set_accountant_from_queue(acc, q)
        print(f"\n\n", pprint.pformat(acc.accounts), "\n\n")


