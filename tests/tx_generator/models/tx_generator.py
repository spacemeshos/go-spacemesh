import struct

import tests.tx_generator.config as conf
from tests.ed25519.eddsa import signature2


ADDRESS_SIZE = 20
INT_SIZE = INT_SIZE
ENDIAN = "little"

class TxGenerator:
    """
    This object generates new transactions for a specific account by signing
    the new transaction with the account's private key

    """
    def __init__(self, pub=conf.acc_pub, pri=conf.acc_priv):
        self.publicK = bytes.fromhex(pub)
        self.privateK = bytes.fromhex(pri)

    def generate(self, dst, nonce, gas_limit, fee, amount):
        inner = nonce.to_bytes(INT_SIZE, ENDIAN)
        inner += bytes.fromhex(dst)[-ADDRESS_SIZE:]
        inner += gas_limit.to_bytes(INT_SIZE, ENDIAN)
        inner += fee.to_bytes(INT_SIZE, ENDIAN)
        inner += amount.to_bytes(INT_SIZE, ENDIAN)

        sign = signature2(inner, self.privateK)
        return inner + sign

