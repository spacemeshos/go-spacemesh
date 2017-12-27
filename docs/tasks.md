#### App Tasks / Issues

Todo: move tasks to github issues

### Build tasks
- Dockerfile to setup a completely isolated docker dev env with no deps installed in the go path - only in /vendor.
- Makefile that builds node packaged binary without relying on packages outside of /vendor - e.g. GOPATH as proj root.
Current makefile is using the global system go path.
- A script to install all required dev tools: e.g. proto-c (with grpc-gateway support). govendor, etc....
- CI - Integrate travis CI with the repo and setup full CI system (on hold until repo is public for Travis integration)

### App Shell Tasks
- Write more tests for app flags, commands and config file
- Impl cli.v1 TOML support and write test with TOML file (a bit of a pain with cli via vendoring) - alt use YAML

### Misc. Tasks
- Tests for all implemented functionality - the more the better.
- Fully support a coinbase account:
    - when new account is created - if it is only account - set it as coinbase on the running node
    - restore node coinbase account when loading a node from storage
- NTP client with time-drift detection (SNTP protocol)
- Refactor swarm and friends into sub-packages with shared public interfaces
- Make sure all types are defined using interface+struct combo and not just structs
- Peers latency matrices and usage in dht/kad 

### Hard(er) Tasks
- Implement an optimized Merkle tree data structure with backing storage in leveldb (kv storage). Implement Merkle proofs.
- Write comprehensive tests for the dht/kad protocol and swarm nodes discovery.
- Implement gossip support (gossip flag) and write tests for gossiping a p2p message (e.g. 20 nodes, 1 bootstrap)

### App Shell Implemented Features
Features we have a basic 1st pass implementation for:
- Initial project packages structure and entry points (app, node, grpcApi, etc...)
- A cli app shell with flags and commands
- Grpc and json-http api servers with config and decent tests
- Basic node with 2 implemented example p2p protocols and basic tests
- Basic keys and id type system (wrapping lib-p2p keys and ids types)
- A buildable go-spacemesh cross-platform exe with full cli features (flags, commands)
- Basic working makefile (not fully isolated from $GOPATH although make should use vendored versions)
- App persistent store with support for multiple nodes data
- Restore node data from store on session start or create new id automatically
- Auto persist node data to store
- Load data about bootstrap nodes on node startup
- Integrated rolling file logging https://github.com/natefinch/lumberjack with go-logging so we get proper rolling files logging - needed for long-running simulations.
- Accounts support with locking/unlocking, persistence and passpharse kdf
- Implement accounts and keystore files
- Per-node logger to console and to node-specific log files

## p2p Implemented Features
- Swarm - peer management, low-level tcp network and connections
- Partly done: node discovery protocol - connect to bootstrap node and to 5 random nodes on startup
- Handshake protocol for secure and authenticated comm using a ECDH key exchange scheme
- App-level p2p protocols support with full encryption and authorship auth - see ping
- Pattern for app-level protocols using callback channels for processing responses to requests
- Design for an extendable protobufs based message format - see message.proto
- Demuxer (router) - routes incoming messages to app-level protocols handlers
- 100% mutex-free (mutexes in msgio should be removed as well) - all concurrent flows are using channels
- 100% lib-p2p free :-)
- Basic DHT and findPeer protocol
