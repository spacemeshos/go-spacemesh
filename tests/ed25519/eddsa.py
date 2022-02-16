from pure25519.eddsa import H, Hint
from pure25519.basic import bytes_to_clamped_scalar, bytes_to_element, bytes_to_scalar, scalar_to_bytes, Base, L
from pure25519.eddsa import create_signing_key, create_verifying_key

"""
This file, using pure25519 package, provides high security,
generating key pairs and signatures for the miners.
(e.g. signing transactions)

"""


def genkeypair():
    pri = create_signing_key()
    return pri, create_verifying_key(pri)


def inv2(x):
    return pow(x, L-2, L)


def signature2(m, sk):
    assert len(sk) == 32  # seed
    h = H(sk[:32])
    a_bytes, inter = h[:32], h[32:]
    a = bytes_to_clamped_scalar(a_bytes)
    r = Hint(inter + m)
    R = Base.scalarmult(r)
    R_bytes = R.to_bytes()
    S = r + Hint(R_bytes + m) * a
    return R_bytes + scalar_to_bytes(S)


def extractpk(s, m):
    if len(s) != 64:
        raise Exception("Signature length is wrong")
    R = bytes_to_element(s[:32])
    S = bytes_to_scalar(s[32:])
    h = Hint(s[:32] + m)
    h_inv = inv2(h)
    R_neg = R.scalarmult(L-1)
    v1 = Base.scalarmult(S)
    v2 = v1.add(R_neg)
    A = v2.scalarmult(h_inv)
    return A


def checkpk(pk, ext_pk):
    if len(pk) != 32:
        raise Exception("Public-key length is wrong")

    A = bytes_to_element(pk)
    if A != ext_pk:
        raise Exception("Wrong public key extracted")
