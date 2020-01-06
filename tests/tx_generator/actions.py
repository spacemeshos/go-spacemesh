from tests.tx_generator import config as conf
from tests.tx_generator.models.accountant import Accountant
from tests.tx_generator.models.tx_generator import TxGenerator


def transfer(wallet_api, frm, to, amount, accountant=None, gas_price=1, gas_limit=None,
             curr_nonce=None, priv=None, fixed_node=None):
    # set private key
    if not priv:
        if not accountant:
            raise Exception("private key was not supplied can not perform the transfer")

        priv = accountant.get_acc_priv(frm)
    # set gas limit
    if not gas_limit:
        gas_limit = gas_price + 1
    # set nonce
    if curr_nonce:
        nonce = int(curr_nonce)
    elif accountant:
        nonce = accountant.get_nonce(frm)
    else:
        nonce_res = wallet_api.get_nonce(frm)
        nonce = int(wallet_api.extract_nonce_from_resp(nonce_res))

    tx_gen = TxGenerator(pub=frm, pri=priv)
    # create transaction
    tx_bytes = tx_gen.generate(to, nonce, gas_limit, gas_price, amount)
    # submit transaction
    success = wallet_api.submit_tx(to, frm, gas_price, amount, tx_bytes)
    if success:
        if accountant:
            print("setting up accountant after transfer success")
            # append transactions into accounts data structure
            accountant.set_nonce(frm)
            accountant.set_acc_recv(to, Accountant.set_recv(bytes.hex(tx_gen.publicK), amount, gas_price))
            accountant.set_acc_send(frm, Accountant.set_send(to, amount, gas_price))

        return True

    return False


def validate_nonce(wallet_api, acc, nonce):
    print(f"\nchecking nonce for {acc}")
    res = wallet_api.get_nonce_value(acc)
    # res might be None
    if str(nonce) == str(res):
        return True

    print(f"nonce did not match: returned balance={res}, expected={nonce}")
    return False


# this function serves pytest code
def validate_acc_amount(wallet_api, accountant, acc):
    print(f"\nvalidate balance for {acc} ", end="")
    print("(TAP)") if acc == conf.acc_pub else print()

    res_fmt = "{{'value': '{}'}}"
    res = wallet_api.get_balance_value(acc)
    balance = accountant.get_balance(acc)

    # out might be None
    if str(balance) == str(res):
        print(f"balance ok (origin={balance})")
        return True

    print(f"balance did not match: returned balance={res}, expected={balance}")
    return False
