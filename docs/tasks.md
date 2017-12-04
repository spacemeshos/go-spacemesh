#### App Tasks / Issues

### Build tasks
- Dockerfile to setup a completely isolated docker dev env
- Makefile that builds node packaged binary without relying on packages outside of /vendor - e.g. GOPATH as proj root
- Script to install required build tools: e.g. proto-c (with grpc gateway support). govendor, etc....
- Integrate travis CI with the repo and setup full CI system (hold until repo is public for Travis integration)

### App Shell Tasks
- Write tests for app flags, commands and config file
- Implement accounts and keystore files
- Implement node db in node data folders
- Impl cli.v1 TOML support and write test with TOML file (a bit of a pain with cli via vendoring) - alt use YAML

#### Misc. Tasks
- Implement an optimized Merkle tree data structure with backing storage in leveldb (kv storage). Implement Merkle proofs.
- Write tests to validate the plan to use libp2p-kad-dht for peer discovery (simulated network)
- add support for uTp (over udp) in lib-p2p - it only supports tcp right now - this is a hard task. We might contrib this to lib-p2p.
- Implement gossip support for each protocol (gossip flag) and write tests for gossiping a p2p message (20 nodes, 1 bootstrap)
- Local Javascript console for interactive RPC calls (wrapping the json-http api).
- Tests for all implement functionality - the more the better.

### App Shell Implemented Features

Stuff we have a basic first implementation for:

- Initial project packages strcuture and entry points (app, node, grpcApi, etc...)
- A cli app shell with flags and commands
- Grpc and json-http api servers with config and decent tests
- Basic node with 2 implemented example p2p protocols and basic tests
- Basic keys and id type system (wrapping lib-p2p keys and ids types)
- A buildable go-unruly cross-platform exe with full cli features (flags, commands)
