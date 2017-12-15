#### App Tasks / Issues

todo: move tasks to github issues

### Build tasks
- Dockerfile to setup a completely isolated docker dev env
- Makefile that builds node packaged binary without relying on packages outside of /vendor - e.g. GOPATH as proj root.
Current makefile is using a global go path.
- Script to install all required dev tools: e.g. proto-c (with grpc-gateway support). govendor, etc....
- Integrate travis CI with the repo and setup full CI system (hold until repo is public for Travis integration)

### App Shell Tasks
- Write tests for app flags, commands and config file
- Impl cli.v1 TOML support and write test with TOML file (a bit of a pain with cli via vendoring) - alt use YAML

### Misc. Tasks
- Implement an optimized Merkle tree data structure with backing storage in leveldb (kv storage). Implement Merkle proofs.
- Tests for all implemented functionality - the more the better.
- Fully support coinbase account:
    - when new account is created - if it is only account - set it as coinbase on the running node
    - restore node coinbase account when loading a node from storage
- NTP client with time-drift detection (SNTP protocol)

### Hard Tasks
- Implement KAD/DHT node discovery protocol
- Implement gossip support for each protocol (gossip flag) and write tests for gossiping a p2p message (20 nodes, 1 bootstrap)
- Local Javascript console for interactive RPC calls (wrapping the json-http api) - possible via V8 engine?
- Integrate a V8 instance for javascript execution into the node. See: https://github.com/augustoroman/v8 

### App Shell Implemented Features
Stuff we have a basic 1st pass implementation for:
- Initial project packages structure and entry points (app, node, grpcApi, etc...)
- A cli app shell with flags and commands
- Grpc and json-http api servers with config and decent tests
- Basic node with 2 implemented example p2p protocols and basic tests
- Basic keys and id type system (wrapping lib-p2p keys and ids types)
- A buildable go-unruly cross-platform exe with full cli features (flags, commands)
- Basic working makefile (not fully isolated from $GOPATH although make should use vendored versions)
- App persistent store with support for multiple nodes data
- Restore node data from store on session start or create new id automatically
- Auto persist node data to store
- Load data about bootstrap nodes on node startup
- Integrated rolling file logging https://github.com/natefinch/lumberjack with go-logging so we get proper rolling files logging - needed for long-running simulations.
- Accounts support with locking/unlocking, persistence and passpharse kdf
- Implement accounts and keystore files

## p2p implemented features
- Handshake protocol for secure and authenticated comm
- App-level p2p protocols support with full encryption authorship auth - see ping
- Get rid of libp2p :-)