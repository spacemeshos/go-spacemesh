import xdrlib

import tests.tx_generator.config as conf
from tests.ed25519.eddsa import signature2
from tests.transactions import OldCoinTx, Signer, Address, sign


class TxGenerator:
    """
    This object generates new transactions for a specific account by signing
    the new transaction with the account's private key

    """
    def __init__(self, pub=conf.acc_pub, pri=conf.acc_priv):
        self.signer = Signer(priv=bytes.fromhex(pri),pubk=bytes.fromhex(pub))

    def generate(self, dst, nonce, gas_limit, fee, amount):
        tx = OldCoinTx(nonce=nonce,recipient=Address(bytes.fromhex(dst)),gas_limit=gas_limit,fee=fee,amount=amount)
        return sign(tx.new_ed(), self.signer)

