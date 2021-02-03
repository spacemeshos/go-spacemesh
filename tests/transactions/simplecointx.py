from dataclasses import dataclass
from .transaction import Address, TxType, IncompleteTransaction, TransactionBody, \
    TX_SIMPLE_COIN, TX_SIGNING_ED, TX_SIGNING_ED_PLUS
from .signature import EdSigningScheme, EdPlusSignigScheme
import xdrlib


@dataclass(frozen=True)
class SimpleCoinTx(TransactionBody):
    ttl: int
    nonce: int
    recipient: Address
    amount: int
    gas_limit: int
    gas_price: int

    def new_ed(self):
        return IncompleteTransaction(SIMPLE_COIN_ED_TX, self)

    def new_ed_plus(self):
        return IncompleteTransaction(SIMPLE_COIN_ED_PLUS_TX, self)

    def get_xdr_bytes(self) -> bytes:
        p = xdrlib.Packer()
        p.pack_int(self.ttl)
        p.pack_fopaque(1, bytes([self.nonce]))
        p.pack_fopaque(20, self.recipient.bytes)
        p.pack_hyper(self.amount)
        p.pack_hyper(self.gas_limit)
        p.pack_hyper(self.gas_price)
        return p.get_buffer()

    def get_digest_bytes(self, xdr_bytes: bytes) -> bytes:
        if len(xdr_bytes) == 0:
            xdr_bytes = self.get_xdr_bytes()
        return xdr_bytes

    def get_recipient(self) -> Address:
        return self.recipient

    def get_ttl(self) -> int:
        return self.ttl

    def get_amount(self) -> int:
        return self.amount

    def get_nonce(self) -> int:
        return self.nonce

    def get_gas_limit(self) -> int:
        return self.gas_limit

    def get_gas_price(self) -> int:
        return self.gas_price

    def get_fee(self, spent: int) -> int:
        return self.gas_price * spent

    def __str__(self) -> str:
        return "SimpleCoinTx(ttl=%d, nonce=%d, recipient=%s, amount=%d, gas_limit=%d, gas_price=%d)" % \
               (self.ttl, self.nonce, str(self.recipient), self.amount, self.gas_limit, self.gas_price)


def decode_simple_coin_tx(bs: bytes, tt: TxType) -> IncompleteTransaction:
    p = xdrlib.Unpacker(bs)
    ttl = p.unpack_int()
    nonce = p.unpack_fopaque(1)[0]
    addr = p.unpack_fopaque(20)
    amount = p.unpack_hyper()
    gas_limit = p.unpack_hyper()
    gas_price = p.unpack_hyper()
    p.done()
    return IncompleteTransaction(tt, SimpleCoinTx(ttl, nonce, Address(addr), amount, gas_limit, gas_price))


SIMPLE_COIN_ED_TX = TxType(TX_SIMPLE_COIN + TX_SIGNING_ED, "SIMPLE_COIN_ED_TX", EdSigningScheme, decode_simple_coin_tx)
SIMPLE_COIN_ED_PLUS_TX = TxType(TX_SIMPLE_COIN + TX_SIGNING_ED_PLUS, "SIMPLE_COIN_ED_PLUS_TX", EdPlusSignigScheme,
                                decode_simple_coin_tx)
