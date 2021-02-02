from typing import Any, Callable
from abc import ABC, abstractmethod
from dataclasses import dataclass
from .signature import Signature, Signer, Address, Signing, PublicKey, SIGNATURE_LENGTH
import hashlib
import xdrlib

ENABLE_TRANSACTION_PRUNING = False


def network_id() -> bytes:
    return bytes([0] * 32)


@dataclass(frozen=True)
class TxType:
    value: int
    name: str
    signing: Signing
    decode: Callable[[bytes], Any]

    def kind(self) -> int:
        return self.value & ~1


@dataclass(frozen=True)
class TxID:
    bytes: bytes


def tx_id_form_signed_transaction(stx: bytes) -> TxID:
    return TxID(hashlib.sha256(stx).digest())


@dataclass(frozen=True)
class TransactionMessage:
    tx_type: TxType
    digest: bytes
    data: bytes

    def sign(self, signer: Signer) -> bytes:
        signature = self.tx_type.signing.sign(self.digest, signer)
        return self.encode(signer.public_key, signature)
        pass

    def encode(self, public_key: PublicKey, signature: Signature) -> bytes:
        p = xdrlib.Packer()
        p.pack_fopaque(1, bytes([self.tx_type.value]))
        p.pack_fopaque(SIGNATURE_LENGTH, signature.bytes)
        p.pack_opaque(self.data)
        if not self.tx_type.signing.extractable:
            p.pack_opaque(public_key.bytes)
        return p.get_buffer()


class TransactionBody(ABC):

    @abstractmethod
    def get_xdr_bytes(self) -> bytes:
        pass

    @abstractmethod
    def get_digest_bytes(self, bs: bytes) -> bytes:
        pass

    @abstractmethod
    def get_recipient(self) -> Address:
        pass

    @abstractmethod
    def get_ttl(self) -> int:
        pass

    @abstractmethod
    def get_amount(self) -> int:
        pass

    @abstractmethod
    def get_nonce(self) -> int:
        pass

    @abstractmethod
    def get_gas_limit(self) -> int:
        pass

    @abstractmethod
    def get_gas_price(self) -> int:
        pass

    @abstractmethod
    def get_fee(self, spent: int) -> int:
        pass


@dataclass(frozen=True)
class GeneralTransaction:
    tx_type: TxType
    body: TransactionBody

    def _digest(self, xdr_bytes: bytes = bytes()) -> bytes:
        digest_bytes = self.body.get_digest_bytes(xdr_bytes)
        p = xdrlib.Packer()
        net_id = network_id()
        p.pack_fopaque(len(net_id), net_id)
        p.pack_fopaque(1, bytes([self.tx_type.value]))
        p.pack_opaque(digest_bytes)
        digest = hashlib.sha512(p.get_buffer()).digest()
        return digest

    @property
    def digest(self) -> bytes:
        return self._digest()

    @property
    def message(self) -> TransactionMessage:
        data = self.body.get_xdr_bytes()
        digest = self._digest(data)
        return TransactionMessage(self.tx_type, digest, data)

    @property
    def xdr_bytes(self) -> bytes:
        return self.body.get_xdr_bytes()

    @property
    def recipient(self) -> Address:
        return self.body.get_recipient()

    @property
    def ttl(self) -> int:
        return self.body.get_ttl()

    @property
    def get_amount(self) -> int:
        return self.body.get_amount()

    @property
    def get_nonce(self) -> int:
        return self.body.get_nonce()

    @property
    def gas_limit(self) -> int:
        return self.body.get_gas_limit()

    @property
    def gas_price(self) -> int:
        return self.body.get_gas_price()

    def get_fee(self, spent: int) -> int:
        return self.body.get_fee(spent)


@dataclass(frozen=True)
class CompletedTransaction:
    _signature: Signature
    _public_key: PublicKey
    _origin: Address
    _tx_id: TxID

    @property
    def origin(self) -> Address:
        return self._origin

    @property
    def public_key(self) -> PublicKey:
        return self._public_key

    @property
    def signature(self) -> Signature:
        return self._signature

    @property
    def tx_id(self) -> TxID:
        return self._tx_id


class IncompleteTransaction(GeneralTransaction):

    def __init__(self, tx_type: TxType, body: TransactionBody):
        super().__init__(tx_type, body)

    def complete(self, signature: Signature, public_key: PublicKey, tx_id: TxID) -> Any:
        return Transaction(self, signature, public_key, tx_id)

    def __str__(self) -> str:
        return "IncompleteTransaction(tx_type=" + self.tx_type.name + ", body=" + str(self.body) + ")"


class Transaction(GeneralTransaction, CompletedTransaction):
    def __init__(self, tx: IncompleteTransaction, signature: Signature, public_key: PublicKey, tx_id: TxID):
        GeneralTransaction.__init__(self, tx.tx_type, tx.body)
        CompletedTransaction.__init__(self, signature, public_key, Address.form_pk(public_key), tx_id)

    def encode(self) -> bytes:
        return self.message.encode(self._public_key, self._signature)

    def __str__(self) -> str:
        return "Transaction(tx_type=" + self.tx_type.name + ", body=" + str(self.body) + \
               ", signature=" + str(self._signature) + \
               ", public_key=" + str(self._public_key) + ")"


TX_SIGNING_ED = 0
TX_SIGNING_ED_PLUS = 1
TX_SIMPLE_COIN = 0
TX_CALL_APP = 2
TX_SPAWN_APP = 4
TX_OLD_COIN = 6
