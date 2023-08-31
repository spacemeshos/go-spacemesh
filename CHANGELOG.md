# Changelog

See [RELEASE](./RELEASE.md) for workflow instructions.

## UNRELEASED

### Upgrade information

### Highlights

### Features

* [#4765](https://github.com/spacemeshos/go-spacemesh/pull/4765) hare 3 consensus protocol.

Replacement for original version of hare. Won't be enabled on mainnet for now.
Otherwise protocol uses significantly less traffic (atlest x20), and will allow
to set lower expected latency in the network, eventually reducing layer time. 

### Improvements

* [#4879](https://github.com/spacemeshos/go-spacemesh/pull/4795) Makes majority calculation weighted for optimistic filtering.
The network will start using the new algorithm at layer 18_000 (2023-09-14 20:00:00 +0000 UTC)
* [#4923](https://github.com/spacemeshos/go-spacemesh/pull/4923) Faster ballot eligibility validation. Improves sync speed. 

## v1.1.2

### Upgrade information

Legacy discovery protocol was removed in [#4836](https://github.com/spacemeshos/go-spacemesh/pull/4836).
Config option and flag `p2p-disable-legacy-discovery` is noop, and will be completely removed in future versions.

### Highlights

With [#4893](https://github.com/spacemeshos/go-spacemesh/pull/4893) Nodes are given more time to publish an ATX
Nodes still need to publish an ATX before the new PoET round starts (within 12h on mainnet) to make it into the
next PoET round, but if they miss that deadline they will now continue to publish an ATX to receive rewards for
the upcoming epoch and skip one after that.

### Features

* [#4845](https://github.com/spacemeshos/go-spacemesh/pull/4845) API to fetch opened connections.

> grpcurl -plaintext 127.0.0.1:9093 spacemesh.v1.AdminService.PeerInfoStream

```json
{
  "id": "12D3KooWEcgADBR4zHirw7YAt6nUtCNhbPWkB2fnHAemnG5cGf2n",
  "connections": [
    {
      "address": "/ip4/46.4.81.145/tcp/5001",
      "uptime": "1359.186975782s",
      "outbound": true
    }
  ]
}
{
  "id": "12D3KooWHK5m83sNj2eNMJMGAngcS9gBja27ho83t79Q2CD4iRjQ",
  "connections": [
    {
      "address": "/ip4/34.86.244.124/tcp/5000",
      "uptime": "1108.414456262s",
      "outbound": true
    }
  ],
  "tags": [
    "bootnode"
  ]
}
```

* [4795](https://github.com/spacemeshos/go-spacemesh/pull/4795) p2p: add ip4/ip6 blocklists

Doesn't affect direct peers. In order to disable:

```json
{
  "p2p": {
    "ip4-blocklist": [],
    "ip6-blocklist": []
  }
}
```

### Improvements

* [#4882](https://github.com/spacemeshos/go-spacemesh/pull/4882) Increase cache size and parametrize datastore.
* [#4887](https://github.com/spacemeshos/go-spacemesh/pull/4887) Fixed crashes on API call.
* [#4871](https://github.com/spacemeshos/go-spacemesh/pull/4871) Add jitter to spread out requests to get poet proof and submit challenge
