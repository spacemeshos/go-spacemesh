# Changelog

See [RELEASE](./RELEASE.md) for workflow instructions.

## Release v1.2.13

### Improvements

* [#5384](https://github.com/spacemeshos/go-spacemesh/pull/5384) to improve network stability and performance allow the
  active set to be set in advance for an epoch.

## Release v1.2.12

### Improvements

* [#5373](https://github.com/spacemeshos/go-spacemesh/pull/5373) automatic scaling of post verifying workers to a lower
  value (1 by default) when POST proving starts. The workers are scaled up when POST proving finishes.

* [#5382](https://github.com/spacemeshos/go-spacemesh/pull/5382) avoid processing same (gossiped/fetched) ATX many times
  in parallel

## Release v1.2.11

### Improvements

* increased the max response data size in p2p to 40MiB

## Release v1.2.10

### Improvements

* further increased cache sizes and and p2p timeouts to compensate for the increased number of nodes on the network.

* [#5329](https://github.com/spacemeshos/go-spacemesh/pull/5329) P2P decentralization improvements. Added support for QUIC
  transport and DHT routing discovery for finding peers and relays. Also, added the `ping-peers` feature which is useful
  during connectivity troubleshooting. `static-relays` feature can be used to provide a static list of circuit v2 relays
  nodes when automatic relay discovery is not desired. All of the relay server resource settings are now configurable. Most
  of the new functionality is disabled by default unless explicitly enabled in the config via `enable-routing-discovery`,
  `routing-discovery-advertise`, `enable-quic-transport`, `static-relays` and `ping-peers` options in the `p2p` config
  section. The non-conditional changes include values/provides support on all of the nodes, which will enable DHT to
  function efficiently for routing discovery.

## Release v1.2.9

### Improvements

* increased default cache sizes to improve disk IO and queue sizes for gossip to better handle the increasing number of
  nodes on the network.

## Release v1.2.8

### Improvements

* cherry picked migrations for active sets to enable easier pruning in the future

## Release v1.2.7

### Improvements

* [#5289](https://github.com/spacemeshos/go-spacemesh/pull/5289) build active set from activations received on time

  should decrease the state growth at the start of the epoch because of number of unique activets.

* [#5291](https://github.com/spacemeshos/go-spacemesh/pull/5291) parametrize queue size and throttle limits in gossip

* reduce log level for "atx omitted from active set"

* [#5302](https://github.com/spacemeshos/go-spacemesh/pull/5302) improvements in transaction validation

## Release v1.2.6

### Improvements

* [#5263](https://github.com/spacemeshos/go-spacemesh/pull/5263) randomize peer selection

  without this change node can get stuck after restart on requesting data from peer that is misbehaving.
  log below will be printed repeatedly:

  > 2023-11-15T08:00:17.937+0100 INFO fd68b.sync syncing atx from genesis

* [#5264](https://github.com/spacemeshos/go-spacemesh/pull/5264) increase limits related to activations

  some of the limits were hardcoded and didn't account for growth in atx number.
  this change is not required for node to work correct in the next epoch, but will be required later.

## Release v1.2.5

### Improvements

* [#5247](https://github.com/spacemeshos/go-spacemesh/pull/5247) exits cleanly without misleading spamming

Fixes a bug that would spam with misleading log on exit, such as below:

  > 2023-11-09T11:13:27.835-0600 ERROR e524f.hare failed to update weakcoin {"node_id":
  "e524fce96f0a87140ba895b56a2e37e29581075302778a05dd146f4b74fce72e", "module": "hare", "lid": 0, "error":
  "set weak coin 0: database: no free connection"}

## Release v1.2.4

### Upgrade information

This release enables hare3 on testnet networks. And schedules planned upgrade on mainnet.
Network will have downtime if majority of nodes don't upgrade before layer 35117.
Starting at layer 35117 network is expected to start using hare3, which reduces total bandwidth
requirements.

## Release v1.2.2

### Improvements

* [#5118](https://github.com/spacemeshos/go-spacemesh/pull/5118) reduce number of tortoise results returned after recovery.

  this is hotfix for a bug introduced in v1.2.0. in rare conditions node may loop with the following warning:

  > 2023-10-02T15:28:14.002+0200 WARN fd68b.sync mesh failed to process layer from sync {"node_id":
  "fd68b9397572556c2f329f3e5af2faf23aef85dbbbb7e38447fae2f4ef38899f", "module": "sync", "sessionId":
  "29422935-68d6-47d1-87a8-02293aa181f3", "layer_id": 23104, "errmsg": "requested layer 8063 is before evicted 13102",
  "name": "sync"}

* [#5109](https://github.com/spacemeshos/go-spacemesh/pull/5109) Limit number of layers that tortoise needs to read on startup.

  Bounds the time required to restart a node.

* [#5138](https://github.com/spacemeshos/go-spacemesh/pull/5138) Bump poet to v0.9.7

  The submit proof of work should now be up to 40% faster thanks to [code optimization](https://github.com/spacemeshos/poet/pull/419).

* [#5143](https://github.com/spacemeshos/go-spacemesh/pull/5143) Select good peers for sync requests.

  The change improves initial sync speed and any sync protocol requests required during consensus.

## v1.2.0

### Upgrade information

This upgrade is incompatible with versions older than v1.1.6.

At the start of the upgrade, mesh data will be pruned and compacted/vacuumed. Pruning takes longer for
nodes that joined the network earlier. For 1-week-old nodes, it takes ~20 minutes. for 3-week-old
nodes it takes ~40 minutes. The vacuum operation takes ~5 minutes and requires extra disk space
to complete successfully. If the size of state.sql is 25 GiB at the beginning of upgrade, the WAL file
(state.sql-wal) will grow to 25 GiB and then drop to 0 when the vacuum is complete. The node will resume
its normal startup routine after the pruning and vacuum is complete. The final size of state.sql is
expected to be ~1.5 GiB.

A new config `poet-request-timeout` has been added, that defines the timeout for requesting PoET proofs.
It defaults to 9 minutes so there is enough time to retry if the request fails.

Config option and flag `p2p-disable-legacy-discovery` is dropped. DHT has been the only p2p discovery
mechanism since release v1.1.2.

Support for old certificate sync protocol is dropped. This update is incompatible with v1.0.x series.

### Features

* [#5031](https://github.com/spacemeshos/go-spacemesh/pull/5031) Nodes will also fetch from PoET 112 for round 4 if they
  were able to register to PoET 110.
* [#5067](https://github.com/spacemeshos/go-spacemesh/pull/5067) dbstat virtual table can be read periodically to collect
  table/index sizes.

In order to enable provide following configuration:

```json
"main": {
    "db-size-metering-interval": "10m"
}
```

### Improvements

* [#4998](https://github.com/spacemeshos/go-spacemesh/pull/4998) First phase of state size reduction.
  Ephemeral data are deleted and state compacted at the time of upgrade. In steady-state, data is pruned periodically.
* [#5021](https://github.com/spacemeshos/go-spacemesh/pull/5021) Drop support for old certificate sync protocol.
* [#5024](https://github.com/spacemeshos/go-spacemesh/pull/5024) Active set will be saved in state separately from ballots.
* [#5032](https://github.com/spacemeshos/go-spacemesh/pull/5032) Activeset data pruned from ballots.
* [#5035](https://github.com/spacemeshos/go-spacemesh/pull/5035) Fix possible nil pointer panic when node fails to persist
  nipost builder state.
* [#5079](https://github.com/spacemeshos/go-spacemesh/pull/5079) increase atx cache to 50 000 to reduce disk reads.
* [#5083](https://github.com/spacemeshos/go-spacemesh/pull/5083) Disable beacon protocol temporarily.

## v1.1.5

### Upgrade information

It is critical for most nodes in the network to use v1.1.5 when layer 20 000 starts. Starting from that layer
active set will not be gossipped together with proposals. That was the main network bottleneck in epoch 4.

### Features

* [#4969](https://github.com/spacemeshos/go-spacemesh/pull/4969) Nodes will also fetch from PoET 111 for round 3 if they
  were able to register to PoET 110.

### Improvements

* [#4965](https://github.com/spacemeshos/go-spacemesh/pull/4965) Updates to PoST:
  * Prevent errors when shutting down the node that can result in a crash
  * `postdata_metadata.json` is now updated atomically to prevent corruption of the file.
* [#4956](https://github.com/spacemeshos/go-spacemesh/pull/4956) Active set is will not be gossipped in every proposal.
  Active set usually contains list of atxs that targets current epoch. As the number of atxs grows this object grows as well.
* [#4993](https://github.com/spacemeshos/go-spacemesh/pull/4993) Drop proposals after generating a block. This limits
  growth of the state.

## v1.1.4

### Upgrade information

### Highlights

### Features

* [#4765](https://github.com/spacemeshos/go-spacemesh/pull/4765) hare 3 consensus protocol.

Replacement for original version of hare. Won't be enabled on mainnet for now.
Otherwise protocol uses significantly less traffic (at least x20), and will allow
to set lower expected latency in the network, eventually reducing layer time.

### Improvements

* [#4879](https://github.com/spacemeshos/go-spacemesh/pull/4879) Makes majority calculation weighted for optimistic filtering.
The network will start using the new algorithm at layer 18_000 (2023-09-14 20:00:00 +0000 UTC)
* [#4923](https://github.com/spacemeshos/go-spacemesh/pull/4923) Faster ballot eligibility validation. Improves sync speed.
* [#4934](https://github.com/spacemeshos/go-spacemesh/pull/4934) Ensure state is synced before participating in tortoise
  consensus.
* [#4939](https://github.com/spacemeshos/go-spacemesh/pull/4939) Make sure to fetch data from peers that are already connected.
* [#4936](https://github.com/spacemeshos/go-spacemesh/pull/4936) Use correct hare active set after node was synced.
  Otherwise applied layer may lag slightly behind the rest.

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
* [#4871](https://github.com/spacemeshos/go-spacemesh/pull/4871) Add jitter to spread out requests to get poet proof and
  submit challenge
* [#4988](https://github.com/spacemeshos/go-spacemesh/pull/4988) Improve logging around communication with PoET services
