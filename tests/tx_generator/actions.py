from itertools import cycle, islice
import multiprocessing as mp
import random

from tests.convenience import sleep_print_backwards
from tests.tx_generator import config as conf
from tests.tx_generator.models.accountant import Accountant
from tests.tx_generator.models.tx_generator import TxGenerator


def transfer(wallet_api, frm, to, amount, gas_price=1, gas_limit=None, curr_nonce=None, accountant=None,
             priv=None, queue=None):
    """
    transfer some mo-ney!

    :param wallet_api: WalletAPI, manager for communicating with the miners
    :param frm: string, a 64 characters hex representation of the sending account
    :param to: string, a 64 characters hex representation of the receiving account
    :param amount: int, amount to send
    :param accountant: Accountant, a manager for current state
    :param gas_price: int, gas-price
    :param gas_limit: int, `gas-limit = gas-price + 1` if no value was supplied
    :param curr_nonce: int, overwrite current sender nonce
    :param priv: string, sending account's private key
    :param queue: multiprocess.Queue, a queue for collecting the accountant result

    :return:
    """

    # set private key
    if not priv:
        if not accountant:
            raise Exception("private key was not supplied can not perform the transfer")

        priv = accountant.get_acc_priv(frm)

    # set gas limit (this was copied from the old script)
    gas_limit = gas_price + 1 if not gas_limit else gas_limit

    # set nonce
    if curr_nonce:
        nonce = int(curr_nonce)
    elif accountant:
        nonce = accountant.get_nonce(frm)
    else:
        nonce = wallet_api.get_nonce_value(frm)

    if nonce is None:
        raise Exception("could not resolve nonce")

    tx_gen = TxGenerator(pub=frm, pri=priv)
    # create a transaction
    tx_bytes = tx_gen.generate(to, nonce, gas_limit, gas_price, amount)
    # submit transaction
    success = wallet_api.submit_tx(to, frm, gas_price, amount, nonce, tx_bytes)

    if success:
        if accountant:
            send_entry = Accountant.set_send(to, amount, gas_price)
            recv_entry = Accountant.set_recv(bytes.hex(tx_gen.publicK), amount, gas_price)
            if queue:
                queue.put(("send", frm, send_entry))
                queue.put(("recv", to, recv_entry))
            else:
                # append transactions into accounts data structure
                print("setting up accountant after transfer success")
                accountant.set_sending_acc_after_tx(frm, send_entry)
                accountant.set_receiving_acc_after_tx(to, recv_entry)

        return True

    return False


def validate_nonce(wallet_api, accountant, acc_pub):
    print(f"\nchecking nonce for {acc_pub} ", end="")
    print("(TAP)") if acc_pub == conf.acc_pub else print()

    nonce = accountant.get_nonce(acc_pub)
    res = wallet_api.get_nonce_value(acc_pub)
    # res might be None, str(None) == 'None'
    if str(nonce) == str(res):
        print(f"nonce ok (origin={nonce})")
        return True

    print(f"nonce did not match: returned balance={res}, expected={nonce}")
    return False


def validate_acc_amount(wallet_api, accountant, acc):
    print(f"\nvalidate balance for {acc} ", end="")
    print("(TAP)") if acc == conf.acc_pub else print()

    res = wallet_api.get_balance_value(acc)
    balance = accountant.get_balance(acc)

    # out might be None
    if str(balance) == str(res):
        print(f"balance ok (origin={balance})\n")
        return True

    print(f"balance did not match: returned balance={res}, expected={balance}")
    return False


def run_processes(processes, accountant, queue):
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


def send_coins_to_new_accounts(wallet_api, new_acc_num, amount, accountant, gas_price=1, src=conf.acc_pub):
    # create #new_acc_num new accounts by sending them new txs
    for tx in range(new_acc_num):
        dst = accountant.new_account()

        if transfer(wallet_api, src, dst, amount, accountant=accountant, gas_price=gas_price):
            print("transaction succeeded!\n")

            # sleep briefly to allow tx to be gossipped and projections to be updated
            sleep_print_backwards(2)

            continue

        print("transaction FAILED!\n")
        return False
    return True


def send_tx_from_each_account(wallet, accountant, tx_num, amount=1, gas_limit=None, is_new_acc=False,
                              is_concurrent=False, is_use_tap=True):
    """
    send random transactions iterating over the accounts list

    :param wallet: WalletAPI, a manager for GRPC communication
    :param accountant: Accountant, a manager for current state
    :param tx_num: int, number of transactions to send
    :param amount: int, number of coins to send
    :param gas_limit: int, max reward for processing a tx
    :param is_new_acc: bool, create new accounts and send money to (if True) or use existing (False)
    :param is_concurrent: bool, send transactions concurrently
    :param is_use_tap: bool, send txs from tap account as well if True

    :return:
    """

    # a list for all transfers to be made concurrently
    processes = []
    # Set a queue for collecting all the transactions output,
    # this will help with updating the "accountant" when multiprocessing
    queue = mp.Queue() if is_concurrent else None

    accounts_copy = accountant.accounts.copy()
    if not is_use_tap:
        del accounts_copy[conf.acc_pub]

    tx_counter = 0
    accs_len = len(accounts_copy)
    pub_keys = list(accounts_copy.keys())
    # randomize senders list
    random.shuffle(pub_keys)
    for sending_acc_pub in islice(cycle(pub_keys), 0, tx_num):
        account_det = accounts_copy[sending_acc_pub]
        if is_concurrent and tx_counter > 0 and tx_counter % accs_len == 0:
            # run all processes and update accountant after finishing a round of txs (for each account by order)
            # if more transaction will be sent after this round without saving accountant
            # accountant will lose track because of the concurrence nature,
            # which mainly will mess up the nonce
            run_processes(processes, accountant, queue)
            processes = []

        # we might send here tx from and to the same account
        # if we're using the random choice
        dst = accountant.new_account() if is_new_acc else random.choice(list(accountant.accounts.keys()))
        balance = accountant.get_balance(sending_acc_pub)
        if balance < 1:
            print(f"account {sending_acc_pub} does not have sufficient funds to make a tx, balance: {balance}")
            continue

        if is_concurrent:
            processes.append(mp.Process(target=transfer, args=(wallet, sending_acc_pub, dst, amount, accountant.tx_cost,
                                                               gas_limit, None, accountant, account_det["priv"], queue))
                             )
            # increment counter
            tx_counter += 1
            continue
        elif transfer(wallet, sending_acc_pub, dst, amount, accountant=accountant, gas_price=accountant.tx_cost,
                      priv=account_det["priv"]):
            print("transaction succeeded!")
            # increment counter
            tx_counter += 1
            continue

        print("transaction failed!")
        tx_counter += 1

    if processes:
        # run all remaining processes
        run_processes(processes, accountant, queue)


def send_random_txs(wallet, accountant, amount=1):
    max_tts = 10
    max_rnd_txs = 50

    while True:
        # use same pod for all GRPCs
        wallet.fixed_node = None
        if bool(random.getrandbits(1)):
            wallet.fixed_node = -1

        is_concurrent = bool(random.getrandbits(1))
        tx_num = random.randint(1, max_rnd_txs)
        send_tx_from_each_account(wallet, accountant, tx_num, amount=amount, gas_limit=None, is_new_acc=False,
                                  is_concurrent=is_concurrent)
        tts = random.randint(3, max_tts)
        sleep_print_backwards(tts)
