## Key File Spec

##### Account Key File (stored at rest)
- User account private key should be stored encrypted with the user's passphrase on disk and salted. This provides some security against attackers which gain access to the key store file. The passphrase should only be stored in the user's password wallet (ideally both on his PC and at least 1 mobile device). Users are encouraged to backup their account's key store files. Each file has data about 1 account. File name should be `{user base58 account id}.json`

##### Key file data format

For sake of saving development time we are considering to reuse most of eth key store file format and private key encryption using scrypt, a password and a salt.

```json
{
  "accountPublicKey":"base58-encoded-account-pub-key(Scep256k1)",
  "accountId":"account-id--is-a-base-58-encoded-multi-hash256-of-public-key-raw-binary-format",
  "version":1,
  "crypto":{
    "cipher":"aes-128-ctr",
    "ciphertext":"AES(salt||encrypted-private-key-data-only-non-multi-hash)",
    "cipherparams":{
      "iv":"cabce7fb34e4881870a2419b93f6c796"
    },
    "kdf":"scrypt",
    "kdfparams":{
      "dklen":32,
      "n":262144,
      "p":1,
      "r":8,
      "salt":"1af9c4a44cf45fe6fb03dcc126fa56cb0f9e81463683dd6493fb4dc76edddd51"
    },
    "mac":"5cf4012fffd1fbe41b122386122350c3825a709619224961a16e908c2a366aa6"
  }
}
```

Refs
- https://www.tarsnap.com/scrypt/scrypt.pdf 
- https://github.com/ethereum/go-ethereum/wiki/Passphrase-protected-key-store-spec
- https://github.com/Gustav-Simonsson/go-ethereum/blob/improve_key_store_crypto/crypto/key_store_passphrase.go#L29

- We need to do more research to figure out if we really need to use `scrypt` here or we can just make the cipher private key a sha256(salt||user-password).

- Technically, both account id and public key can be derived from the private key but it is useful to store them in the key store file so users can see to which accounts they have private keys for by looking at the content of the file. The file name will also be `{account-id-str}.json` to make it easy to backup keys for each account

- Users must maintain both their passphrase as well as a backup of the key file. Without both they lose access to their account. They are encouraged to backup both to a secure password vault on at least 2 devices.

##### Account passphrase
User provided. Not persisted in the key store file. Users are recommend to generate these using a strong random password generator such as 1-password. We'd like to avoid having users deal with a 12-phrase nemonic and a passphrase. The node software should also support strong random password generation option to minimize the number of weak user-generated passwords (a common thing).

- To access an account private key, user must provide: (passphrase, salt, kdfparams params, cypher init vector). Passphrase is something user has to remember / know, the other params are something user has to have - a keystore file.

##### Unlocking an account
- Enable node to have access to the account's private key (and sign messages / transactions with it) while it is running. Needed for validation as well.
- Alternative is to provide passphrase as param to submit a tx or to submit a signed transaction via the api.
