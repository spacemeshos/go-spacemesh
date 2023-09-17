P2P
===

### Direct connections

If you are running multiple nodes in the local network and exposing all of them
is not convenient there is an option to setup network manually, by making
couple of nodes publicly available and the rest connected to them directly.

### Get network id
- get it with grpcurl

> grpcurl -plaintext 127.0.0.1:9092 spacemesh.v1.DebugService.NetworkInfo

```json
{
  "id": "12D3KooWRfy4Sj4rDHDuBaYw3Mg5d2puwiCyqBCWMziFquaGQ5g8"
}
```

- get it from stored data

> cat ~/spacemesh/p2p/p2p.key
```json
{"Key":"CAESQAQN38GXvr+L+G+/JWoimpqPBK7I6INe+PKYA+hRJg0I65Q3IPK49Ii9dcnC3+UqB+jMEL16sqDfUubxTs62rZU=","ID":"12D3KooWRfy4Sj4rDHDuBaYw3Mg5d2puwiCyqBCWMziFquaGQ5g8"}
```

### Configuration for public node

Public node should have higher peer limits to help with network connectivity
and a list of botnodes. Direct connections should be reciprocal, otherwise
public node may prune your private node if overloaded. 

Setup more than one public node, and perform rolling upgrades or restart
for them if needed.

```json
{
    "p2p": {
        "min-peers": 30,
        "low-peers": 60,
        "high-peers": 100,
        "direct": [
            "/ip4/0.0.0.0/tcp/6000/p2p/12D3KooWRkBh6QayKLb1pDRJGMHE94Lix4ZBVh2BJJeX6mghk8VH"
        ],
        "bootnodes": [
            "/dns4/mainnet-bootnode-10.spacemesh.network/tcp/5000/p2p/12D3KooWHK5m83sNj2eNMJMGAngcS9gBja27ho83t79Q2CD4iRjQ",
            "/dns4/mainnet-bootnode-11.spacemesh.network/tcp/5000/p2p/12D3KooWFrCDS8tc29nxJEYf4sKFXhXw7wMSdhQP4S7tsbfh6ngn"
        ]
    }
}
```

> [!NOTE]  
> Please note that 0.0.0.0 in the above config will work ONLY if all nodes are on the same host. If you're using multiple hosts make sure that you're using proper IPs on both sides.
> The `bootnodes` section can contain any bootnodes (or all) depending on your preference.

### Configuration for private node

Set min-peers to the number of peers in the config.
low-peers and high-peers should not be lower than min-peers.

```json
{
    "p2p": {
        "listen": "/ip4/0.0.0.0/tcp/6000",
        "min-peers": 1,
        "low-peers": 10,
        "high-peers": 20,
        "bootnodes": [],
        "direct": [
            "/ip4/0.0.0.0/tcp/7513/p2p/12D3KooWRfy4Sj4rDHDuBaYw3Mg5d2puwiCyqBCWMziFquaGQ5g8"
        ]
    }
}
```

> [!NOTE]  
> Please note that 0.0.0.0 in the above config will work ONLY if all nodes are on the same host. If you're using multiple hosts make sure that you're using proper IPs on both sides.

#### Expected result

Public node will maintain many open connections

> ss -npO4 | rg spacemesh | rg 7513 | rg ESTAB | wc -l
> 52

Private will connect only to the specified public node:

> ss -npO4 | rg spacemesh | rg 6000

```
tcp   ESTAB      0      0              127.0.0.1:7513        127.0.0.1:6000  users:(("go-spacemesh",pid=39165,fd=11)) 
tcp   ESTAB      0      0              127.0.0.1:6000        127.0.0.1:7513  users:(("go-spacemesh",pid=39202,fd=47))
```
