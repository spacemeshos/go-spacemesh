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

    if str(nonce) == "None":
        raise Exception("could not resolve nonce")

    tx_gen = TxGenerator(pub=frm, pri=priv)
    # create a transaction
    tx_bytes = tx_gen.generate(to, nonce, gas_limit, gas_price, amount)
    # submit transaction
    success = wallet_api.submit_tx(to, frm, gas_price, amount, tx_bytes)

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


def validate_nonce(wallet_api, acc, nonce):
    print(f"\nchecking nonce for {acc}")
    res = wallet_api.get_nonce_value(acc)
    # res might be None, str(None) == 'None'
    if str(nonce) == str(res):
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
        print(f"balance ok (origin={balance})")
        return True

    print(f"balance did not match: returned balance={res}, expected={balance}")
    return False
