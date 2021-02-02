import xdrlib
from .transaction import \
    IncompleteTransaction, GeneralTransaction, Transaction, TransactionBody, TransactionMessage,\
    tx_id_form_signed_transaction, \
    TX_SIGNING_ED, TX_SIGNING_ED_PLUS, \
    TX_SIMPLE_COIN, TX_CALL_APP, TX_SPAWN_APP, TX_OLD_COIN
from .signature import \
    Address, \
    Signature, Signer, PublicKey, \
    new_signer, signer_from_seed, signer_from_bytes, \
    SIGNATURE_LENGTH, PUBLIC_KEY_LENGTH
from .simplecointx import \
    SIMPLE_COIN_ED_TX, SIMPLE_COIN_ED_PLUS_TX, SimpleCoinTx
from .oldcointx import \
    OLD_COIN_ED_TX, OLD_COIN_ED_PLUS_TX, OldCoinTx
from .callapptx import \
    CALL_APP_ED_TX, CALL_APP_ED_PLUS_TX, CallAppTx
from .spawnapptx import \
    SPAWN_APP_ED_TX, SPAWN_APP_ED_PLUS_TX, SpawnAppTx

tx_types_map = {
    SIMPLE_COIN_ED_TX.value: SIMPLE_COIN_ED_TX,
    SIMPLE_COIN_ED_PLUS_TX.value: SIMPLE_COIN_ED_PLUS_TX,
    OLD_COIN_ED_TX.value: OLD_COIN_ED_TX,
    OLD_COIN_ED_PLUS_TX.value: OLD_COIN_ED_PLUS_TX,
    CALL_APP_ED_TX.value: CALL_APP_ED_TX,
    CALL_APP_ED_PLUS_TX.value: CALL_APP_ED_PLUS_TX,
    SPAWN_APP_ED_TX.value: SPAWN_APP_ED_TX,
    SPAWN_APP_ED_PLUS_TX.value: SPAWN_APP_ED_PLUS_TX,
}


errUnknownType = Exception("can't decode signed transaction: unknown transaction type")
errBadTransaction = Exception("can't decode signed transaction: bad transaction")
errBadPublicKey = Exception("can't decode signed transaction: bad public key")
errVerificationFailed = Exception("can't decode signed transaction: verification failed")


def decode(signed_transaction: bytes) -> Transaction:
    p = xdrlib.Unpacker(signed_transaction)
    tx_type = tx_types_map.get(int(p.unpack_fopaque(1)[0]))
    if tx_type is None:
        raise errUnknownType
    signature = Signature(p.unpack_fopaque(SIGNATURE_LENGTH))
    data = p.unpack_opaque()
    itx = tx_type.decode(data,tx_type)
    if not tx_type.signing.extractable:
        if p.get_position() + PUBLIC_KEY_LENGTH + 4 != len(signed_transaction):
            raise errBadTransaction
        pk_data = p.unpack_opaque()
        if len(pk_data) != PUBLIC_KEY_LENGTH:
            raise errBadPublicKey
        public_key = PublicKey(pk_data)
    else:
        if p.get_position() != len(signed_transaction):
            raise errBadTransaction
        public_key = None
    if tx_type.signing.extractable:
        _, public_key = tx_type.signing.extract(itx.digest, signature)
    elif not tx_type.signing.verify(itx.digest, public_key, signature):
        raise errVerificationFailed
    tx_id = tx_id_form_signed_transaction(signed_transaction)
    return itx.complete(signature, public_key, tx_id)


def sign(tx: IncompleteTransaction, signer: Signer) -> Transaction:
    signed_transaction = tx.message.sign(signer)
    return decode(signed_transaction)


__ALL__ = [
    Address, Signer, PublicKey,
    TX_SIGNING_ED, TX_SIGNING_ED_PLUS,
    TX_SIMPLE_COIN,
    SIMPLE_COIN_ED_TX, SIMPLE_COIN_ED_PLUS_TX, SimpleCoinTx,
    OLD_COIN_ED_TX, OLD_COIN_ED_PLUS_TX, OldCoinTx,
    CALL_APP_ED_TX, CALL_APP_ED_PLUS_TX, CallAppTx,
    SPAWN_APP_ED_TX, SPAWN_APP_ED_PLUS_TX, SpawnAppTx,
    decode, sign, new_signer, signer_from_seed, signer_from_bytes,
]

