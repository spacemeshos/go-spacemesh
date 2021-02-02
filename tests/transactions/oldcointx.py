from dataclasses import dataclass
from .transaction import Address, TxType, IncompleteTransaction, TransactionBody, \
    TX_OLD_COIN, TX_SIGNING_ED, TX_SIGNING_ED_PLUS
from .signature import EdSigningScheme, EdPlusSignigScheme
import xdrlib

@dataclass(frozen=True)
class OldCoinTx(TransactionBody):
    nonce: int
    recipient: Address
    amount: int
    gas_limit: int
    fee: int

    def new_ed(self):
        return IncompleteTransaction(OLD_COIN_ED_TX, self)

    def new_ed_plus(self):
        return IncompleteTransaction(OLD_COIN_ED_PLUS_TX, self)

    def get_xdr_bytes(self) -> bytes:
        p = xdrlib.Packer()
        p.pack_hyper(self.nonce)
        p.pack_fopaque(20,self.recipient.bytes)
        p.pack_hyper(self.gas_limit)
        p.pack_hyper(self.fee)
        p.pack_hyper(self.amount)
        return p.get_buffer()

    def get_digest_bytes(self,xdr_bytes:bytes) -> bytes:
        if len(xdr_bytes) == 0:
            xdr_bytes = self.get_xdr_bytes()
        return xdr_bytes

    def get_recipient(self) -> Address:
        return self.recipient

    def get_ttl(self) -> int:
        return 0

    def get_amount(self) -> int:
        return self.amount

    def get_nonce(self) -> int:
        return self.nonce

    def get_gas_limit(self) -> int:
        return self.gas_limit

    def get_gas_price(self) -> int:
        return 1

    def get_fee(self, spent: int) -> int:
        return self.fee

    def __str__(self) -> str:
        return "OldCoinTx(nonce=%d, recipient=%s, amount=%d, gas_limit=%d, fee=%d)"%\
               (self.nonce, str(self.recipient), self.amount, self.gas_limit, self.fee)

def decode_old_coin_tx(bs: bytes, tt: TxType) -> IncompleteTransaction:
    p = xdrlib.Unpacker(bs)
    account_nonce = p.unpack_hyper()
    addr = p.unpack_fopaque(20)
    gas_limit = p.unpack_hyper()
    fee = p.unpack_hyper()
    amount = p.unpack_hyper()
    p.done()
    return IncompleteTransaction(tt,OldCoinTx(account_nonce,Address(addr),amount,gas_limit,fee))

OLD_COIN_ED_TX = TxType(TX_OLD_COIN + TX_SIGNING_ED, "OLD_COIN_ED_TX", EdSigningScheme, decode_old_coin_tx)
OLD_COIN_ED_PLUS_TX = TxType(TX_OLD_COIN + TX_SIGNING_ED_PLUS, "OLD_COIN_ED_PLUS_TX", EdPlusSignigScheme, decode_old_coin_tx)
