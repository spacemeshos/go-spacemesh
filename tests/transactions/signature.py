from typing import List
from dataclasses import dataclass
from abc import ABC, abstractmethod
from pure25519.eddsa import H, Hint
from pure25519.basic import bytes_to_clamped_scalar, bytes_to_element, bytes_to_scalar, scalar_to_bytes, Base, L
from pure25519.eddsa import create_signing_key, create_verifying_key, sign as ed_sign, verify as ed_verify

PUBLIC_KEY_LENGTH = 32
SIGNATURE_LENGTH = 64
ADDRESS_LENGTH = 20


@dataclass(frozen=True)
class PublicKey:
    bytes: bytes

    def __str__(self) -> str:
        return self.bytes.hex()

    @staticmethod
    def from_hex(hex_str: str):
        return PublicKey(bytes.fromhex(hex_str))


@dataclass(frozen=True)
class Address:
    bytes: bytes

    def __str__(self) -> str:
        return self.bytes.hex()

    @staticmethod
    def from_list(lst: List[int]):
        return Address(bytes(lst))

    @staticmethod
    def form_pk(pk: PublicKey):
        return Address(pk.bytes[:ADDRESS_LENGTH])

    @staticmethod
    def from_hex(hex_str: str):
        return Address(bytes.fromhex(hex_str))


@dataclass(frozen=True)
class Signature:
    bytes: bytes

    def __str__(self) -> str:
        return self.bytes.hex()

    @staticmethod
    def from_list(lst: List[int]):
        return Signature(bytes(lst))

    @staticmethod
    def from_hex(hex_str: str):
        return Signature(bytes.fromhex(hex_str))


@dataclass(frozen=True)
class Signer:
    priv: bytes
    pubk: bytes

    @property
    def bytes(self) -> bytes:
        return self.priv

    @property
    def address(self) -> Address:
        return Address(self.pubk[:ADDRESS_LENGTH])

    @property
    def public_key(self) -> PublicKey:
        return PublicKey(self.pubk)

    @staticmethod
    def new():
        priv = create_signing_key()
        pubk = create_verifying_key(priv)
        return Signer(priv, pubk)

    @staticmethod
    def from_seed(seed: str):
        priv = seed.encode('utf-8')
        priv = priv + bytes([0] * (PUBLIC_KEY_LENGTH - len(priv)))
        pubk = create_verifying_key(priv)
        return Signer(priv, pubk)

    @staticmethod
    def from_bytes(bs: bytes):
        priv = bs
        pubk = create_verifying_key(priv)
        return Signer(priv, pubk)

    @property
    def bytes(self) -> bytes:
        return self.priv


class Signing(ABC):

    @property
    @abstractmethod
    def extractable(self) -> bool:
        pass

    @abstractmethod
    def sign(self, digest: bytes, signer: Signer) -> Signature:
        pass

    @abstractmethod
    def extract(self, digest: bytes, signature: Signature) -> (bool, PublicKey):
        pass

    @abstractmethod
    def verify(self, digest: bytes, pub_key: PublicKey, signature: Signature) -> bool:
        pass


class _EdSigningScheme(Signing):

    @property
    def extractable(self) -> bool:
        return False

    def sign(self, digest: bytes, signer: Signer) -> Signature:
        return Signature(ed_sign(signer.priv, digest))

    def extract(self, digest: bytes, signature: Signature) -> (bool, PublicKey):
        return False, None

    def verify(self, digest: bytes, pub_key: PublicKey, signature: Signature) -> bool:
        try:
            return ed_verify(pub_key.bytes, signature.bytes, digest)
        except:
            return False


EdSigningScheme = _EdSigningScheme()


class _EdPlusSigningScheme(Signing):

    @property
    def extractable(self) -> bool:
        return True

    def sign(self, digest: bytes, signer: Signer) -> Signature:
        sk = signer.priv
        assert len(sk) == 32  # seed
        h = H(sk[:32])
        a_bytes, inter = h[:32], h[32:]
        a = bytes_to_clamped_scalar(a_bytes)
        r = Hint(inter + digest)
        R = Base.scalarmult(r)
        R_bytes = R.to_bytes()
        S = r + Hint(R_bytes + digest) * a
        return Signature(R_bytes + scalar_to_bytes(S))

    def extract(self, digest: bytes, signature: Signature) -> (bool, PublicKey):
        s = signature.bytes
        if len(s) != 64:
            raise Exception("Signature length is wrong")
        R = bytes_to_element(s[:32])
        S = bytes_to_scalar(s[32:])
        h = Hint(s[:32] + digest)
        h_inv = pow(h, L - 2, L)  # inv2(h)
        R_neg = R.scalarmult(L - 1)
        v1 = Base.scalarmult(S)
        v2 = v1.add(R_neg)
        return True, PublicKey(v2.scalarmult(h_inv).to_bytes())

    def verify(self, digest: bytes, pub_key: PublicKey, signature: Signature) -> bool:
        return True


EdPlusSignigScheme = _EdPlusSigningScheme()
