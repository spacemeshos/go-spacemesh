from dataclasses import dataclass
from .transaction import Address, TxType, IncompleteTransaction, TransactionBody, \
    TX_CALL_APP, TX_SIGNING_ED, TX_SIGNING_ED_PLUS, \
    ENABLE_TRANSACTION_PRUNING
from .signature import EdSigningScheme, EdPlusSignigScheme
import xdrlib
import hashlib


@dataclass(frozen=True)
class CallAppTx(TransactionBody):
    ttl: int
    nonce: int
    app_address: Address
    amount: int
    gas_limit: int
    gas_price: int
    call_data: bytes

    def new_ed(self):
        return IncompleteTransaction(CALL_APP_ED_TX, self)

    def new_ed_plus(self):
        return IncompleteTransaction(CALL_APP_ED_PLUS_TX, self)

    def get_xdr_bytes(self) -> bytes:
        p = xdrlib.Packer()
        p.pack_int(self.ttl)
        p.pack_fopaque(1,bytes([self.nonce]))
        p.pack_fopaque(20,self.app_address.bytes)
        p.pack_hyper(self.amount)
        p.pack_hyper(self.gas_limit)
        p.pack_hyper(self.gas_price)
        p.pack_opaque(self.call_data)
        return p.get_buffer()

    def get_digest_bytes(self, xdr_bytes: bytes) -> bytes:
        if ENABLE_TRANSACTION_PRUNING:
            p = xdrlib.Packer()
            p.pack_fopaque(20,self.app_address.bytes)
            p.pack_opaque(self.call_data)
            hash1 = hashlib.sha256(p.get_buffer()).digest()
            p = xdrlib.Packer()
            p.pack_int(self.ttl)
            p.pack_fopaque(1,bytes([self.nonce]))
            p.pack_hyper(self.amount)
            p.pack_hyper(self.gas_limit)
            p.pack_hyper(self.gas_price)
            p.pack_fopaque(32,hash1)
            return p.get_buffer()
        else:
            return xdr_bytes if len(xdr_bytes) != 0 else self.get_xdr_bytes()

    def get_recipient(self) -> Address:
        return self.app_address

    def get_ttl(self) -> int:
        return 0

    def get_amount(self) -> int:
        return self.amount

    def get_nonce(self) -> int:
        return self.nonce

    def get_gas_limit(self) -> int:
        return self.gas_limit

    def get_gas_price(self) -> int:
        return self.gas_price

    def get_fee(self, spent: int) -> int:
        return spent * self.gas_price

    def __str__(self) -> str:
        return "CallAppTx(ttl=%d, nonce=%d, app_address=%s, amount=%d, gas_limit=%d, gas_price=%d)" % \
               (self.ttl, self.nonce, str(self.app_address), self.amount, self.gas_limit, self.gas_price)


def decode_call_app_tx(bs: bytes, tt: TxType) -> IncompleteTransaction:
    p = xdrlib.Unpacker(bs)
    ttl = p.unpack_int()
    nonce = p.unpack_fopaque(1)[0]
    addr = p.unpack_fopaque(20)
    amount = p.unpack_hyper()
    gas_limit = p.unpack_hyper()
    gas_price = p.unpack_hyper()
    call_data = p.unpack_opaque()
    p.done()
    return IncompleteTransaction(tt, CallAppTx(ttl, nonce, Address(addr), amount, gas_limit, gas_price, call_data))


CALL_APP_ED_TX = TxType(
    TX_CALL_APP + TX_SIGNING_ED, "CALL_APP_ED_TX",
    EdSigningScheme,
    decode_call_app_tx)

CALL_APP_ED_PLUS_TX = TxType(
    TX_CALL_APP + TX_SIGNING_ED_PLUS, "CALL_APP_ED_PLUS_TX",
    EdPlusSignigScheme,
    decode_call_app_tx)
