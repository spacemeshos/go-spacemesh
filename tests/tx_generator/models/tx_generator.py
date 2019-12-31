import tests.tx_generator.config as conf
from tests.ed25519.eddsa import signature2
import xdrlib


ADDRESS_SIZE = 20
SIGNATURE_SIZE = 64


class TxGenerator:
    """
    This object generates new transactions for a specific account by signing
    the new transaction with the account's private key

    """
    def __init__(self, pub=conf.acc_pub, pri=conf.acc_priv):
        self.publicK = bytes.fromhex(pub)
        self.privateK = bytes.fromhex(pri)

    def generate(self, dst, nonce, gas_limit, fee, amount):
        p = xdrlib.Packer()
        p.pack_hyper(nonce)
        dst_bytes = bytes.fromhex(dst)
        # get the LAST 20 bytes of the dst address
        addr = dst_bytes[-ADDRESS_SIZE:]
        p.pack_fstring(ADDRESS_SIZE, addr)
        p.pack_hyper(gas_limit)
        p.pack_hyper(fee)
        p.pack_hyper(amount)

        data = p.get_buffer()
        sign = signature2(data, self.privateK)
        p.pack_fstring(SIGNATURE_SIZE, sign)

        return p.get_buffer()

