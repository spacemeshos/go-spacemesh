## Crypto Design Questions

#### please add all security / crypto design issues here

1. Use only n lsb bytes of account bin data for canonical account id?
Eth is using last 20 bytes of a 32 bytes hash.

2. Do we need to use scrypt to securly store user key encrypted by his password?
The idea is to have a memory-hard stop for each brute-force password guess.
So even an attacker with access to the keystore file but not to the password
won't be able to brute-force the private key from the key store.

3. We use the golang built-in crypto lib and go-btcsuite
