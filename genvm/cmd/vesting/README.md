Vesting address generation
===

This tool generates an address for vesting and vault accounts, based on the set of parameters:
- total number of keys used in vesting multisig
- required signatures
- total vested amount

Amount that is unlocked at the start of vesting is 1/4 of total vested amount.

#### How to use it (with openssl example)?

1. Install tool.

```bash
go install github.com/spacemeshos/go-spacemesh/genvm/cmd/vesting@develop
```

2. Generate keys that will be a part of multisig.

Repeat that as many times as you need keys in multisig.

```bash
./smcli wallet create
...
./smcli wallet create
```


3. Extract public keys from wallet for each one.

```
./smcli wallet read /home/dd/.spacemesh/wallet_2023-07-12T06-14-00.086Z.json -f
...
./smcli wallet read /home/dd/.spacemesh/wallet_2023-07-12T06-43-19.316Z.json -f
```

Expected output:

```
Enter wallet password: 
+---------------------------------------------------------------------------------------------------------------------------------+
| Wallet Contents                                                                                                                 |
+------------------------------------------------------------------+---------------------+-------------+--------------------------+
| PUBKEY                                                           | PATH                | NAME        | CREATED                  |
+------------------------------------------------------------------+---------------------+-------------+--------------------------+
| 33f8d3b3a9fd558ccef98e9693b6b264e5a593ea096c8984012e94c6e7f72692 | m/44'/540'/0'/0'/0' | Child Key 0 | 2023-07-12T06-13-57.282Z |
+------------------------------------------------------------------+---------------------+-------------+--------------------------
```

3. Generate account addresses.

```bash
vesting/ -k=1 -key=33f8d3b3a9fd558ccef98e9693b6b264e5a593ea096c8984012e94c6e7f72692 -key=93f801839a7e709f4c270370665ee71b7d86b5f65cb816559503b311d3213962 -total=100000000e9
```

Note that order of keys and number of requires signatures (k) matters as it is a preimage for generated address, 
and any changes will produce different address.


Without debug:

```json
{"address":"sm1qqqqqq8wk2nfvmsn4qy7g3pmuqh74ylm73we4jcs6k9vu","balance":100000000000000000}
```

With debug:

```
vesting address: sm1qqqqqqphles02lr3um5t8fqxqspfvg525j4y50qvcrfwm.
parameters: required=1
0 : 0x33f8d3b3a9fd558ccef98e9693b6b264e5a593ea096c8984012e94c6e7f72692
---
vault address: sm1qqqqqq8wk2nfvmsn4qy7g3pmuqh74ylm73we4jcs6k9vu.
parameters: owner = sm1qqqqqqphles02lr3um5t8fqxqspfvg525j4y50qvcrfwm. total = 100000000000000000 smidge. initial = 25000000000000000 smidge. start = 105120. end = 420480
```






