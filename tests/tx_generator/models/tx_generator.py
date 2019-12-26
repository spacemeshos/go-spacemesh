from tests.ed25519.eddsa import signature2
import xdrlib


ADDRESS_SIZE = 20
SIGNATURE_SIZE = 64


class TxGenerator:
    def __init__(self, pub="7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c",
                 pri="81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce5"):
        self.publicK = bytes.fromhex(pub)
        self.privateK = bytes.fromhex(pri)

    def generate(self, dst, nonce, gas_limit, fee, amount):
        p = xdrlib.Packer()
        p.pack_hyper(nonce)
        dst_bytes = bytes.fromhex(dst)
        addr = dst_bytes[len(dst_bytes) - 20:]
        p.pack_fstring(ADDRESS_SIZE, addr)
        p.pack_hyper(gas_limit)
        p.pack_hyper(fee)
        p.pack_hyper(amount)

        data = p.get_buffer()
        sign = signature2(data, self.privateK)
        p.pack_fstring(SIGNATURE_SIZE, sign)

        return p.get_buffer()

