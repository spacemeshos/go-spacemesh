import random

from tests.tx_generator import config as conf
from tests.tx_generator.models.accountant import Accountant
from tests.tx_generator.models.tx_generator import TxGenerator


def transfer(wallet_api, accountant, frm, to, amount=None, gas_price=None, gas_limit=None):
    tx_gen = TxGenerator(pub=frm, pri=accountant.get_acc_priv(frm))
    if not amount:
        amount = random.randint(1, accountant.get_balance(frm) - accountant.tx_cost)
    if not gas_price:
        gas_price = 1
    if not gas_limit:
        gas_limit = gas_price + 1

    # create transaction
    tx_bytes = tx_gen.generate(to, accountant.get_nonce(frm), gas_limit, gas_price, amount)
    # submit transaction
    success = wallet_api.submit_tx(to, frm, gas_price, amount, tx_bytes)
    accountant.set_nonce(frm)
    # append transactions into accounts data structure
    if success:
        accountant.set_acc_recv(to, Accountant.set_recv(bytes.hex(tx_gen.publicK), amount, gas_price))
        accountant.set_acc_send(frm, Accountant.set_send(to, amount, gas_price))
        return True

    return False


def validate_nonce(wallet_api, acc, nonce):
    print(f"\nchecking nonce for {acc}")
    out = wallet_api.get_nonce(acc)
    if str(nonce) in out:
        return True

    return False


def validate_acc_amount(wallet_api, accountant, acc):
    print(f"\nvalidate balance for {acc}")
    if acc == conf.acc_pub:
        print("TAP")

    res_fmt = "{{'value': '{}'}}"
    out = wallet_api.get_balance(acc)
    balance = accountant.get_balance(acc)

    if res_fmt.format(str(balance)) in out:
        print(f"balance ok (origin={balance})")
        return True

    print(f"balance did not match: returned balance={out}, expected={balance}")
    return False
