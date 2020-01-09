from tests.tx_generator import config as conf
from tests.tx_generator.models.accountant import Accountant
from tests.tx_generator.models.tx_generator import TxGenerator


def transfer(wallet_api, frm, to, amount, gas_price=1, gas_limit=None, curr_nonce=None, accountant=None,
             priv=None, queue=None):
    """
    TODO add docstring here #@!

    :param wallet_api:
    :param frm:
    :param to:
    :param amount:
    :param accountant:
    :param gas_price:
    :param gas_limit:
    :param curr_nonce:
    :param priv:
    :param queue:

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
            print("setting up accountant after transfer success")
            send_entry = Accountant.set_send(to, amount, gas_price)
            recv_entry = Accountant.set_recv(bytes.hex(tx_gen.publicK), amount, gas_price)

            if queue:
                queue.put(("send", frm, send_entry))
                queue.put(("recv", to, recv_entry))
            else:
                # append transactions into accounts data structure
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
