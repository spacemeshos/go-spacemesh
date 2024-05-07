# Changelog

See [RELEASE](./RELEASE.md) for workflow instructions.

## Release v1.5.2-hotfix1

This release includes our first CVE fix. A vulnerability was found in the way a node handles incoming ATXs. We urge all
node operators to update to this version as soon as possible.

### Improvements

* Fixed a vulnerability in the way a node handles incoming ATXs. This vulnerability allows an attacker to claim rewards
  for a full tick amount although they should not be eligible for them.

## Release v1.5.2

### Improvements

* [#5904](https://github.com/spacemeshos/go-spacemesh/pull/5904) Avoid repeated searching for positioning ATX in 1:N

* [#5911](https://github.com/spacemeshos/go-spacemesh/pull/5911) Avoid pulling poet proof multiple times in 1:N setups

## Release v1.5.1

### Improvements

* [#5896](https://github.com/spacemeshos/go-spacemesh/pull/5896) Increase supported number of ATXs to 4.5 Mio.

## Release v1.5.0

### Upgrade information

* [#5814](https://github.com/spacemeshos/go-spacemesh/pull/5814) Removed in-code local DB migrations.
  Updating to this version requires going through v1.4 first.

### Improvements

* [#5807](https://github.com/spacemeshos/go-spacemesh/pull/5807) Implement SMIP-0002: remove vesting vault cliff.

* [#5840](https://github.com/spacemeshos/go-spacemesh/pull/5840) Allow vaults to spend received (as well as vested)
coins. Fixes an oversight in the genesis VM implementation.

* [#5791](https://github.com/spacemeshos/go-spacemesh/pull/5791) Speed up ATX queries.
  This also fixes ambiguity of nonces for equivocating identities.

* [#5856](https://github.com/spacemeshos/go-spacemesh/pull/5856) Bump github.com/spacemeshos/api/release/go to v1.37.0.

## Release v1.4.6

### Improvements

* [#5839](https://github.com/spacemeshos/go-spacemesh/pull/5839) Fix a bug where nodes would stop agreeing on the order
  of TX within a block.

## Release v1.4.5

### Improvements

* [#5796](https://github.com/spacemeshos/go-spacemesh/pull/5796) Reject p2p messages containing invalid malfeasance proofs.

* [#5797](https://github.com/spacemeshos/go-spacemesh/pull/5797) Improve logging around ATX building process.

* [#5802](https://github.com/spacemeshos/go-spacemesh/pull/5802) Increase the number of supported ATX per epoch to 3.5 Mio.

* [#5803](https://github.com/spacemeshos/go-spacemesh/pull/5803) Fixed PoST verifiers autoscaling for 1:N setups.

* [#5815](https://github.com/spacemeshos/go-spacemesh/pull/5815) Add spread to the discovery advertisement interval.

* [#5819](https://github.com/spacemeshos/go-spacemesh/pull/5819) The node will now refuse connections from post services
  if no coinbase account is set.

## Release v1.4.4

### Improvements

* [#5777](https://github.com/spacemeshos/go-spacemesh/pull/5777) Adjusted GRPC keepalive parameters on node to allow
  pings every 60 seconds and send keepalive pings every 10 minutes if no activity from the client is observed.

## Release v1.4.3

### Improvements

* [#5753](https://github.com/spacemeshos/go-spacemesh/pull/5753) Fix for a possible segmentation fault in setups with
  remote post services when an identity does their initial proof.

* [#5755](https://github.com/spacemeshos/go-spacemesh/pull/5755) improve efficiency of downloading and applying blocks
  after ballots were counted.

* [#5761](https://github.com/spacemeshos/go-spacemesh/pull/5761) don't interrupt sync if ballots in a layer were
  ignored or rejected.

* [#5762](https://github.com/spacemeshos/go-spacemesh/pull/5762) Fix a bug where the node could get stuck in a loop
  when trying to fetch a block that is not in the mesh.

## Release v1.4.2

### Improvements

* [#5730](https://github.com/spacemeshos/go-spacemesh/pull/5730) Fixed a bug where the node behaves incorrectly when
  first started with supervised smeshing.

* [#5731](https://github.com/spacemeshos/go-spacemesh/pull/5731) The default listen address for `PostService` is now
  `127.0.0.1:0` instead of `127.0.0.1:9094`. This will ensure that a node binds the post service to a random free port
  and prevents multiple instances of the post service from binding to the same port.

* [#5735](https://github.com/spacemeshos/go-spacemesh/pull/5735) Don't hold database connections for long in fetcher
  streaming mode

* [#5736](https://github.com/spacemeshos/go-spacemesh/pull/5736) Fixed slow POST initialization on Windows.

* [#5738](https://github.com/spacemeshos/go-spacemesh/pull/5738) sql: fix epoch ATX ID cache deadlock

## Release v1.4.1

### Improvements

* [#5707](https://github.com/spacemeshos/go-spacemesh/pull/5707) Fix a race on closing a channel when the node is
  shutting down.

* [#5709](https://github.com/spacemeshos/go-spacemesh/pull/5709) Prevent users from accidentally deleting their keys,
  if they downgrade to v1.3.x and upgrade again.

* [#5710](https://github.com/spacemeshos/go-spacemesh/pull/5710) Node now checks the database version and will refuse to
  start if it is newer than expected.

* [#5562](https://github.com/spacemeshos/go-spacemesh/pull/5562) Add streaming mode for fetcher. This should lessen
  GC pressure during sync

* [#5684](https://github.com/spacemeshos/go-spacemesh/pull5684) Use separate fetcher protocol for active sets.
  This enables separate pacers for active set requests

* [#5718](https://github.com/spacemeshos/go-spacemesh/pull/5718) Sync malfeasance proofs continuously.

## Release v1.4.0

### Upgrade information

#### Post service endpoint

The post service now has its own endpoint separate from `grpc-private-listener`. The default for `grpc-post-listener`
is `127.0.0.1:9094`. In contrast to `grpc-tls-listener` this endpoint does not require setting up mTLS.

The post service cannot connect to `grpc-private-listener` anymore. If you are using a remote smeshing setup please
adjust your configuration accordingly. If you are using a remote setup with mTLS over a private network you can switch
to using `grpc-post-listener` to not require the overhead of mTLS. We however strongly recommend using an mTLS
encrypted connection between the post service and the node over insecure connections (e.g. over the Internet).

Smeshers using the default setup with a supervised post service do not need to make changes to their node configuration.

#### Fully migrated local state into `local.sql`

With this release the node has fully migrated its local state into `local.sql`. During the first start after the
upgrade the node will migrate the data from disk and store it in the database. This change also allows the PoST data
directory to be set to read only after the migration is complete, as the node will no longer write to it.

**NOTE:** To ensure a successful migration make sure that the config contains all PoETs your node is using.

#### New poets configuration

Upgrading requires changes in config and in CLI flags (if not using the default).

⚠ Users that use additional poet servers need to add their address and public key to one of the lists below,
depending on their configuration method.

##### CLI argument `--poet-server`

This argument was replaced by `--poet-servers` that take JSON-encoded array of poet server descriptors
(address, public key). The default value is

```json
[
  {
    "address": "https://mainnet-poet-0.spacemesh.network",
    "pubkey": "cFnqCS5oER7GOX576oPtahlxB/1y95aDibdK7RHQFVg="
  },
  {
    "address": "https://mainnet-poet-1.spacemesh.network",
    "pubkey": "Qh1efxY4YhoYBEXKPTiHJ/a7n1GsllRSyweQKO3j7m0="
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
  }
}
```

#### Extend go-spacemesh with option to manage multiple identities/PoST services

**NOTE:** This is a new feature, not yet supported by Smapp and possibly subject to change. Please use with caution.

A node can now manage multiple identities and their life cycle. This reduces the amount of data that is needed to be
broadcasted / fetched from the network and reduces the amount of data that needs to be stored locally, because only one
database is needed for all identities instead of one for each.

To ensure you are eligible for rewards of any given identity, the associated PoST service must be running and connected
to the node during the cyclegap set in the node's configuration. After successfully broadcasting the ATX and registering
at a PoET server the PoST services can be stopped with only the node having to be online.

This change moves the private keys associated for an identity from the PoST data directory to the node's data directory
and into the folder `identities` (i.e. if `state.sql` is in folder `data` the keys will now be stored in `data/identities`).
The node will automatically migrate the `key.bin` file from the PoST data directory during the first startup and copy
it to the new location as `local.key`. The content of the file stays unchanged (= the private key of the identity hex-encoded).

##### Adding new identities/PoST services to a node

To add a new identity to a node, initialize PoST data with `postcli` and let it generate a new private key for you:

```shell
./postcli -provider=2 -numUnits=4 -datadir=/path/to/data \
    -commitmentAtxId=c230c51669d1fcd35860131e438e234726b2bd5f9adbbd91bd88a718e7e98ecb
```

Make sure to replace `provider` with your provider of choice and `numUnits` with the number of PoST units you want to
initialize. The `commitmentAtxId` is the commitment ATX ID for the identity you want to initialize. For details on the
usage of `postcli` please refer to [postcli README](https://github.com/spacemeshos/post/blob/develop/cmd/postcli/README.md).

During initialization `postcli` will generate a new private key and store it in the PoST data directory as `identity.key`.
Copy this file to your `data/identities` directory and rename it to `xxx.key` where `xxx` is a unique identifier for
the identity. The node will automatically pick up the new identity and manage its lifecycle after a restart.

Setup the `post-service` [binary](https://github.com/spacemeshos/post-rs/releases) or
[docker image](https://hub.docker.com/r/spacemeshos/post-service/tags) with the data and configure it to connect to your
node. For details refer to the [post-service README](https://github.com/spacemeshos/post-rs/blob/main/service/README.md).

##### Migrating existing identities/PoST services to a node

If you have multiple nodes running and want to migrate to use only one node for all identities:

1. Stop all nodes.
2. Convert the nodes to remote nodes by setting `smeshing-start` to `false` in the configuration/cli parameters and
   renaming the `local.key` file to a unique  name in the PoST data directory.
3. Use the `merge-nodes` CLI tool to merge your remote nodes into one. Follow the instructions of the tool to do so.
   It will copy your keys (as long as you gave all of them unique names) and merge your `local.sql` databases.
4. Start the node that you used as target for `merge-nodes` and is now managing the identities.
5. For every identity setup a post service to use the existing PoST data for that identity and connect to the node.
   For details refer to the [post-service README](https://github.com/spacemeshos/post-rs/blob/main/service/README.md).

**WARNING:** DO NOT run multiple nodes with the same identity at the same time! This will result in an equivocation
and permanent ineligibility for rewards.

### Highlights

* [#5599](https://github.com/spacemeshos/go-spacemesh/pull/5599) new atx sync that is less fragile to network failures.

  new atx sync will avoid blocking startup, and additionally will be running in background to ask peers for atxs.
  by default it does that every 4 hours by requesting known atxs from 2 peers. configuration can be adjusted by providing

```json
  {
    "syncer": {
      "atx-sync": {
        // interval and number of peers determine how much traffic node will spend for asking about known atxs.
        // for example in this configuration every 4 hours it will download known atxs from 2 peers.
        // with 2_000_000 atxs it will amount to ~128MB of traffic every 4 hours.
        "epoch-info-request-interval": "4h",
        "epoch-info-peers": 2,
        // number of retries to fetch any specific atx.
        "requests-limit": 20,
        // number of full atxs that will be downloaded in parallel.
        // you can try to tune this value up if sync will be slow.
        "atxs-batch": 1000,
        // atx sync progress will be reported when 10% of known atxs were downloaded, or every 20 minutes.
        // if it is too noisy tune them to your liking.
        "progress-every-fraction": 0.1,
        "progress-on-time": "20m"
      }
    }
  }
```

* [#5293](https://github.com/spacemeshos/go-spacemesh/pull/5293) change poet servers configuration
  The config now takes the poet server address and its public key. See the [Upgrade Information](#new-poets-configuration)
  for details.

* [#5390](https://github.com/spacemeshos/go-spacemesh/pull/5390)
  Distributed PoST verification.

  The nodes on the network can now choose to verify
  only a subset of labels in PoST proofs by choosing a K3 value lower than K2.
  If a node finds a proof invalid, it will report it to the network by
  creating a malfeasance proof. The malicious node will then be blacklisted by the network.

* [#5592](https://gihtub.com/spacemeshos/go-spacemesh/pull/5592)
  Extend node with option to have multiple PoST services connect. This allows users to run multiple PoST services,
  without the need to run multiple nodes. A node can now manage multiple identities and will manage the lifecycle of
  those identities.
  To collect rewards for every identity, the associated PoST service must be running and connected to the node during
  the cyclegap set in the node's configuration.

* [#5651](https://github.com/spacemeshos/go-spacemesh/pull/5651)
  New GRPC service `spacemesh.v1.PostInfoService.PostStates` allowing to check the status of
  PoST proving of all registered identities. It's useful for operators to figure out when
  post-services for each identity need to be up or can be shutdown.

  An example output:

  ```json
  {
    "states": [
      {
        "id": "874uW9N/y0PwUXxJ8Kb8Q2Yd+sqhpGJYkLbjlkIz5Ig=",
        "state": "IDLE",
        "name": "post0.key"
      },
      {
        "id": "sBq/pHUHtzczY1quuG2muPGkksfVldwnH0M/eUPc3qE=",
        "state": "PROVING",
        "name": "post1.key"
      }
    ]
  }
  ```

* [#5685](https://github.com/spacemeshos/go-spacemesh/pull/5685) A new tool `merge-nodes` was added to the repository
  and release artifact. It can be used to merge multiple nodes into one. This is useful for users that have multiple
  nodes running and want to migrate to use only one node for all identities. See the
  [Upgrade Information](#migrating-existing-identitiespost-services-to-a-node) for details.

### Features

* [#5678](https://github.com/spacemeshos/go-spacemesh/pull/5678) API to for changing log level without restarting a
  node. Examples:

  > grpcurl -plaintext -d '{"module": "sync", "level": "debug"}' 127.0.0.1:9093 spacemesh.v1.DebugService.ChangeLogLevel
  > grpcurl -plaintext -d '{"module": "*", "level": "debug"}' 127.0.0.1:9093 spacemesh.v1.DebugService.ChangeLogLevel

  "*" will replace log level for all known modules, expect that some of them will spam too much.
* [#5612](https://github.com/spacemeshos/go-spacemesh/pull/5612)
  Add relay command for running dedicated relay nodes

### Improvements

* [#5641](https://github.com/spacemeshos/go-spacemesh/pull/5641) Rename `node_state.sql` to `local.sql`.

  To avoid confusion with the `state.sql` database, the `node_state.sql` database has been renamed to `local.sql`.

* [#5219](https://github.com/spacemeshos/go-spacemesh/pull/5219) Migrate data from `nipost_builder_state.bin` to `local.sql`.

  The node will automatically migrate the data from disk and store it in the database. The migration will take place at the
  first startup after the upgrade.

* [#5418](https://github.com/spacemeshos/go-spacemesh/pull/5418) Add `grpc-post-listener` to separate post service from
  `grpc-private-listener` and not require mTLS for the post service.

* [#5465](https://github.com/spacemeshos/go-spacemesh/pull/5465)
  Add an option to cache SQL query results. This is useful for nodes with high peer counts.

  If you are not using a remote post service you do not need to adjust anything. If you are using a remote setup
  make sure your post service now connects to `grpc-post-listener` instead of `grpc-private-listener`. If you are
  connecting to a remote post service over the internet we strongly recommend using mTLS via `grpc-tls-listener`.
* [#5601](https://github.com/spacemeshos/go-spacemesh/pull/5601) measure latency from all requests in sync
  This improves peers selection logic, mainly to prevent asking slow peers for collection of atxs, which often blocks sync.

* [5602](https://github.com/spacemeshos/go-spacemesh/pull/5602) Optimize client side of fetcher to avoid encoding when
  not needed.

* [5561](https://github.com/spacemeshos/go-spacemesh/pull/5561) Reuse atxdata in Tortoise to optimize memory usage.

## Release v1.3.11

### Improvements

* [#5586](https://github.com/spacemeshos/go-spacemesh/pull/5586)
  Do not try to publish proofs for malicious ATXs during sync.
  Publishing is blocked during sync because `Syncer::ListenToATXGossip()` returns false, and thus every malicious ATX being
  synced was causing an error resulting in an interruption of sync.

* [#5603](https://github.com/spacemeshos/go-spacemesh/pull/5603)
  Do not try to sync over transient (relayed) connections. This fixes
  possible sync issues when hole punching is enabled.

* [#5618](https://github.com/spacemeshos/go-spacemesh/pull/5618)
  Add index on ATXs that makes epoch ATX requests faster

* [#5619](https://github.com/spacemeshos/go-spacemesh/pull/5619)
  Updated data structures to support the network with up to 2.2 unique smesher identities.

## Release v1.3.10

### Improvements

* [#5564](https://github.com/spacemeshos/go-spacemesh/pull/5564) Use decaying tags for fetch peers. This prevents
  libp2p's Connection Manager from breaking sync.

* [#5522](https://github.com/spacemeshos/go-spacemesh/pull/5522) Disable mesh agreement sync protocol.
  It reduces number of requests for historical activation ids.

* [#5571](https://github.com/spacemeshos/go-spacemesh/pull/5571) Adjust to 2.2M ATXs

## Release v1.3.9

### Improvements

* [#5530](https://github.com/spacemeshos/go-spacemesh/pull/5530)
  Adjusted cache sizes for the increased number of ATXs on the network.

* [#5511](https://github.com/spacemeshos/go-spacemesh/pull/5511)
  Fix dialing peers on their private IPs, which was causing "portscan" complaints.

## Release v1.3.8

### Features

* [#5517](https://github.com/spacemeshos/go-spacemesh/pull/5517)
  Added a flag `--smeshing-opts-verifying-disable` and a config parameter `smeshing-opts-verifying-disable`
  meant for disabling verifying POST in incoming ATXs on private nodes.
  The verification should be performed by the public node instead.

### Improvements

* [#5441](https://github.com/spacemeshos/go-spacemesh/pull/5441)
  Fix possible nil-pointer dereference in blocks.Generator.

* [#5512](https://github.com/spacemeshos/go-spacemesh/pull/5512)
  Increase EpochActiveSet limit to 1.5M to prepare for 1M+ ATXs.

* [#5515](https://github.com/spacemeshos/go-spacemesh/pull/5515)
  Increase fetcher limit to 60MiB to prepare for 1M+ ATXs.

* [#5518](https://github.com/spacemeshos/go-spacemesh/pull/5518) In rare cases the node could create a malfeasance
  proof against itself. This is now prevented.

## Release v1.3.7

### Improvements

* [#5502](https://github.com/spacemeshos/go-spacemesh/pull/5502)
  Increase limits of p2p messages to compensate for the increased number of nodes on the network.

## Release v1.3.6

### Improvements

* [#5479](https://github.com/spacemeshos/go-spacemesh/pull/5486)
  p2p: make AutoNAT service limits configurable. This helps with AutoNAT dialback to determine
  nodes' reachability status.

* [#5490](https://github.com/spacemeshos/go-spacemesh/pull/5490)
  The path in `smeshing-opts-datadir` used to be resolved relative to the location of the `service` binary when running
  the node in supervised mode. This is no longer the case. The path is now resolved relative to the current working
  directory.

* [#5489](https://github.com/spacemeshos/go-spacemesh/pull/5489)
  Fix problem in POST proving where too many files were opened at the same time.

* [#5498](https://github.com/spacemeshos/go-spacemesh/pull/5498)
  Reduce the default number of CPU cores that are used for verifying incoming ATXs to half of the available cores.

* [#5462](https://github.com/spacemeshos/go-spacemesh/pull/5462) Add separate metric for failed p2p server requests

* [#5464](https://github.com/spacemeshos/go-spacemesh/pull/5464) Make fetch request timeout configurable.

* [#5463](https://github.com/spacemeshos/go-spacemesh/pull/5463)
  Adjust deadline during long reads and writes, reducing "i/o deadline exceeded" errors.

* [#5494](https://github.com/spacemeshos/go-spacemesh/pull/5494)
  Make routing discovery more configurable and less spammy by default.
* [#5511](https://github.com/spacemeshos/go-spacemesh/pull/5511)
  Fix dialing peers on their private IPs, which was causing "portscan" complaints.

## Release v1.3.5

### Improvements

* [#5470](https://github.com/spacemeshos/go-spacemesh/pull/5470)
  Fixed a bug in event reporting where the node reports a disconnection from the PoST service as a "PoST failed" event.
  Disconnections cannot be avoided completely and do not interrupt the PoST proofing process. As long as the PoST
  service reconnects within a reasonable time, the node will continue to operate normally without reporting any errors
  via the event API.

  Users of a remote setup should make sure that the PoST service is actually running and can reach the node. Observe
  the log of both apps for indications of a connection problem.

## Release v1.3.4

### Improvements

* [#5467](https://github.com/spacemeshos/go-spacemesh/pull/5467)
  Fix a bug that could cause ATX sync to stall because of exhausted limit of concurrent requests for dependencies.
  Fetching dependencies of an ATX is not limited anymore.

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

* further increased cache sizes and p2p timeouts to compensate for the increased number of nodes on the network.

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
