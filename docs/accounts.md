## Account Data File Spec

##### Account Key File (stored at rest)
- User account private key should be stored encrypted with the user's passphrase on disk and salted. This provides some security against attackers which gain access to the key store file. The passphrase should only be stored in the user's password wallet (ideally both on his PC and at least 1 mobile device). Users are encouraged to backup their account's key store files. Each file has data about 1 account. File name should be `{user base58 account id}.json`

##### Account data file format

We are using `scrypt` to derive a strong key from users passphrases which may be weak passwords.
Json is used as the file format instead of a binary opaque format to make the account data somewhat human readable and to allow users to view backedup account files.

 
```json
{
  "id": "QmYvV9V1RuwzXjiX5DhesR8PVoA6EATpa1Qp4W96Mr7zDQ",
  "publicKey": "GZsJqUXaEPs35S1HRZehp45eiKRJ3YJfdAH63wkiZYiYyhLanf",
  "crypto": {
    "cipher": "AES-128-CTR",
    "cipherText": "8662bcdb82f8a38f2d4c3a1b6d848fbb19f03d02388aca9442d5e4cc7b5c70aff02c452b",
    "cipherIv": "106c11fa110e99fdcbc5b4a29ddbff7d",
    "mac": "7c5df1ef3bde5e2642931298a9b00fe4375e8722f32e879a155e0d031dd39cf1"
  },
  "kd": {
    "n": 262144,
    "r": 8,
    "p": 1,
    "saltLen": 16,
    "dkLen": 32,
    "salt": "48464c24034c1e706c82336172b67156"
  }
}
```

###### Strings encoding
We use base58 for account id and public key as the canonical encodding but other data is hex encoded.

- id - base58
- publicKey - base58
- cipherText - hex
- cipherV - hex
- mac - hex
- salt: hex



Refs
- https://www.tarsnap.com/scrypt/scrypt.pdf 
- https://github.com/ethereum/go-ethereum/wiki/Passphrase-protected-key-store-spec
- https://github.com/Gustav-Simonsson/go-ethereum/blob/improve_key_store_crypto/crypto/key_store_passphrase.go#L29

- Technically, both account id and public key can be derived from the private key but it is useful to store them in the key store file so users can see to which accounts they have private keys for by looking at the content of the file. The file name will also be `{account-id-str}.json` to make it easy to backup keys for each account.

- Users must maintain both their passphrase as well as a backup of the account data file. Without both they lose access to their account. They are encouraged to backup both to a secure password vault on at least 2 devices.

##### Account passphrase
User provided. Not persisted in the key store file. Users are recommend to generate these using a strong random password generator such as 1-password. We'd like to avoid having users deal with a 12-phrase nemonic and a passphrase. The node software should also support strong random password generation option to minimize the number of weak user-generated passwords (a common thing).

- To access an account private key, user must provide: (passphrase, salt, kdfparams params, cypher init vector). Passphrase is something user has to remember / know, the other params are something user has to have - a keystore file.

##### Unlocking an account
- Enable node to have access to the account's private key (and sign messages / transactions with it) while it is running. Needed for validation as well.
- Alternative is to provide passphrase as param to submit a tx or to submit a signed transaction via the api.
