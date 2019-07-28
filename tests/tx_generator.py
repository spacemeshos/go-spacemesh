import xdrlib
import binascii
from tests.ed25519.eddsa import signature2

ADDRESS_SIZE = 20
SIGNATURE_SIZE = 64

class TxGenerator:
    def __init__(self, pub = "7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c",
                 pri = "81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce5"):
        self.publicK = bytes.fromhex(pub)
        self.privateK = bytes.fromhex(pri)

    def generate(self, dst, nonce, gasLimit, price, amount):
        p = xdrlib.Packer()
        p.pack_hyper(nonce)
        dstBytes = bytes.fromhex(dst)
        addr = dstBytes[len(dstBytes)-20:]
        p.pack_fstring(ADDRESS_SIZE, addr)
        p.pack_hyper(gasLimit)
        p.pack_hyper(price)
        p.pack_hyper(amount)

        data = p.get_buffer()
        sign = signature2(data, self.privateK)
        p.pack_fstring(SIGNATURE_SIZE, sign)

        return p.get_buffer()


if __name__ == "__main__":
# execute only if run as a script
    gen = TxGenerator()
    data = gen.generate("0000000000000000000000000000000000001111", 12345, 56789, 24680, 86420)
    #data = gen.generate("0000000000000000000000000000000000002222", 0, 123, 321, 100)
    #x = (str(list(data)))
    #print('{"tx":'+ x + '}')

    expected = "00000000000030390000000000000000000000000000000000001111000000000000ddd50000000000006068000000000001519417a80a21b815334b3e9afd1bde2b78ab1e3b17932babd2dab33890c2dbf731f87252c68f3490cce3ee69fd97d450d97d7fcf739b05104b63ddafa1c94dae0d0f"
    assert (binascii.hexlify(data)).decode('utf-8') == str(expected)
