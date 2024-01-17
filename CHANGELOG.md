# Changelog

See [RELEASE](./RELEASE.md) for workflow instructions.

## UNRELEASED

### Upgrade information

#### New poets configuration

Upgrading requires changes in config and in CLI flags (if not using the default).

⚠ Users that use additional poet servers need to add their address and public key to one of the lists below,
depending on their configuration method.

##### CLI argument `--poet-server`

This argument was replaced by `--poet-servers` that take JSON-encoded array of poet server descriptors
(address, public key). The default value is

```json
[{"address":"https://mainnet-poet-0.spacemesh.network","pubkey":"cFnqCS5oER7GOX576oPtahlxB/1y95aDibdK7RHQFVg="},{"address":"https://mainnet-poet-1.spacemesh.network","pubkey":"Qh1efxY4YhoYBEXKPTiHJ/a7n1GsllRSyweQKO3j7m0="},{"address":"https://mainnet-poet-2.spacemesh.network","pubkey":"8RXEI0MwO3uJUINFFlOm/uTjJCneV9FidMpXmn55G8Y="},{"address":"https://poet-110.spacemesh.network","pubkey":"8Qqgid+37eyY7ik+EA47Nd5TrQjXolbv2Mdgir243No="},{"address":"https://poet-111.spacemesh.network","pubkey":"caIV0Ym59L3RqbVAL6UrCPwr+z+lwe2TBj57QWnAgtM="},{"address":"https://poet-112.spacemesh.network","pubkey":"5p/mPvmqhwdvf8U0GVrNq/9IN/HmZj5hCkFLAN04g1E="}]
```

##### config.json

The existing field `poet-server` that accepted an array of addresses of poet servers
was replaced by a new field `poet-servers` that accepts an object containing an address and a public key.
The default configuration was adapted and doesn't need any action on the user's side. Users who
overwrite the `poet-server` field need to adapt their config accordingly. The default
configuration is as follows:

```json
{
  "main": {
    "poet-servers": [
      {
        "address": "https://mainnet-poet-0.spacemesh.network",
        "pubkey": "cFnqCS5oER7GOX576oPtahlxB/1y95aDibdK7RHQFVg="
      },
      {
        "address": "https://mainnet-poet-1.spacemesh.network",
        "pubkey": "Qh1efxY4YhoYBEXKPTiHJ/a7n1GsllRSyweQKO3j7m0="
      },
      {
        "address": "https://mainnet-poet-2.spacemesh.network",
        "pubkey": "8RXEI0MwO3uJUINFFlOm/uTjJCneV9FidMpXmn55G8Y="
      },
      {
        "address": "https://poet-110.spacemesh.network",
        "pubkey": "8Qqgid+37eyY7ik+EA47Nd5TrQjXolbv2Mdgir243No="
      },
      {
        "address": "https://poet-111.spacemesh.network",
        "pubkey": "caIV0Ym59L3RqbVAL6UrCPwr+z+lwe2TBj57QWnAgtM="
      },
      {
        "address": "https://poet-112.spacemesh.network",
        "pubkey": "5p/mPvmqhwdvf8U0GVrNq/9IN/HmZj5hCkFLAN04g1E="
      }
    ]
  },
}
```

### Highlights

* [#5293](https://github.com/spacemeshos/go-spacemesh/pull/5293) change poet servers configuration
  The config now takes the poet server address and its public key. See the [Upgrade Information](#new-poets-configuration)
  for details.

* [#5219](https://github.com/spacemeshos/go-spacemesh/pull/5219) Migrate data from `nipost_builder_state.bin` to `node_state.sql`.

  The node will automatically migrate the data from disk and store it in the database. The migration will take place at the
  first startup after the upgrade.

### Features

### Improvements

## Release v1.3.3

### Improvements

* [#5442](https://github.com/spacemeshos/go-spacemesh/pull/5442)
  Limit concurrent requests for ATXs to reduce usage of memory and p2p streams.

* [#5394](https://github.com/spacemeshos/go-spacemesh/pull/5394) Add rowid to tables with inefficient clustered indices.
  This reduces database size and improves its performance.

* [#5334](https://github.com/spacemeshos/go-spacemesh/pull/5334) Hotfix for API queries for activations.
  Two API endpoints (`MeshService.{AccountMeshDataQuery,LayersQuery}`) were broken because they attempt to read
  all activation data for an epoch. As the number of activations per epoch has grown, this brute force query (i.e.,
  without appropriate database indices) became very expensive and could cause the node to hang and consume an enormous
  amount of resources. This hotfix removes all activation data from these endpoints so that they still work for
  querying other data. It also modifies `LayersQuery` to not return any _ineffective_ transactions in blocks, since
  there's currently no way to distinguish between effective and ineffective transactions using the API.

* [#5417](https://github.com/spacemeshos/go-spacemesh/pull/5417)
  Prioritize verifying own ATX's PoST.

* [#5423](https://github.com/spacemeshos/go-spacemesh/pull/5423)
  Wait to be ATX-synced before selecting the commitment ATX for initialization.
  Also, remove unnecessary wait for ATXs to be synced before beginning initialization if
  the commitment ATX is already selected.

## Release v1.3.2

### Improvements

* [#5432](https://github.com/spacemeshos/go-spacemesh/pull/5419) Fixed a possible race that is caused by a node
  processing the same bootstrapped active set twice.

## Release v1.3.1

### Improvements

* [#5419](https://github.com/spacemeshos/go-spacemesh/pull/5419) Fixed `0.0.0.0` not being a valid listen address for
  `grpc-private-listener`.

* [#5424](https://github.com/spacemeshos/go-spacemesh/pull/5424) Further increased limits for message sizes and caches
  to compensate for the increased number of nodes on the network.

## Release v1.3

### Upgrade information

This release is not backwards compatible with v1.2.x. Upgrading will migrate local state to a new database.
The migration will take place at the first startup after the upgrade. Be aware that after a successful upgrade
downgrading isn't supported and might result in at least one epoch of missed rewards. See change #5207 for more
information.

This release is the first step towards separating PoST from the node. Proof generation is now done via a dedicated
service. This service is started automatically by the node and is shut down when the node shuts down. In most
setups this should work out of the box, but if you are running into issues please check the README.md file
for more information on how to configure the node to work with the PoST service.

### Highlights

### Features

### Improvements

* [#5091](https://github.com/spacemeshos/go-spacemesh/pull/5091) Separating PoST from the node into its own service.

* [#5061](https://github.com/spacemeshos/go-spacemesh/pull/5061) Proof generation is now done via a dedicated service
  instead of the node.

* [#5154](https://github.com/spacemeshos/go-spacemesh/pull/5154) Enable TLS connections between node and PoST service.

  PoST proofs are now done via a dedicated process / service that the node communicates with via gRPC. Smapp users can
  continue to smesh as they used to. The node will automatically start the PoST service when it starts and will shut it
  down when it shuts down.

* [#5171](https://github.com/spacemeshos/go-spacemesh/pull/5171) Set minimal active set according to the observed number
  of atxs.

  It will prevent ballots that under report observed atxs from spamming the network. It doesn't have impact on rewards.

* [#5169](https://github.com/spacemeshos/go-spacemesh/pull/5169) Support pruning activesets.

  As of epoch 6 activesets storage size is about ~1.5GB. They are not useful after verifying eligibilities
  for ballots in the current epoch and can be pruned.

  Pruning will be enabled starting from epoch 8, e.g in epoch 8 we will prune all activesets for epochs 7 and below.
  We should also run an archival node that doesn't prune them. To disable pruning we should configure

  ```json
  "main": {
    "prune-activesets-from": 4294967295
  }
  ```

* [#5189](https://github.com/spacemeshos/go-spacemesh/pull/5189) Removed deprecated "Best Provider" option for initialization.

  With v1.1.0 (<https://github.com/spacemeshos/go-spacemesh/releases/tag/v1.1.0>) selecting `-1` as `smeshing-opts-provider`
  has been deprecated. This option has now been removed. Nodes that already finished initialization can leave this setting
  empty, as it is not required any more to be set when no initialization is performed. For nodes that have not yet created
  their initial proof the operator has to specify which provider to use. For Smapp users this is done automatically by
  Smapp, users that do not use Smapp may use `postcli -printProviders` (<https://github.com/spacemeshos/post/releases>)
  to list their OpenCL providers and associated IDs.

* [#5207](https://github.com/spacemeshos/go-spacemesh/pull/5207) Move the NiPoST state of a node into a new node-local database.

  The node now uses 2 databases: `state.sql` which holds the node's view on the global state of the network and `node_state.sql`
  which holds ephemeral data of the node.

  With this change `post.bin` and `nipost_challenge.bin` files are no longer used. The node will automatically migrate
  the data from disk and store it in the database. The migration will take place during the first startup after the upgrade.
  If you want to downgrade to a version before v1.3.0 you will need to restore `state.sql` as well as `post.bin` and
  `nipost_challenge.bin` from a backup and do so before the end of the PoET round in which you upgraded. Otherwise the node
  will not be able to participate in consensus and will miss at least one epoch of rewards.

* [#5209](https://github.com/spacemeshos/go-spacemesh/pull/5209) Removed API to update poet servers from SmesherService.

* [#5276](https://github.com/spacemeshos/go-spacemesh/pull/5276) Removed the option to configure API services per endpoint.
  The public listener exposes the following services: "debug", "global", "mesh", "transaction", "node", "activation"
  The private listener exposes the following services: "admin", "smesher", "post"
  The mTLS listener exposes only the "post" service.

* [#5199](https://github.com/spacemeshos/go-spacemesh/pull/5199) Adds smesherID to rewards table. Historically rewards
  were keyed by (coinbase, layer). Now the primary key has changed to (smesherID, layer), which allows querying rewards
  by any subset of layer, smesherID, and coinbase. While this change does add smesherID to existing API endpoints
  (`GlobalStateService.{AccountDataQuery,AccountDataStream,GlobalStateStream}`), it does not yet expose an endpoint to
  query rewards by smesherID. Additionally, it does not re-index old data. Rewards will contain smesherID going forward,
  but to refresh data for all rewards, a node will have to delete its database and resync from genesis.

* [#5329](https://github.com/spacemeshos/go-spacemesh/pull/5329) P2P decentralization improvements. Added support for QUIC
  transport and DHT routing discovery for finding peers and relays. Also, added the `ping-peers` feature which is useful
  during connectivity troubleshooting. `static-relays` feature can be used to provide a static list of circuit v2 relays
  nodes when automatic relay discovery is not desired. All of the relay server resource settings are now configurable. Most
  of the new functionality is disabled by default unless explicitly enabled in the config via `enable-routing-discovery`,
  `routing-discovery-advertise`, `enable-quic-transport`, `static-relays` and `ping-peers` options in the `p2p` config
  section. The non-conditional changes include values/provides support on all of the nodes, which will enable DHT to
  function efficiently for routing discovery.

* [#5367](https://github.com/spacemeshos/go-spacemesh/pull/5367) Add `no-main-override` toplevel config option and
  `--no-main-override` CLI option that makes it possible to run "nomain" builds on mainnet.

* [#5384](https://github.com/spacemeshos/go-spacemesh/pull/5384) to improve network stability and performance allow the
  active set to be set in advance for an epoch. This allows the network to start consensus on the first layer of an epoch.

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
