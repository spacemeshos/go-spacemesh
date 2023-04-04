Vesting address generation
===

This tool generates an address for vesting and vault accounts, based on the set of parameters:
- total number of keys used in vesting multisig
- required signatures
- total vested amount
- initial vested amount

#### How to use it (with openssl example)?

1. Install tool.

```bash
go install github.com/spacemeshos/go-spacemesh/genvm/cmd/vesting@develop
```

2. Setup some public keys.

```bash
openssl genpkey -algorithm ed25519 -out /tmp/privates/0.pem
openssl genpkey -algorithm ed25519 -out /tmp/privates/1.pem
openssl genpkey -algorithm ed25519 -out /tmp/privates/2.pem

openssl pkey -in /tmp/privates/0.pem -out /tmp/publics/0.pem
openssl pkey -in /tmp/privates/1.pem -out /tmp/publics/1.pem
openssl pkey -in /tmp/privates/2.pem -out /tmp/publics/2.pem
```

3. Generate account addresses.

```bash
vesting -k 2 --d /tmp/publics

vesting: sm1qqqqqqp8pfuxrvxhjshvxg8f7yavxmykrh3ey0scf946c
vault: sm1qqqqqq8v8lj5498j48z2r6vm25uanjuf4ysj53sucs8mz
public keys:
0: 0x537764ca2312e3b2e247a535ffb2be95f88dd3973a2b7cadc18489d64e923555
1: 0x857373be04b38b92a450bac8e69fed5fcc58543aff2fe275735a76c0a6686388
2: 0x6c99f84c9e11283f3095b5f57b0687ab98eb4fbadc2892089298ff17b00cd4a2
```

Note that order of the keys is important, and different order will result in different address.





